-- Phase 8 — Market readiness.
--
-- PLAN.md §7 is headed "GCC specifics — treat as blockers, not polish", and
-- every table below exists because something in that table is a legal or
-- operational precondition for taking money in this region rather than a
-- feature anyone asked for.
--
-- The four groups, and the sentence each one answers:
--
--   notifications  "the delivery OTP is issued and nothing sends it"
--                  (docs/README.md decision #8 — cash on delivery cannot
--                  complete until this exists)
--   tax + invoices "a tax invoice needs a gapless number, a TRN, and a rate
--                  that was correct on the date of supply"
--   KYC + payout   "you may not pay a vendor you have not identified"
--   T&S scale-up   "reporting exists; strikes and appeals do not"
--
-- 17 tables.
--
-- One thing deliberately NOT here: a `vendor_commission_rates` table. The
-- commission model is still open (docs/README.md decision #4) and inventing a
-- schema for a model nobody has chosen is how you end up migrating it twice.


-- ─────────────────────────────────────────────────────────────────────────────
-- 1. Localisation of authored content
-- ─────────────────────────────────────────────────────────────────────────────

-- Vendor-authored strings, per locale.
--
-- PLAN.md §7: "Arabic / RTL — not a translation pass." The UI chrome lives in
-- the web app's message catalogue and always will. This table is the other
-- half, and it is the half that cannot live in a JSON file: dish names, plan
-- descriptions, vehicle listing titles and branch names are written by vendors
-- and sellers, and a deploy is not an acceptable prerequisite for a kitchen
-- adding a dish in Arabic.
--
-- Keyed by (entity, field, locale) rather than by a translation-group id
-- because every read is "give me this field of this row in this locale" and a
-- join through a group table buys nothing at that access pattern.
CREATE TABLE translations (
    entity_type text NOT NULL
                     CHECK (entity_type IN ('vendor', 'vendor_branch', 'service_offering',
                                            'menu_item', 'subscription_plan', 'plan_tier',
                                            'vehicle_listing', 'reel', 'notification_template')),
    entity_id   uuid NOT NULL,
    field       text NOT NULL,
    locale      locale_code NOT NULL,
    value       text NOT NULL,

    -- Machine translations are labelled, and the label is not cosmetic.
    --
    -- An automatic translation displayed as though a human wrote it is how a
    -- menu ends up describing a dish in terms that are merely wrong in English
    -- and offensive in Arabic. Labelling lets the console show "needs review",
    -- lets the feed prefer a human string when both exist, and lets a future
    -- audit find every string no person ever read.
    source      text NOT NULL DEFAULT 'human'
                     CHECK (source IN ('human', 'machine', 'imported')),
    reviewed_by uuid REFERENCES users (id),
    reviewed_at timestamptz,

    -- Set when the source-language field changes underneath this row.
    --
    -- Without it the failure is silent and expensive: a vendor edits an English
    -- price description, the Arabic one keeps the old text, and Arabic-speaking
    -- customers read a description of a product that is no longer sold. Stale
    -- rows are served with a fallback rather than hidden — a stale translation
    -- still beats an English-only string for an Arabic reader — but they are
    -- visible in the console and to the re-translation job.
    stale_since timestamptz,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (entity_type, entity_id, field, locale)
);

CREATE INDEX translations_stale_idx ON translations (entity_type, stale_since)
    WHERE stale_since IS NOT NULL;
CREATE INDEX translations_unreviewed_idx ON translations (entity_type, created_at)
    WHERE source = 'machine' AND reviewed_at IS NULL;

CREATE TRIGGER translations_set_updated_at BEFORE UPDATE ON translations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- ─────────────────────────────────────────────────────────────────────────────
-- 2. Notifications
-- ─────────────────────────────────────────────────────────────────────────────

-- Message bodies, per key, per channel, per locale.
--
-- Templates live in the database rather than in Go source for one reason that
-- outweighs the convenience of compiled-in strings: the Arabic copy is written
-- and corrected by people who do not deploy. A typo in an Arabic SMS should not
-- need a release.
--
-- Versioned rather than updated in place. `notifications` records which version
-- it rendered, so "why did this customer receive that" is answerable six months
-- later — and it is a question that gets asked precisely when the text was
-- wrong.
CREATE TABLE notification_templates (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key         text NOT NULL,
    channel     text NOT NULL
                     CHECK (channel IN ('sms', 'push', 'email', 'whatsapp', 'in_app')),
    locale      locale_code NOT NULL,
    version     int NOT NULL DEFAULT 1,

    -- The consent category. This is what `notification_preferences` switches
    -- off, and the distinction is legal rather than editorial: marketing may be
    -- refused, a delivery OTP may not.
    category    text NOT NULL
                     CHECK (category IN ('transactional', 'delivery', 'account',
                                          'moderation', 'marketing')),

    -- Used by email and push; NULL for SMS.
    subject     text,
    body        text NOT NULL,

    -- WhatsApp Business template name, registered and approved with the
    -- provider. Outside a 24-hour session window only an approved template may
    -- be sent, so a WhatsApp row without one is undeliverable half the time —
    -- see whatsapp_sessions below.
    provider_template_name text,

    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (key, channel, locale, version),

    CONSTRAINT notification_templates_whatsapp_needs_name
        CHECK (channel <> 'whatsapp' OR provider_template_name IS NOT NULL)
);

CREATE UNIQUE INDEX notification_templates_active_idx
    ON notification_templates (key, channel, locale)
    WHERE is_active;

CREATE TRIGGER notification_templates_set_updated_at BEFORE UPDATE ON notification_templates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Seeded because the delivery OTP template is not optional content — a
-- deployment without it cannot complete a cash delivery, which is the exact
-- failure Phase 8 exists to fix. Shipping it as data rather than as a runbook
-- step means the gap cannot reappear in a fresh environment.
--
-- Arabic first-person copy, not a transliteration of the English. "رمز التسليم"
-- is what the code is called; "OTP" transliterated is not a phrase anyone uses.
INSERT INTO notification_templates (key, channel, locale, category, subject, body) VALUES
    ('delivery.otp', 'sms', 'en', 'delivery', NULL,
     'Your delivery code is {{code}}. Give it to the driver to confirm receipt. Do not share it with anyone else.'),
    ('delivery.otp', 'sms', 'ar', 'delivery', NULL,
     'رمز التسليم الخاص بك هو {{code}}. أعطه للسائق لتأكيد الاستلام. لا تشاركه مع أي شخص آخر.'),

    ('delivery.on_the_way', 'sms', 'en', 'delivery', NULL,
     'Your order from {{vendor}} is on the way and should arrive by {{eta}}.'),
    ('delivery.on_the_way', 'sms', 'ar', 'delivery', NULL,
     'طلبك من {{vendor}} في الطريق ومن المتوقع وصوله بحلول {{eta}}.'),

    ('delivery.failed', 'sms', 'en', 'delivery', NULL,
     'We could not complete your delivery from {{vendor}}. Reason: {{reason}}. We will contact you to rearrange.'),
    ('delivery.failed', 'sms', 'ar', 'delivery', NULL,
     'لم نتمكن من إتمام التوصيل من {{vendor}}. السبب: {{reason}}. سنتواصل معك لإعادة الترتيب.'),

    ('order.placed', 'push', 'en', 'transactional', 'Order confirmed',
     'Your order {{order_no}} from {{vendor}} is confirmed. Total {{total}}.'),
    ('order.placed', 'push', 'ar', 'transactional', 'تم تأكيد الطلب',
     'تم تأكيد طلبك {{order_no}} من {{vendor}}. الإجمالي {{total}}.'),

    ('account.login_otp', 'sms', 'en', 'account', NULL,
     '{{code}} is your verification code. It expires in {{minutes}} minutes.'),
    ('account.login_otp', 'sms', 'ar', 'account', NULL,
     '{{code}} هو رمز التحقق الخاص بك. تنتهي صلاحيته خلال {{minutes}} دقائق.'),

    ('moderation.removed', 'push', 'en', 'moderation', 'Content removed',
     'Your {{kind}} was removed because it breaks our {{policy}} rules. You can appeal within {{days}} days.'),
    ('moderation.removed', 'push', 'ar', 'moderation', 'تمت إزالة المحتوى',
     'تمت إزالة {{kind}} الخاص بك لمخالفته قواعد {{policy}}. يمكنك تقديم اعتراض خلال {{days}} أيام.'),

    ('payout.issued', 'email', 'en', 'transactional', 'Your statement for {{period}}',
     'Your payout statement for {{period}} is ready. Net payable: {{net}}.'),
    ('payout.issued', 'email', 'ar', 'transactional', 'كشف حسابك لفترة {{period}}',
     'كشف الدفعات الخاص بك لفترة {{period}} جاهز. صافي المستحق: {{net}}.');


-- Per-user, per-category, per-channel consent.
--
-- Absence means "the default for that category", which is opted-in for
-- everything the user cannot refuse and opted-in-with-a-visible-switch for
-- marketing. Storing only the exceptions keeps this table small and makes the
-- default a single place in code rather than a backfill.
--
-- The CHECK is the important line in this table. UAE and KSA both require a
-- working opt-out for marketing; neither permits a customer to opt out of a
-- one-time password for a delivery they are standing at the door for. Making
-- the database refuse the row is how that survives a well-meaning "let users
-- turn off all notifications" ticket.
CREATE TABLE notification_preferences (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    category   text NOT NULL,
    channel    text NOT NULL
                    CHECK (channel IN ('sms', 'push', 'email', 'whatsapp', 'in_app')),
    allowed    boolean NOT NULL,

    -- Evidence, for the day a regulator asks how consent was obtained.
    source     text NOT NULL DEFAULT 'user'
                    CHECK (source IN ('user', 'import', 'support', 'legal')),
    changed_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, category, channel),

    CONSTRAINT notification_preferences_only_optional_categories
        CHECK (category IN ('marketing'))
);


