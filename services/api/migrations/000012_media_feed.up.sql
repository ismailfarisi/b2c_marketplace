-- Phase 2: media pipeline, moderation, feed.
--
-- Implements docs/PHASE-2-MEDIA-AND-FEED.md §6, with the additions that document
-- called for in prose but did not put in the DDL: the two independent upload
-- preconditions (§1.1), CHECK constraints on the text-typed enums, and the
-- coverage resolution function behind §4.1.


-- ── vendor membership ────────────────────────────────────────────────────────
--
-- Belongs conceptually with 000003, and arrives here because 000003 is already
-- committed and migrations are append-only. It lands in Phase 2 because this is
-- the phase that first needs it: exit criterion #1 is "a vendor uploads from the
-- web dashboard", and without membership there is no answer to "may this user
-- post as this branch". Until now every write path was customer-scoped.
CREATE TABLE vendor_users (
    vendor_id  uuid NOT NULL REFERENCES vendors (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL DEFAULT 'staff'
                    CHECK (role IN ('owner', 'manager', 'staff')),
    -- NULL means every branch of the vendor. A named branch scopes a staff
    -- member to one facility, which is how a chain actually delegates.
    branch_id  uuid REFERENCES vendor_branches (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (vendor_id, user_id)
);

CREATE INDEX vendor_users_user_idx ON vendor_users (user_id);


-- Platform staff. Arrives with Phase 2 because this is the first phase with a
-- privileged surface — the moderation queue — and "any authenticated user can
-- approve content" is not a state to pass through on the way to something
-- better.
--
-- A table rather than a JWT claim: revoking a moderator must take effect
-- immediately, and a claim stays valid until the access token expires.
CREATE TABLE platform_admins (
    user_id    uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL DEFAULT 'moderator'
                    CHECK (role IN ('moderator', 'ops', 'superadmin')),
    created_at timestamptz NOT NULL DEFAULT now()
);


-- ── media ────────────────────────────────────────────────────────────────────

-- A media_asset is the raw upload. It exists before a reel is playable and
-- outlives the reel row's renditions — a takedown drops the reel from the feed
-- but the asset is what a regulator asks for.
CREATE TABLE media_assets (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_type   text NOT NULL CHECK (owner_type IN ('vendor_branch', 'user')),
    owner_id     uuid NOT NULL,

    -- The S3 key is assigned by us at presign time, never by the client. A
    -- client-chosen key is a path traversal and an overwrite of someone else's
    -- object waiting to happen.
    s3_key       text NOT NULL UNIQUE,
    bucket       text NOT NULL,

    bytes        bigint,
    mime_type    text,
    duration_ms  int,
    width        int,
    height       int,
    -- Raw ffprobe output. Kept because the validation thresholds in §2.3 will be
    -- retuned, and you cannot retune against data you discarded.
    probe        jsonb,

    created_at   timestamptz NOT NULL DEFAULT now(),
    probed_at    timestamptz
);

CREATE INDEX media_assets_owner_idx ON media_assets (owner_type, owner_id, created_at DESC);


-- ── reels ────────────────────────────────────────────────────────────────────

CREATE TYPE reel_state AS ENUM (
    'draft',
    'uploaded',
    'probing',
    'transcoding',
    'moderating',
    'review_pending',
    'approved',
    'published',
    'rejected',
    'removed',
    'failed',
    'expired'
);

CREATE TABLE reels (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Polymorphic from the first migration rather than a vendor_branch_id that
    -- gets widened later. Phase 6 adds user-authored content; retrofitting
    -- polymorphism onto a live table with a million rows and eight foreign keys
    -- pointing at it is a migration nobody schedules.
    author_type    text NOT NULL CHECK (author_type IN ('vendor_branch', 'user')),
    author_id      uuid NOT NULL,

    -- What the reel is *about*. Nullable: a brand reel advertises no single
    -- offering. Phase 3 turns this into the action target.
    subject_type   text CHECK (subject_type IN ('service_offering', 'meal_plan', 'vehicle_listing')),
    subject_id     uuid,

    media_asset_id uuid REFERENCES media_assets (id) ON DELETE SET NULL,

    caption        text,
    caption_locale text CHECK (caption_locale IN ('ar', 'en', 'mixed')),
    category_id    uuid,
    duration_ms    int,

    state          reel_state NOT NULL DEFAULT 'draft',
    visibility     text NOT NULL DEFAULT 'public'
                        CHECK (visibility IN ('public', 'unlisted', 'private')),
    failure_code   text,
    failure_detail text,
    attempts       int NOT NULL DEFAULT 0,

    -- ── the upload race, §1.1 ────────────────────────────────────────────────
    -- Two independent preconditions. The bytes arrive via an S3 event; the
    -- metadata arrives via POST /reels/{id}. Either can land first, so neither
    -- writer chains the next step directly — both stamp their column and call
    -- try_advance_reel(), and whichever commits second does the work.
    bytes_ready_at    timestamptz,
    metadata_ready_at timestamptz,
    -- A draft with no bytes after this is swept to 'expired'. Without it every
    -- abandoned upload holds a presigned URL and a row forever.
    upload_expires_at timestamptz,

    -- ── resolved eligibility geometry, §4.1 ──────────────────────────────────
    -- Service areas are polygons and vehicle listings are radii; resolving both
    -- into one geography column at publish time means one predicate and one
    -- index on the hottest read path in the product. The cost is a
    -- reconciliation job when a vendor edits a service area — a background pass
    -- over a small set, which is the cheaper half of the trade.
    coverage       geography(Geometry, 4326),
    coverage_kind  text CHECK (coverage_kind IN ('service_area', 'radius', 'global')),
    coverage_resolved_at timestamptz,
    origin_point   geography(Point, 4326),

    poster_key     text,
    preview_key    text,
    -- BlurHash or a base64 micro-thumb, stored inline so the feed paints before
    -- any network fetch resolves. Inline because a join to fetch 30 bytes is
    -- worse than the 30 bytes.
    lqip           text,

    published_at   timestamptz,
    removed_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    -- A published reel without coverage would be invisible to every geo query
    -- and look like a pipeline bug rather than a data bug. Refuse it at write
    -- time.
    CONSTRAINT reels_published_needs_coverage
        CHECK (state <> 'published' OR (coverage IS NOT NULL AND published_at IS NOT NULL)),
    CONSTRAINT reels_subject_pair
        CHECK ((subject_type IS NULL) = (subject_id IS NULL))
);

CREATE TRIGGER reels_set_updated_at
    BEFORE UPDATE ON reels
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The feed index. btree_gist (migration 000001) is what lets published_at ride
-- along inside a GiST index; without it the planner fetches every geo match and
-- sorts the lot, which is fine at 5k reels and painful at 500k.
CREATE INDEX reels_feed_idx ON reels
    USING gist (coverage, published_at)
    WHERE state = 'published' AND visibility = 'public';

CREATE INDEX reels_author_recent_idx ON reels (author_id, published_at DESC);
CREATE INDEX reels_state_idx ON reels (state, created_at);
CREATE INDEX reels_subject_idx ON reels (subject_type, subject_id)
    WHERE subject_id IS NOT NULL;
-- Drives the expiry sweeper.
CREATE INDEX reels_upload_expiry_idx ON reels (upload_expires_at)
    WHERE state = 'draft';
-- Drives the coverage reconciliation job after a vendor edits a service area.
CREATE INDEX reels_coverage_stale_idx ON reels (author_type, author_id)
    WHERE state = 'published';


CREATE TABLE reel_renditions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reel_id      uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    label        text NOT NULL CHECK (label IN ('360p', '540p', '720p')),
    width        int NOT NULL,
    height       int NOT NULL,
    bitrate_kbps int NOT NULL,
    playlist_url text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (reel_id, label)
);

CREATE INDEX reel_renditions_reel_idx ON reel_renditions (reel_id);


-- Every transition, with who caused it. When a vendor asks why their reel was
-- rejected — and they will, loudly — this is the answer.
CREATE TABLE reel_state_history (
    id         bigserial PRIMARY KEY,
    reel_id    uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    from_state reel_state,
    to_state   reel_state NOT NULL,
    actor_type text CHECK (actor_type IN ('system', 'moderator', 'author')),
    actor_id   uuid,
    reason     text,
    at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX reel_state_history_reel_idx ON reel_state_history (reel_id, at DESC);


CREATE TABLE reel_tags (
    reel_id uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    tag     text NOT NULL,
    PRIMARY KEY (reel_id, tag)
);

CREATE INDEX reel_tags_tag_idx ON reel_tags (tag);


-- ── moderation ───────────────────────────────────────────────────────────────

CREATE TABLE moderation_jobs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reel_id         uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    layer           text NOT NULL CHECK (layer IN ('poster', 'caption', 'video', 'audio')),
    provider        text NOT NULL CHECK (provider IN ('rekognition', 'transcribe', 'claude', 'stub')),
    provider_job_id text,
    state           text NOT NULL DEFAULT 'queued'
                         CHECK (state IN ('queued', 'running', 'done', 'error')),
    error_detail    text,
    started_at      timestamptz,
    finished_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- One live job per layer per reel. A retry supersedes rather than
    -- duplicates, so the decision step never has to reason about which of three
    -- 'poster' rows is the real one.
    UNIQUE (reel_id, layer)
);

CREATE INDEX moderation_jobs_pending_idx ON moderation_jobs (state, created_at)
    WHERE state IN ('queued', 'running');


-- Every raw label, not just the ones that crossed a threshold. §3.2 says these
-- thresholds are a starting point to tune against the review queue's overturn
-- rate — and you cannot tune on data you did not keep.
CREATE TABLE moderation_labels (
    id           bigserial PRIMARY KEY,
    job_id       uuid NOT NULL REFERENCES moderation_jobs (id) ON DELETE CASCADE,
    reel_id      uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    category     text NOT NULL,
    confidence   numeric(5, 2) NOT NULL CHECK (confidence >= 0 AND confidence <= 100),
    -- Frame offset for video labels; NULL for poster and text.
    timestamp_ms int,
    raw          jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX moderation_labels_reel_idx ON moderation_labels (reel_id);
CREATE INDEX moderation_labels_category_idx ON moderation_labels (category, confidence DESC);


CREATE TABLE moderation_decisions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reel_id      uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    decision     text NOT NULL CHECK (decision IN ('approve', 'reject', 'review')),
    decided_by   text NOT NULL CHECK (decided_by IN ('auto', 'moderator')),
    moderator_id uuid,
    categories   text[],
    notes        text,
    at           timestamptz NOT NULL DEFAULT now(),

    -- A human decision without a human attached is unauditable, and moderation
    -- is exactly where that matters.
    CONSTRAINT moderation_decisions_moderator_present
        CHECK (decided_by <> 'moderator' OR moderator_id IS NOT NULL)
);

CREATE INDEX moderation_decisions_reel_idx ON moderation_decisions (reel_id, at DESC);


-- Trust tiers are the cost control (§3.4): human-reviewing every reel from a
-- vendor with 200 clean posts is waste.
CREATE TABLE author_trust (
    author_type     text NOT NULL CHECK (author_type IN ('vendor_branch', 'user')),
    author_id       uuid NOT NULL,
    tier            text NOT NULL DEFAULT 'new'
                         CHECK (tier IN ('new', 'probation', 'trusted', 'flagged')),
    published_count int NOT NULL DEFAULT 0 CHECK (published_count >= 0),
    strike_count    int NOT NULL DEFAULT 0 CHECK (strike_count >= 0),
    last_strike_at  timestamptz,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (author_type, author_id)
);


CREATE TABLE reel_reports (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reel_id     uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    reporter_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason      text NOT NULL,
    detail      text,
    state       text NOT NULL DEFAULT 'open'
                     CHECK (state IN ('open', 'upheld', 'dismissed')),
    resolved_by uuid,
    resolved_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- One report per person per reel. Without this, a brigade of one account
    -- refreshing looks identical to genuine mass reporting.
    UNIQUE (reel_id, reporter_id)
);

CREATE INDEX reel_reports_open_idx ON reel_reports (created_at)
    WHERE state = 'open';


-- ── feed / engagement ────────────────────────────────────────────────────────

-- Truth in Postgres, hot path in Redis. Never UPDATE this on view — that
-- serializes every viewer of a popular reel behind one row lock, and popularity
-- is precisely when it matters.
CREATE TABLE reel_counters (
    reel_id    uuid PRIMARY KEY REFERENCES reels (id) ON DELETE CASCADE,
    views      bigint NOT NULL DEFAULT 0,
    likes      bigint NOT NULL DEFAULT 0,
    comments   bigint NOT NULL DEFAULT 0,
    shares     bigint NOT NULL DEFAULT 0,
    saves      bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);


CREATE TABLE user_blocks (
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    blocked_type text NOT NULL CHECK (blocked_type IN ('vendor_branch', 'user')),
    blocked_id   uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, blocked_type, blocked_id)
);


