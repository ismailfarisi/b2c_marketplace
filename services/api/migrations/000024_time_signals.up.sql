-- Time signals: the projection the reel overlay and the "Ending soon" surface
-- both read.
--
-- Every reel in the product is about something that is true for a while and then
-- is not — a lot closes, an offer window shuts, a listing goes stale, tomorrow's
-- delivery cycle locks. Until now the feed carried `published_at` and nothing
-- else temporal, so the only time a client could render was "how old is this",
-- which is the one signal that does not create urgency.
--
-- Implements the time half of internal/seam/subject. The summary half is
-- resolved per page through the registry and is deliberately not stored: it
-- depends on the viewer's locale and currency, and nothing sorts by it.
--
--
-- ── Why this is a table and not another resolver call ────────────────────────
--
-- Actions are resolved per page because availability is viewer-dependent. A time
-- signal is not viewer-dependent, and the difference matters because the feed
-- must *order* by it. "Ending soon" is not a filter applied to ten reels the
-- ranker already chose — it is a different candidate source, sorted by
-- time-to-close across the whole eligible set. That is a query, and a query
-- needs a column.
--
-- So the vertical that owns the subject writes here, and the feed reads a plain
-- indexed timestamptz without ever learning what an auction is. The seam holds:
-- there is no `subject_type` branch anywhere in the feed, only a mode string it
-- passes through to the client.
--
--
-- ── Why it is keyed by subject and not by reel ───────────────────────────────
--
-- A lot closing at 18:00 closes at 18:00 in all four videos of it. Keying by
-- reel would fan the same instant across N rows and then let them drift the
-- moment a fifth reel is uploaded after the projection ran — and the symptom of
-- that drift is one reel in the feed counting down to the wrong second, which
-- reads as a bug in the countdown rather than in the write path.
--
-- The cost is a join on the read path. It is the right cost: for the "Ending
-- soon" surface the join is driven *from* this table, where `ends_at < now() +
-- 6h` is a small and highly selective set, rather than from a reels scan.

CREATE TABLE reel_time_signals (
    subject_type text NOT NULL,
    subject_id   uuid NOT NULL,

    -- Four modes, matching subject.Mode. A CHECK rather than an enum type,
    -- consistent with the rest of the schema: adding a fifth mode is then a
    -- constraint swap inside one migration rather than an ALTER TYPE that
    -- cannot run in a transaction with anything else.
    mode text NOT NULL CHECK (mode IN ('countdown', 'window', 'freshness', 'cutoff')),

    starts_at timestamptz,
    ends_at   timestamptz,

    -- The bar's denominator when the interval does not supply a useful one.
    --
    -- A plate auction opens nine days before it closes; a bar drawn across nine
    -- days does not visibly move during the two minutes anyone is watching,
    -- which is worse than no bar. The vertical picks the span over which urgency
    -- is legible and the client draws across that, saturated before it.
    horizon_seconds int CHECK (horizon_seconds IS NULL OR horizon_seconds > 0),

    -- IANA name. An offset would be correct until the first DST boundary in a
    -- market that observes one, and "order by 16:00" is a rule in the vendor's
    -- wall clock rather than an instant the vendor meant once.
    timezone text,

    -- The boundary comes back: ends_at is the next 16:00, not the last one there
    -- will ever be. Read by the client to decide what happens when the clock
    -- runs out — a countdown that expires removes the reel, a cutoff that passes
    -- rolls to tomorrow and the CTA stays live — and by the worker below to
    -- decide what to re-project.
    recurring boolean NOT NULL DEFAULT false,

    -- When the owning vertical last computed this. Not decoration: it is how the
    -- reconcile worker finds rows whose source changed without anyone telling
    -- us, and how "the countdown was wrong at 14:20" is answerable.
    projected_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (subject_type, subject_id),

    -- The storage-level mirror of subject.TimeSignal.Validate(). Duplicated
    -- rather than left to the Go side because this table has two writers by
    -- design — the vertical and the reconcile worker — and a projection that can
    -- be written incoherently by either will eventually be.
    CONSTRAINT time_signal_coherent CHECK (
        CASE mode
            WHEN 'countdown' THEN ends_at IS NOT NULL
                              AND (starts_at IS NOT NULL OR horizon_seconds IS NOT NULL)
            WHEN 'window'    THEN starts_at IS NOT NULL AND ends_at IS NOT NULL
                              AND ends_at > starts_at
            WHEN 'freshness' THEN starts_at IS NOT NULL AND horizon_seconds IS NOT NULL
            WHEN 'cutoff'    THEN ends_at IS NOT NULL AND timezone IS NOT NULL
        END
    ),

    -- Recurrence is a property of a boundary, and freshness has no boundary.
    CONSTRAINT time_signal_recurrence_needs_an_end CHECK (
        NOT recurring OR ends_at IS NOT NULL
    )
);


-- The "Ending soon" candidate source, and the only index on the read path that
-- has to be fast under load.
--
-- Partial on the modes that have an end, because freshness rows are the majority
-- and would otherwise be two thirds of an index nothing queries. Ascending
-- because the question is always "what closes next".
CREATE INDEX reel_time_signals_ending_idx
    ON reel_time_signals (ends_at)
    WHERE ends_at IS NOT NULL AND mode <> 'freshness';


-- The reconcile worker's index.
--
-- Two jobs share it. A recurring cutoff whose instant has passed must be rolled
-- to the next cycle — the provider owns the recurrence rule, so the worker
-- re-calls Project rather than doing interval arithmetic it would get wrong
-- across a DST boundary. And any row not re-projected in a day is re-projected
-- anyway, because the failure this catches is a vertical that changed a source
-- row without calling the writer, and that failure is silent by construction.
CREATE INDEX reel_time_signals_stale_idx
    ON reel_time_signals (projected_at)
    WHERE recurring OR ends_at IS NOT NULL;


-- No foreign key, and this is a decision rather than an omission.
--
-- `subject_type` is polymorphic: the row points at `vehicle_listings`,
-- `meal_plans` or `service_offerings` depending on its own value, and the
-- schema already made this call once for `reel_actions` in 000013 — the note
-- there about not bolting polymorphism onto a live table applies unchanged.
-- Referential integrity is the writing vertical's job; a subject whose row is
-- gone resolves to no summary and its reels drop out of the feed on the
-- moderation join before this table is consulted.


COMMENT ON TABLE reel_time_signals IS
    'Viewer-independent time dimension per subject, projected by the owning '
    'vertical. Read by the feed for the reel overlay and by the Ending soon '
    'candidate source. See internal/seam/subject.';