-- The delivery log.
--
-- Partitioned by month from the start. It is one row per message per recipient
-- and grows with users × activity rather than with orders; the first Ramadan
-- promotion makes it the second-largest table in this database after the
-- location pings.
CREATE TABLE notifications (
    id          uuid NOT NULL DEFAULT gen_random_uuid(),
    user_id     uuid,
    -- Denormalised at send time rather than joined at read time. A number that
    -- has since been changed on the account is not the number the message went
    -- to, and this table's whole job is recording what actually happened.
    destination text NOT NULL,

    template_id uuid,
    template_key text NOT NULL,
    channel     text NOT NULL,
    locale      locale_code NOT NULL,
    category    text NOT NULL,

    -- Rendered body, stored. Costs space and is worth it: without it a dispute
    -- about what a customer was told is settled by re-rendering a template that
    -- may since have changed, which is not evidence.
    rendered_body text NOT NULL,

    status      text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'sent', 'delivered', 'failed', 'suppressed')),
    -- Why it was not sent. 'preference' and 'quiet_hours' are refusals the
    -- platform made; 'provider_rejected' is one it was handed.
    suppressed_reason text,
    provider    text,
    provider_ref text,
    failure_code text,
    attempts    smallint NOT NULL DEFAULT 0,

    -- Caller-supplied deduplication key. The dispatcher is at-least-once — an
    -- outbox relay always is — and a customer who receives the same OTP three
    -- times has been given three reasons to distrust the product.
    dedupe_key  text,

    created_at  timestamptz NOT NULL DEFAULT now(),
    sent_at     timestamptz,
    delivered_at timestamptz,
    read_at     timestamptz,

    PRIMARY KEY (id, created_at),

    CONSTRAINT notifications_suppressed_has_reason
        CHECK (status <> 'suppressed' OR suppressed_reason IS NOT NULL)
) PARTITION BY RANGE (created_at);