-- Client event landing table.
--
-- §5 routes events to Kinesis → ClickHouse. This table is the development sink
-- behind the same interface, and the durable buffer when the stream is
-- unavailable. It is deliberately append-only and index-light: it is written on
-- every batch and read by nothing on the serving path.
CREATE TABLE client_events (
    id          bigserial PRIMARY KEY,
    user_id     uuid,
    session_id  text,
    reel_id     uuid,
    event_type  text NOT NULL,
    -- Client clock. Kept alongside received_at because the difference is the
    -- only way to spot a client batching hours of offline events.
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    properties  jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- Dedup key for at-least-once client retries: (session, reel, type, offset).
    dedup_key   text NOT NULL
);

CREATE UNIQUE INDEX client_events_dedup_idx ON client_events (dedup_key);
CREATE INDEX client_events_reel_idx ON client_events (reel_id, occurred_at);


-- ── coverage resolution, §4.1 ────────────────────────────────────────────────

-- Resolve one reel's eligibility geometry.
--
-- The whole point of §4.1 is that the feed query has exactly one predicate. The
-- asymmetry between polygon-scoped services and radius-scoped listings is
-- absorbed here, once, at publish time — not on every read.
--
-- Returns false when there is nothing to resolve to, which is a publishable
-- reel with no audience and must not silently become one.
CREATE OR REPLACE FUNCTION resolve_reel_coverage(p_reel_id uuid)
RETURNS boolean
LANGUAGE plpgsql
AS $$
DECLARE
    v_author_type text;
    v_author_id   uuid;
    v_coverage    geography;
    v_kind        text;
    v_origin      geography;
BEGIN
    SELECT author_type, author_id INTO v_author_type, v_author_id
      FROM reels WHERE id = p_reel_id;

    IF v_author_type IS NULL THEN
        RETURN false;
    END IF;

    IF v_author_type = 'vendor_branch' THEN
        -- service_area is nullable and a branch that has not drawn one yet has
        -- no audience. Returning false here surfaces that as a publish failure
        -- the vendor can act on, rather than a reel that publishes into a void.
        SELECT b.service_area, b.point
          INTO v_coverage, v_origin
          FROM vendor_branches b
         WHERE b.id = v_author_id
           AND b.deleted_at IS NULL
           AND b.is_active;
        v_kind := 'service_area';
    END IF;

    IF v_coverage IS NULL THEN
        RETURN false;
    END IF;

    UPDATE reels
       SET coverage             = v_coverage,
           coverage_kind        = v_kind,
           origin_point         = v_origin,
           coverage_resolved_at = now()
     WHERE id = p_reel_id;

    RETURN true;
END;
$$;
