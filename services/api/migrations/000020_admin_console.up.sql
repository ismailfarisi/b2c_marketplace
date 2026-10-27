-- Admin console — permissions, flags and settings.
--
-- Implements PLAN-ADMIN-CONSOLE §3.5. The console eleven phases never budgeted
-- needs one thing before it needs any screen: **a permission model finer than
-- "staff"**.
--
-- Today `platform_admins.role` is an enum of three, and `RequireStaff` is a
-- boolean. That was correct while the only staff surface was a moderation queue
-- — the worst a moderator could do was judge a reel. It stops being correct the
-- moment a detail page exists, because the first one hands every moderator a
-- customer's home address.
--
-- Coarse permissions cost nothing before there is data and are a migration
-- afterwards, which is why this is in the console's first phase rather than
-- deferred to the phase that needs it most.
--
-- 6 tables.

-- ─────────────────────────────────────────────────────────────────────────────
-- 1. Permissions
-- ─────────────────────────────────────────────────────────────────────────────

-- The closed set of things staff can be granted.
--
-- A table rather than an enum, because a new permission at launch should be a
-- row. An enum means a migration, a deploy and a code change to answer "the
-- finance contractor needs to see payouts and nothing else" — which is a
-- question that gets asked in week one and answered with superadmin if the
-- honest answer is expensive.
--
-- Seeded below and rarely extended. The *grants* are data; the permissions
-- themselves are a vocabulary the code knows, so each row's key appears in Go
-- as a constant and the seed is the contract between them.
CREATE TABLE admin_permissions (
    key         text PRIMARY KEY,
    description text NOT NULL,
    -- Marks permissions that expose personal data. Reads through these are
    -- logged to audit_log — see §3.4 of the plan. Not every permission needs
    -- that: an audit log that records everything is one nobody reads.
    is_sensitive boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO admin_permissions (key, description, is_sensitive) VALUES
    ('content.review',   'Reel, comment, message and listing queues and detail', false),
    ('content.strike',   'Issue and void strikes; decide appeals', false),
    ('commerce.read',    'Orders, subscriptions and fulfillments, without contact details', false),
    ('commerce.contact', 'Recipient name, phone and address on those records', true),
    ('commerce.write',   'Cancel, reschedule and credit an order', false),
    ('money.read',       'Ledger, payouts, invoices and cash', false),
    ('money.write',      'Refunds, credits, statement issue and write-offs', false),
    ('identity.review',  'KYC cases and documents; bank account verification', true),
    ('config.write',     'Templates, rate cards, tax rates, packages and flags', false),
    ('staff.admin',      'Grant and revoke every permission above', false),
    ('health.read',      'Job loops, partitions and delivery rates', false);


-- Named bundles.
--
-- Roles are data so that "the launch team needs a role that can see orders and
-- refund but not touch KYC" is a row rather than a release.
CREATE TABLE admin_roles (
    key         text PRIMARY KEY,
    name        text NOT NULL,
    description text NOT NULL,
    -- Built-in roles cannot be deleted, because deleting the one every staff
    -- member holds is a self-inflicted outage with no undo.
    is_builtin  boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO admin_roles (key, name, description, is_builtin) VALUES
    ('moderator', 'Content moderator',
     'Judges content. Sees no customer contact details, no money, no identity documents.', true),
    ('support', 'Support agent',
     'Answers the phone. Reads orders with contact details and can credit or reschedule; cannot refund.', true),
    ('finance', 'Finance',
     'Ledger, payouts, invoices and refunds. No content, no identity documents.', true),
    ('compliance', 'Compliance reviewer',
     'Identity checks and bank accounts. Deliberately holds no money permission — approving an account and then paying it is payout fraud in two clicks.', true),
    ('ops', 'Operations',
     'Content, commerce and health. The day-to-day role.', true),
    ('superadmin', 'Superadmin',
     'Everything, including granting it. Should be two people.', true);


CREATE TABLE admin_role_permissions (
    role_key       text NOT NULL REFERENCES admin_roles (key) ON DELETE CASCADE,
    permission_key text NOT NULL REFERENCES admin_permissions (key) ON DELETE CASCADE,
    PRIMARY KEY (role_key, permission_key)
);

INSERT INTO admin_role_permissions (role_key, permission_key) VALUES
    ('moderator',  'content.review'),
    ('moderator',  'content.strike'),

    ('support',    'commerce.read'),
    ('support',    'commerce.contact'),
    ('support',    'commerce.write'),
    ('support',    'content.review'),

    ('finance',    'money.read'),
    ('finance',    'money.write'),
    ('finance',    'commerce.read'),

    -- No money permission, deliberately. See the role's description.
    ('compliance', 'identity.review'),
    ('compliance', 'commerce.read'),

    ('ops',        'content.review'),
    ('ops',        'content.strike'),
    ('ops',        'commerce.read'),
    ('ops',        'commerce.contact'),
    ('ops',        'commerce.write'),
    ('ops',        'money.read'),
    ('ops',        'health.read'),

    ('superadmin', 'content.review'),
    ('superadmin', 'content.strike'),
    ('superadmin', 'commerce.read'),
    ('superadmin', 'commerce.contact'),
    ('superadmin', 'commerce.write'),
    ('superadmin', 'money.read'),
    ('superadmin', 'money.write'),
    ('superadmin', 'identity.review'),
    ('superadmin', 'config.write'),
    ('superadmin', 'staff.admin'),
    ('superadmin', 'health.read');


-- Who holds what.
--
-- A row per (user, role) rather than a column on `platform_admins`, because one
-- person legitimately holds two: the founder who is both ops and finance in
-- month one. Modelling that as a single enum forces a `superadmin` grant, which
-- is how everyone ends up a superadmin by week three.
CREATE TABLE admin_grants (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_key   text NOT NULL REFERENCES admin_roles (key) ON DELETE RESTRICT,

    granted_by uuid REFERENCES users (id),
    granted_at timestamptz NOT NULL DEFAULT now(),
    -- Time-boxed access. A contractor, an incident, a covering shift — all of
    -- which are granted "temporarily" and revoked never, unless the revocation
    -- is the default.
    expires_at timestamptz,
    reason     text,

    PRIMARY KEY (user_id, role_key)
);

-- No `WHERE expires_at > now()` predicate: `now()` is not IMMUTABLE and
-- Postgres refuses it in an index predicate. Expiry is filtered in the query
-- instead, which the (user_id, expires_at) ordering serves just as well.
CREATE INDEX admin_grants_user_idx ON admin_grants (user_id, expires_at);


-- Existing staff keep working.
--
-- The enum on `platform_admins` maps one-to-one onto the new roles, so nobody
-- loses access at deploy. `platform_admins` stays as the "is this person staff
-- at all" record — the grants table answers "to do what".
INSERT INTO admin_grants (user_id, role_key, reason)
SELECT user_id,
       CASE role WHEN 'superadmin' THEN 'superadmin'
                 WHEN 'ops'        THEN 'ops'
                 ELSE 'moderator' END,
       'migrated from platform_admins.role'
  FROM platform_admins
ON CONFLICT DO NOTHING;


-- ─────────────────────────────────────────────────────────────────────────────
-- 2. Runtime configuration
-- ─────────────────────────────────────────────────────────────────────────────

-- DATA-MODEL §14 numbers these 111 and 112 and no phase has needed them until
-- now. The console needs both, because "edit this with psql in production" is
-- the thing Phase D exists to end.

CREATE TABLE feature_flags (
    key         text PRIMARY KEY,
    description text NOT NULL,
    is_enabled  boolean NOT NULL DEFAULT false,

    -- Percentage rollout, 0–100. Bucketed on a stable hash of the subject so a
    -- user who is in the treatment stays in it — a flag that re-rolls per
    -- request is not an experiment, it is a flicker.
    rollout_pct smallint NOT NULL DEFAULT 0
                    CHECK (rollout_pct BETWEEN 0 AND 100),
    -- Named allow-list, evaluated before the percentage. The list is how a
    -- vendor gets early access without being in a bucket.
    enabled_for uuid[],

    updated_by  uuid REFERENCES users (id),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    created_at  timestamptz NOT NULL DEFAULT now()
);


CREATE TABLE app_settings (
    key         text PRIMARY KEY,
    value       jsonb NOT NULL,
    description text NOT NULL,
    -- Settings that move money or change what customers are charged need a
    -- second pair of eyes. The console refuses to write these without a reason
    -- and records both in admin_actions.
    is_sensitive boolean NOT NULL DEFAULT false,

    updated_by  uuid REFERENCES users (id),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    created_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO app_settings (key, value, description, is_sensitive) VALUES
    ('commission.default_bp', '1500'::jsonb,
     'Platform commission in basis points. Hard-coded in cmd/api until docs/README decision #4 is answered; this row is where it moves to.', true),
    ('feed.page_size', '20'::jsonb,
     'Items per feed page.', false),
    ('moderation.reel_sla_minutes', '120'::jsonb,
     'The promise made to authors, mirrored by admin.queueSLA.', false),
    ('support.contact_email', '"support@example.ae"'::jsonb,
     'Shown on invoices and failed-delivery notifications.', false);


-- ─────────────────────────────────────────────────────────────────────────────
-- 3. Making the audit trail readable
-- ─────────────────────────────────────────────────────────────────────────────

-- `admin_actions` has been written correctly since Phase 7 and never read back.
-- The console's timeline reads it per subject, and per actor for the staff
-- page. Both indexes it needs already exist — 000017 created them with the
-- table — so creating them again here failed the migration.

-- `audit_log` gains the same, plus the index the PII question is actually asked
-- with: "who looked at this customer".
-- audit_log_target_idx already exists from 000010.
CREATE INDEX audit_log_admin_actor_idx
    ON audit_log (actor_id, occurred_at DESC)
    WHERE actor_type = 'admin';


-- Search needs to find an order by its number and a listing by its VIN without
-- scanning. Both are exact-match lookups on columns with no index today.
CREATE INDEX orders_order_no_idx ON orders (order_no);
CREATE INDEX payout_statements_ref_idx ON payout_statements (reference_no);
CREATE INDEX disputes_ref_idx ON disputes (reference_no);
CREATE INDEX boost_campaigns_ref_idx ON boost_campaigns (reference_no);
CREATE INDEX vehicle_listings_ref_idx ON vehicle_listings (reference_no);

-- Phone lookup. `users.phone` is unique-ish by convention and not indexed;
-- support types a phone number into search more than anything else.
CREATE INDEX users_phone_idx ON users (phone);
-- The last four digits of an IBAN, which is how a vendor identifies their
-- account on the phone without reading out twenty-three characters.
CREATE INDEX payout_accounts_iban_tail_idx ON payout_accounts (right(iban, 4));


COMMENT ON TABLE admin_permissions IS
    'The vocabulary. Each key appears as a constant in Go; this table is the contract between them.';
COMMENT ON TABLE admin_grants IS
    'Who holds what, optionally time-boxed. A row per (user, role) because one person legitimately holds two.';
COMMENT ON COLUMN admin_permissions.is_sensitive IS
    'Reads through this permission are logged to audit_log. Not every permission — a log that records everything is one nobody reads.';