-- Unique per partition, which is the only kind Postgres will give a partitioned
-- table without the partition key in the constraint. That is sufficient: the
-- duplicates being prevented arrive seconds apart, from a relay retrying the
-- same outbox row, and land in the same month.
CREATE UNIQUE INDEX notifications_dedupe_idx
    ON notifications (dedupe_key, created_at) WHERE dedupe_key IS NOT NULL;
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_pending_idx ON notifications (created_at)
    WHERE status = 'pending';

CREATE OR REPLACE FUNCTION create_notification_partition(p_month date)
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
    v_start date := date_trunc('month', p_month)::date;
    v_end   date := (date_trunc('month', p_month) + interval '1 month')::date;
    v_name  text := 'notifications_' || to_char(v_start, 'YYYYMM');
BEGIN
    IF to_regclass(v_name) IS NOT NULL THEN
        RETURN v_name;
    END IF;
    EXECUTE format(
        'CREATE TABLE %I PARTITION OF notifications FOR VALUES FROM (%L) TO (%L)',
        v_name, v_start, v_end);
    RETURN v_name;
END;
$$;

CREATE TABLE notifications_default PARTITION OF notifications DEFAULT;


-- WhatsApp Business 24-hour session windows.
--
-- PLAN.md §7 calls WhatsApp the primary channel in the GCC, and its rule shapes
-- the dispatcher rather than merely being logged by it: inside 24 hours of a
-- user-initiated message you may send free-form text; outside it you may send
-- only a pre-approved template, and the provider charges per conversation.
--
-- Modelling the window explicitly is what lets the sender pick a channel
-- honestly. Without this table the choice is "try WhatsApp, fall back to SMS on
-- rejection", which means the fallback path is exercised for the first time
-- during whatever incident made it matter.
CREATE TABLE whatsapp_sessions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    phone      text NOT NULL,
    opened_at  timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    -- The provider's conversation id, which is the unit it bills.
    conversation_ref text,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT whatsapp_sessions_window CHECK (expires_at > opened_at)
);

CREATE INDEX whatsapp_sessions_open_idx ON whatsapp_sessions (user_id, expires_at DESC);


-- ─────────────────────────────────────────────────────────────────────────────
-- 3. Tax
-- ─────────────────────────────────────────────────────────────────────────────

-- Rates, by jurisdiction and category, valid over a date range.
--
-- Date-versioned because invoices must reprint correctly. When a rate changes,
-- an invoice issued before the change still shows the old rate forever, and a
-- system that stores only "the current rate" silently rewrites its own history
-- the moment the number moves. The UAE went 0% → 5% in 2018; assuming it will
-- never move again is a bet with no upside.
--
-- The exclusion constraint is what makes the lookup total: for a given
-- jurisdiction and category, no two rows may cover the same day, so "the rate
-- on the date of supply" always returns exactly zero or one row and the
-- ambiguous case cannot be created.
CREATE TABLE tax_rates (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    country     char(2) NOT NULL,
    -- Emirate or region, for the day a jurisdiction diverges from its country.
    -- NULL means country-wide, which is the case for both UAE VAT and KSA VAT
    -- today.
    region      text,
    category    text NOT NULL
                     CHECK (category IN ('standard', 'zero_rated', 'exempt', 'out_of_scope')),
    rate_bp     int NOT NULL CHECK (rate_bp BETWEEN 0 AND 10000),
    valid       daterange NOT NULL,
    -- The legal name of the tax as it must appear on an invoice. 'VAT' in both
    -- markets today, and not a constant because it is not one everywhere.
    label       text NOT NULL DEFAULT 'VAT',
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tax_rates_no_overlap EXCLUDE USING gist (
        country WITH =,
        coalesce(region, '') WITH =,
        category WITH =,
        valid WITH &&
    ),
    CONSTRAINT tax_rates_zero_is_zero
        CHECK (category = 'standard' OR rate_bp = 0)
);

-- Seeded, because these are facts about the world rather than configuration.
-- Upper bounds are open: a rate with an end date nobody set would silently
-- stop applying.
INSERT INTO tax_rates (country, category, rate_bp, valid, label) VALUES
    ('AE', 'standard',     500, daterange('2018-01-01', NULL), 'VAT'),
    ('AE', 'zero_rated',     0, daterange('2018-01-01', NULL), 'VAT'),
    ('AE', 'exempt',         0, daterange('2018-01-01', NULL), 'VAT'),
    ('SA', 'standard',    1500, daterange('2020-07-01', NULL), 'VAT'),
    ('SA', 'standard',     500, daterange('2018-01-01', '2020-07-01'), 'VAT'),
    ('SA', 'zero_rated',     0, daterange('2018-01-01', NULL), 'VAT'),
    ('SA', 'exempt',         0, daterange('2018-01-01', NULL), 'VAT');


-- Who is registered for tax, and under which number.
--
-- A tax invoice is invalid without the supplier's registration number, and
-- which number belongs on it depends on an open decision: if the vendor is the
-- supplier of record the vendor's TRN goes on the invoice, and if the platform
-- is a deemed supplier the platform's does (docs/README.md decision #4).
--
-- This table takes no position on that. It records registrations for both
-- owner kinds; the invoicing module picks one, in one function, which is where
-- the decision will land when it is made.
CREATE TABLE tax_registrations (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_type   text NOT NULL CHECK (owner_type IN ('platform', 'vendor')),
    owner_id     uuid,
    country      char(2) NOT NULL,
    -- UAE TRN is 15 digits; KSA VAT number is 15 digits starting and ending
    -- with 3. Shape-checked rather than merely non-empty, because an invoice
    -- carrying a malformed TRN is rejected by the buyer's accountant weeks
    -- later rather than by anything here.
    registration_no text NOT NULL,
    legal_name   text NOT NULL,
    address_line text,
    valid_from   date NOT NULL,
    valid_to     date,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tax_registrations_owner
        CHECK ((owner_type = 'platform' AND owner_id IS NULL)
            OR (owner_type = 'vendor'   AND owner_id IS NOT NULL)),
    CONSTRAINT tax_registrations_shape
        CHECK (registration_no ~ '^[0-9]{15}$'),
    CONSTRAINT tax_registrations_ksa_shape
        CHECK (country <> 'SA' OR registration_no ~ '^3[0-9]{13}3$')
);

CREATE UNIQUE INDEX tax_registrations_current_idx
    ON tax_registrations (owner_type, coalesce(owner_id, '00000000-0000-0000-0000-000000000000'::uuid), country)
    WHERE valid_to IS NULL;

ALTER TABLE tax_registrations
    ADD CONSTRAINT tax_registrations_vendor_fk
    FOREIGN KEY (owner_id) REFERENCES vendors (id) ON DELETE CASCADE;


-- ─────────────────────────────────────────────────────────────────────────────
-- 4. Invoicing
-- ─────────────────────────────────────────────────────────────────────────────

-- The number generator.
--
-- Legal invoice numbering must be sequential and gapless within a series, which
-- is the one requirement in this schema that a UUID cannot satisfy and a
-- Postgres sequence cannot either — sequences skip on rollback by design, and a
-- skipped number is a gap an auditor will ask about.
--
-- So: one row per series, taken with SELECT ... FOR UPDATE, incremented inside
-- the same transaction that inserts the invoice. That serialises invoice
-- creation per series and it is the correct trade. Invoices are issued once per
-- order rather than per request, the lock is held for microseconds, and the
-- alternative — a gap in a tax sequence — is a finding rather than a
-- performance problem.
CREATE TABLE invoice_series (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    country     char(2) NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('tax_invoice', 'credit_note', 'simplified')),
    year        int NOT NULL,
    prefix      text NOT NULL,
    next_number bigint NOT NULL DEFAULT 1 CHECK (next_number > 0),
    created_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (country, kind, year)
);


CREATE TABLE invoices (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    series_id   uuid NOT NULL REFERENCES invoice_series (id),
    -- The rendered legal number, e.g. INV-AE-2026-000117. Unique platform-wide
    -- rather than per series so a number can never be shown for two documents.
    number      text NOT NULL UNIQUE,
    sequence_no bigint NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('tax_invoice', 'credit_note', 'simplified')),

    -- Exactly one subject, the same shape `payments` took in Phase 6 when
    -- listing fees arrived. An invoice is always *for* something, and a
    -- nullable-everything row is one that can be created for nothing.
    order_id              uuid REFERENCES orders (id),
    subscription_cycle_id uuid REFERENCES subscription_cycles (id),
    package_purchase_id   uuid REFERENCES package_purchases (id),
    payout_statement_id   uuid REFERENCES payout_statements (id),

    -- Supplier and buyer, snapshotted.
    --
    -- Snapshotted rather than joined for the same reason the notification body
    -- is stored: an invoice is a legal document describing a transaction on a
    -- date, and a vendor who later changes their legal name has not changed
    -- what the invoice said. Reprinting from live rows would quietly forge it.
    supplier_type text NOT NULL CHECK (supplier_type IN ('platform', 'vendor')),
    supplier_id   uuid REFERENCES vendors (id),
    supplier_name text NOT NULL,
    supplier_trn  text,
    buyer_name    text,
    buyer_trn     text,
    buyer_user_id uuid REFERENCES users (id),
    buyer_address text,

    country       char(2) NOT NULL,
    currency      char(3) NOT NULL DEFAULT 'AED',
    -- Totals in minor units. net + tax = gross, enforced, because an invoice
    -- whose own arithmetic does not close is worse than no invoice.
    net_minor     bigint NOT NULL,
    tax_minor     bigint NOT NULL,
    gross_minor   bigint NOT NULL,

    -- Date of supply, which is not the date of issue and is the one the tax
    -- rate is chosen by.
    supply_date   date NOT NULL,
    issued_at     timestamptz NOT NULL DEFAULT now(),

    status        text NOT NULL DEFAULT 'issued'
                       CHECK (status IN ('issued', 'cancelled', 'superseded')),
    -- A credit note points at what it corrects. There is no UPDATE path to an
    -- issued invoice — corrections are new documents, always.
    corrects_invoice_id uuid REFERENCES invoices (id),

    -- Rendered PDF in object storage. Nullable: the row is the invoice, the PDF
    -- is a rendering of it, and generation failing must not lose the document.
    pdf_ref       text,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT invoices_exactly_one_subject CHECK (
        num_nonnulls(order_id, subscription_cycle_id, package_purchase_id, payout_statement_id) = 1),
    CONSTRAINT invoices_totals_close CHECK (net_minor + tax_minor = gross_minor),
    CONSTRAINT invoices_credit_note_corrects
        CHECK (kind <> 'credit_note' OR corrects_invoice_id IS NOT NULL),
    CONSTRAINT invoices_vendor_supplier
        CHECK ((supplier_type = 'vendor' AND supplier_id IS NOT NULL)
            OR (supplier_type = 'platform' AND supplier_id IS NULL))
);

CREATE UNIQUE INDEX invoices_series_seq_idx ON invoices (series_id, sequence_no);
CREATE INDEX invoices_buyer_idx ON invoices (buyer_user_id, issued_at DESC);
CREATE INDEX invoices_supplier_idx ON invoices (supplier_id, issued_at DESC)
    WHERE supplier_id IS NOT NULL;
-- One tax invoice per order. A second is a duplicate rather than a correction,
-- and duplicates are how a customer ends up able to claim the same input tax
-- twice.
CREATE UNIQUE INDEX invoices_one_per_order_idx ON invoices (order_id)
    WHERE order_id IS NOT NULL AND kind = 'tax_invoice' AND status = 'issued';


CREATE TABLE invoice_lines (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id  uuid NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    line_no     smallint NOT NULL,

    -- Snapshot, in both locales where one exists. An invoice reprinted after a
    -- dish is renamed must still describe what was bought.
    description    text NOT NULL,
    description_ar text,

    quantity        numeric(12,3) NOT NULL CHECK (quantity > 0),
    unit_net_minor  bigint NOT NULL,
    net_minor       bigint NOT NULL,

    -- The rate as applied, not a reference to the rate table. Same reasoning as
    -- everything else here: the rate row could be corrected tomorrow and this
    -- document must not change.
    tax_category    text NOT NULL,
    tax_rate_bp     int NOT NULL CHECK (tax_rate_bp BETWEEN 0 AND 10000),
    tax_minor       bigint NOT NULL,
    gross_minor     bigint NOT NULL,

    UNIQUE (invoice_id, line_no),
    CONSTRAINT invoice_lines_totals_close CHECK (net_minor + tax_minor = gross_minor)
);


-- KSA e-invoicing (ZATCA Phase 2 / Fatoora).
--
-- PLAN.md §7 flags this as weeks of work and docs/README.md decision #6 — "KSA
-- in year one?" — is still open. What is built here is the chain and the
-- submission record, not the cryptography: signing needs a CSID issued by ZATCA
-- against a real commercial registration, which cannot be obtained
-- speculatively.
--
-- The part that had to be built now rather than later is `previous_hash`. Phase
-- 2 e-invoicing requires each invoice's hash to include the hash of the one
-- before it, so the chain is only sound if it has been unbroken from the first
-- KSA invoice ever issued. Retrofitting a chain onto invoices that were issued
-- without one is not possible — you would be re-issuing them.
CREATE TABLE zatca_submissions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id  uuid NOT NULL UNIQUE REFERENCES invoices (id),

    -- Position in the chain, per device/series. Gapless and unique, for the
    -- same reason the invoice number is.
    icv         bigint NOT NULL,
    -- Base64 SHA-256 of the canonicalised invoice XML.
    invoice_hash text NOT NULL,
    -- The previous submission's invoice_hash. The genesis row carries ZATCA's
    -- documented zero-hash rather than NULL, so the verification code has one
    -- path instead of two.
    previous_hash text NOT NULL,

    -- The TLV-encoded QR that must be printed on the invoice.
    qr_tlv      text,

    status      text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'reported', 'cleared', 'rejected')),
    -- Simplified invoices are reported within 24h; standard invoices must be
    -- cleared *before* being given to the buyer. Which one applies changes when
    -- the PDF may be sent, so it is a column rather than a derived guess.
    flow        text NOT NULL CHECK (flow IN ('reporting', 'clearance')),

    submitted_at timestamptz,
    cleared_at   timestamptz,
    -- ZATCA's warnings are not failures but they are how you find out the
    -- integration is drifting before it starts rejecting.
    response_warnings jsonb,
    response_errors   jsonb,
    xml_ref     text,
    created_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (icv)
);

CREATE INDEX zatca_submissions_pending_idx ON zatca_submissions (created_at)
    WHERE status IN ('pending', 'rejected');


-- ─────────────────────────────────────────────────────────────────────────────
-- 5. KYC and payout accounts
-- ─────────────────────────────────────────────────────────────────────────────

-- One case per subject per tier.
--
-- PLAN.md §7: "Trade licence, Emirates ID / national ID, IBAN verification
-- before first payout." The load-bearing words are *before first payout* — this
-- is a gate on money leaving the platform, not a profile-completeness score,
-- and `payouts.Issue` refuses without it.
--
-- Subject is a vendor, a driver or a user, because all three take money out:
-- vendors through statements, drivers through cash remittance settlement, and
-- private sellers through nothing yet but through refunds the moment listing
-- fees can be refunded to a card that has since expired.
CREATE TABLE kyc_cases (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_type text NOT NULL CHECK (subject_type IN ('vendor', 'user', 'driver')),
    subject_id   uuid NOT NULL,

    -- Tiers exist because the requirement is not uniform. A private seller
    -- listing one car needs identity; a dealer taking payouts needs a trade
    -- licence and a bank account in the same legal name.
    tier         text NOT NULL DEFAULT 'basic'
                      CHECK (tier IN ('basic', 'verified', 'enhanced')),

    status       text NOT NULL DEFAULT 'draft'
                      CHECK (status IN ('draft', 'submitted', 'in_review',
                                         'approved', 'rejected', 'expired')),
    -- Approval is not permanent. A trade licence expires, and an approved case
    -- whose evidence has lapsed must stop authorising payouts on its own rather
    -- than waiting for someone to notice.
    approved_at  timestamptz,
    expires_at   timestamptz,

    reviewed_by  uuid REFERENCES users (id),
    reviewed_at  timestamptz,
    -- Required on rejection. A rejected vendor who is told only "rejected"
    -- resubmits the same documents, and the queue gets the same case twice.
    rejection_reason text,

    -- The provider's reference, when an external KYC vendor is used. Nullable
    -- because manual review is the launch answer.
    provider     text,
    provider_ref text,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT kyc_cases_rejection_has_reason
        CHECK (status <> 'rejected' OR rejection_reason IS NOT NULL),
    CONSTRAINT kyc_cases_approved_has_timestamp
        CHECK (status <> 'approved' OR approved_at IS NOT NULL)
);

CREATE UNIQUE INDEX kyc_cases_open_idx ON kyc_cases (subject_type, subject_id, tier)
    WHERE status IN ('draft', 'submitted', 'in_review', 'approved');
CREATE INDEX kyc_cases_queue_idx ON kyc_cases (status, created_at)
    WHERE status IN ('submitted', 'in_review');
CREATE INDEX kyc_cases_expiring_idx ON kyc_cases (expires_at)
    WHERE status = 'approved' AND expires_at IS NOT NULL;

CREATE TRIGGER kyc_cases_set_updated_at BEFORE UPDATE ON kyc_cases
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE kyc_documents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id     uuid NOT NULL REFERENCES kyc_cases (id) ON DELETE CASCADE,
    kind        text NOT NULL
                     CHECK (kind IN ('trade_licence', 'emirates_id', 'national_id',
                                      'passport', 'vat_certificate', 'bank_letter',
                                      'authorised_signatory', 'ownership_proof')),

    -- A reference into object storage, never the document.
    --
    -- The same rule seller_verifications and driver_documents took, and it is
    -- worth restating because this is the table where the temptation is
    -- strongest: a scan of an Emirates ID is the highest-value object in this
    -- system to an attacker, and PDPL treats it accordingly. Postgres backups
    -- go to more places than object-storage buckets do.
    document_ref text,
    -- Enough to answer "which card was this" in support without holding the
    -- number. Four digits is not identifying on its own.
    number_last4 char(4),
    issued_on    date,
    expires_on   date,

    status      text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'verified', 'rejected')),
    rejection_reason text,
    verified_by uuid REFERENCES users (id),
    verified_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX kyc_documents_case_idx ON kyc_documents (case_id);
CREATE INDEX kyc_documents_expiring_idx ON kyc_documents (expires_on)
    WHERE status = 'verified' AND expires_on IS NOT NULL;


-- Where the money goes.
--
-- Separate from kyc_cases because the lifecycles differ: a vendor keeps their
-- identity and changes their bank. Every change re-enters verification, because
-- "vendor changed their IBAN" is the single most common payout-fraud pattern
-- there is and an unverified new account is exactly what the attacker wants
-- paid.
CREATE TABLE payout_accounts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_type   text NOT NULL CHECK (owner_type IN ('vendor', 'driver')),
    owner_id     uuid NOT NULL,

    -- Stored in compact form (no spaces, upper case). Mod-97 checked in Go
    -- before insert; the CHECK here is the shape only, because IBAN length is
    -- country-specific and encoding the full table in a constraint would mean a
    -- migration every time a country is added.
    iban         text NOT NULL,
    bank_name    text,
    -- Must match the legal name on the KYC case. Enforced by the review step
    -- rather than by SQL — the comparison is fuzzy and a human does it — but
    -- stored so the comparison is possible at all.
    holder_name  text NOT NULL,
    currency     char(3) NOT NULL DEFAULT 'AED',
    country      char(2) NOT NULL,

    status       text NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'verified', 'rejected', 'retired')),
    verified_by  uuid REFERENCES users (id),
    verified_at  timestamptz,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT payout_accounts_iban_shape CHECK (iban ~ '^[A-Z]{2}[0-9]{2}[A-Z0-9]{10,30}$')
);

-- One live account per owner. Adding a second retires the first; there is never
-- a moment where a payout run has to choose.
CREATE UNIQUE INDEX payout_accounts_active_idx ON payout_accounts (owner_type, owner_id)
    WHERE status IN ('pending', 'verified');

CREATE TRIGGER payout_accounts_set_updated_at BEFORE UPDATE ON payout_accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- ─────────────────────────────────────────────────────────────────────────────
-- 6. Trust & safety scale-up
-- ─────────────────────────────────────────────────────────────────────────────

-- PLAN-VIDEO-FIRST §6 lists four things: auto-moderation, a review queue with
-- local context, in-feed reporting, and "author strikes, and appeals". The
-- first three shipped in Phase 2. The last two are here, and they are the ones
-- that turn moderation from a per-item decision into a policy an author
-- experiences.

CREATE TABLE author_strikes (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    author_type  text NOT NULL CHECK (author_type IN ('vendor', 'user')),
    author_id    uuid NOT NULL,

    -- What was struck. Reels, comments, chat messages and listings all reach
    -- the same author record, which is the point: an author who is careful in
    -- reels and abusive in chat is one author.
    subject_type text NOT NULL
                      CHECK (subject_type IN ('reel', 'comment', 'message', 'listing')),
    subject_id   uuid NOT NULL,

    severity     text NOT NULL CHECK (severity IN ('minor', 'major', 'severe')),
    reason       text NOT NULL,
    policy_code  text NOT NULL,

    issued_by    uuid REFERENCES users (id),
    issued_at    timestamptz NOT NULL DEFAULT now(),

    -- Strikes expire.
    --
    -- A permanent record makes the ladder a one-way ratchet: an author who
    -- posted something wrong once, two years ago, is treated for the rest of
    -- their life like one who did it last week. Decay is what makes escalation
    -- proportionate, and it is a date rather than a decay curve because an
    -- author must be able to be told when it lifts.
    expires_at   timestamptz NOT NULL,

    -- Set when an appeal succeeds. Voided strikes stay in the table — the
    -- record that a decision was made and reversed is exactly what a review of
    -- the moderation function needs.
    voided_at    timestamptz,
    voided_reason text,

    CONSTRAINT author_strikes_expiry CHECK (expires_at > issued_at)
);

CREATE INDEX author_strikes_active_idx ON author_strikes (author_type, author_id, expires_at)
    WHERE voided_at IS NULL;
CREATE INDEX author_strikes_subject_idx ON author_strikes (subject_type, subject_id);


-- Appeals.
--
-- The constraint worth reading is `content_appeals_not_self_reviewed`. An
-- appeal reviewed by the person whose decision is being appealed is not an
-- appeal, and in a moderation team of four the same person will be the obvious
-- next reviewer most of the time. Making the database refuse it costs one
-- column and removes the failure mode entirely.
CREATE TABLE content_appeals (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_type  text NOT NULL
                       CHECK (subject_type IN ('reel', 'comment', 'message', 'listing', 'strike', 'account')),
    subject_id    uuid NOT NULL,

    appellant_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    statement     text NOT NULL,

    -- Who made the decision being appealed. Copied in at creation because the
    -- original decision rows live in four different tables and a foreign key
    -- cannot point at all of them.
    original_reviewer_id uuid REFERENCES users (id),

    status        text NOT NULL DEFAULT 'open'
                       CHECK (status IN ('open', 'in_review', 'upheld', 'overturned', 'withdrawn')),
    reviewer_id   uuid REFERENCES users (id),
    reviewed_at   timestamptz,
    outcome_note  text,

    -- The SLA clock. An appeals process with no deadline is a way of saying no
    -- slowly, and the app stores do treat "users cannot appeal" as a policy
    -- violation rather than a product choice.
    due_at        timestamptz NOT NULL,

    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT content_appeals_not_self_reviewed
        CHECK (reviewer_id IS NULL OR original_reviewer_id IS NULL
               OR reviewer_id <> original_reviewer_id),
    CONSTRAINT content_appeals_decided_has_reviewer
        CHECK (status NOT IN ('upheld', 'overturned') OR reviewer_id IS NOT NULL)
);

-- One open appeal per subject per appellant. Re-appealing the same decision is
-- a support conversation, not a second queue item.
CREATE UNIQUE INDEX content_appeals_open_idx
    ON content_appeals (subject_type, subject_id, appellant_id)
    WHERE status IN ('open', 'in_review');
CREATE INDEX content_appeals_queue_idx ON content_appeals (status, due_at)
    WHERE status IN ('open', 'in_review');


-- Queue claims.
--
-- Two moderators judging the same reel is wasted capacity; worse, two
-- moderators reaching different verdicts on the same reel is an inconsistency
-- the author sees. A claim with an expiry solves both, and the expiry is what
-- makes it safe — a moderator who closes their laptop mid-review must not
-- remove an item from the queue permanently.
CREATE TABLE moderation_assignments (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    queue        text NOT NULL
                      CHECK (queue IN ('reel', 'comment', 'message', 'listing', 'fraud', 'appeal', 'kyc')),
    subject_id   uuid NOT NULL,
    reviewer_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    claimed_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    released_at  timestamptz,
    resolution   text CHECK (resolution IN ('decided', 'released', 'expired')),

    CONSTRAINT moderation_assignments_expiry CHECK (expires_at > claimed_at)
);

-- One live claim per item. Partial-unique rather than a plain unique so the
-- history of who looked at an item survives.
CREATE UNIQUE INDEX moderation_assignments_live_idx ON moderation_assignments (queue, subject_id)
    WHERE released_at IS NULL;
CREATE INDEX moderation_assignments_reviewer_idx ON moderation_assignments (reviewer_id, claimed_at DESC);


-- Reviewer locale competence.
--
-- PLAN-VIDEO-FIRST §6 asks for "a human review queue with local context —
-- GCC standards on nudity, alcohol, gambling and religious/political content
-- are stricter than the US/EU defaults these services ship with."
--
-- A queue that routes Arabic-language content to a reviewer who does not read
-- Arabic produces the under-blocking that paragraph warns about, and no amount
-- of classifier tuning fixes it. Routing needs to know who can read what.
ALTER TABLE platform_admins
    ADD COLUMN locales text[] NOT NULL DEFAULT ARRAY['en'],
    ADD COLUMN queues  text[] NOT NULL DEFAULT ARRAY['reel', 'comment'],
    ADD COLUMN is_active boolean NOT NULL DEFAULT true;


-- ─────────────────────────────────────────────────────────────────────────────
-- 7. Engine changes Phase 8 forces
-- ─────────────────────────────────────────────────────────────────────────────

-- Fulfillments learn whether their OTP was actually delivered.
--
-- Phase 7 issued the OTP and hashed it and had nowhere to send it, which made
-- cash-on-delivery uncompleteable — docs/README.md decision #8. The notify
-- module closes that, and this column is how the failure stays visible: a
-- driver standing at a door whose customer never received a code needs the
-- dispatcher to know that is what happened, not to be told the code is wrong.
ALTER TABLE fulfillments
    ADD COLUMN otp_notification_id uuid,
    ADD COLUMN otp_sent_at timestamptz;

-- Users get a notification locale distinct from their UI locale.
--
-- These genuinely differ. A resident who uses the app in English may want SMS
-- in Arabic because that is what their household reads, and the person who
-- receives a delivery OTP is frequently not the person who placed the order.
ALTER TABLE users
    ADD COLUMN notification_locale locale_code;


COMMENT ON TABLE tax_rates IS
    'Date-versioned. An invoice issued last year must still print last year''s rate.';
COMMENT ON TABLE invoice_series IS
    'Gapless numbering. Locked per series on issue — a gap is an audit finding, not a performance problem.';
COMMENT ON TABLE zatca_submissions IS
    'previous_hash chains every KSA invoice to the one before it. The chain cannot be retrofitted.';
COMMENT ON TABLE kyc_documents IS
    'document_ref only. A scan of an Emirates ID has no business in Postgres.';
COMMENT ON TABLE author_strikes IS
    'Strikes expire. Without decay the ladder is a one-way ratchet.';
COMMENT ON CONSTRAINT content_appeals_not_self_reviewed ON content_appeals IS
    'An appeal reviewed by the original decider is not an appeal.';
COMMENT ON CONSTRAINT notification_preferences_only_optional_categories ON notification_preferences IS
    'Marketing can be refused. A delivery OTP cannot.';
