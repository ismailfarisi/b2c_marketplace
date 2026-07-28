-- Double-entry ledger, built in Phase 1 rather than retrofitted.
--
-- This is not premature. A marketplace that computes vendor payouts by summing
-- order rows discovers at scale that refunds, partial captures, adjustments,
-- commission changes and cancelled-after-capture orders do not reconcile — and
-- by then there is a year of money that has to be re-derived from an
-- incomplete history. The ledger is cheap now and impossible to backfill later.

CREATE TABLE ledger_accounts (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type        ledger_account_type NOT NULL,
    -- Who the account belongs to. NULL owner = a platform-level account
    -- (VAT payable, platform revenue).
    owner_type  text CHECK (owner_type IN ('vendor', 'user', 'driver', 'platform')),
    owner_id    uuid,
    currency    char(3) NOT NULL DEFAULT 'AED',
    name        text NOT NULL,
    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),

    -- One account per (type, owner, currency). Two vendor_payable accounts for
    -- the same vendor in AED means a payout run silently pays half.
    UNIQUE (type, owner_type, owner_id, currency)
);

CREATE INDEX ledger_accounts_owner_idx ON ledger_accounts (owner_type, owner_id);


-- A journal is one balanced transaction. Entries are only ever written as a
-- group.
CREATE TABLE ledger_journals (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- What happened in business terms: order_captured, refund_issued,
    -- commission_accrued, payout_settled.
    event_type  text NOT NULL,
    -- What it happened to. Kept generic so the ledger does not grow a foreign
    -- key per vertical.
    reference_type text,
    reference_id   uuid,
    description text,
    currency    char(3) NOT NULL DEFAULT 'AED',
    -- Set when this journal reverses another. Corrections are new journals,
    -- never edits — see the immutability trigger below.
    reverses_id uuid REFERENCES ledger_journals (id),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ledger_journals_reference_idx ON ledger_journals (reference_type, reference_id);
CREATE INDEX ledger_journals_occurred_idx ON ledger_journals (occurred_at DESC);
CREATE INDEX ledger_journals_event_type_idx ON ledger_journals (event_type, occurred_at DESC);


-- Immutable, always in balanced pairs. Never updated, never deleted.
CREATE TABLE ledger_entries (
    id          bigserial PRIMARY KEY,
    journal_id  uuid NOT NULL REFERENCES ledger_journals (id),
    account_id  uuid NOT NULL REFERENCES ledger_accounts (id),
    direction   ledger_direction NOT NULL,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency    char(3) NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ledger_entries_journal_idx ON ledger_entries (journal_id);
-- Balance queries: sum entries for an account. This index shape answers them
-- without touching the heap.
CREATE INDEX ledger_entries_account_idx ON ledger_entries (account_id, created_at)
    INCLUDE (direction, amount_minor);


-- Immutability, enforced by the database.
--
-- A trigger rather than a convention because the whole value of the ledger is
-- that it cannot be quietly edited. An ops engineer fixing a number by hand
-- during an incident is exactly the scenario this must survive; a code-level
-- rule does not survive psql.
CREATE OR REPLACE FUNCTION ledger_entries_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION
        'ledger_entries is append-only: % is not permitted. Post a reversing journal instead.',
        TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_entries_no_update
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_immutable();


-- Balance check, deferred to COMMIT.
--
-- Deferred is essential: entries are inserted one row at a time, so the journal
-- is legitimately unbalanced between the debit and the credit. Checking per
-- statement would reject every valid write.
CREATE OR REPLACE FUNCTION ledger_journal_balanced() RETURNS trigger AS $$
DECLARE
    v_debits  bigint;
    v_credits bigint;
BEGIN
    SELECT
        COALESCE(SUM(amount_minor) FILTER (WHERE direction = 'debit'), 0),
        COALESCE(SUM(amount_minor) FILTER (WHERE direction = 'credit'), 0)
      INTO v_debits, v_credits
      FROM ledger_entries
     WHERE journal_id = NEW.journal_id;

    IF v_debits <> v_credits THEN
        RAISE EXCEPTION
            'journal % is unbalanced: debits %, credits %',
            NEW.journal_id, v_debits, v_credits;
    END IF;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER ledger_entries_balanced
    AFTER INSERT ON ledger_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ledger_journal_balanced();


-- Platform-level accounts. Vendor and customer accounts are created lazily on
-- first use.
INSERT INTO ledger_accounts (type, owner_type, owner_id, currency, name) VALUES
    ('platform_cash',              'platform', NULL, 'AED', 'Platform cash (AED)'),
    ('platform_revenue',           'platform', NULL, 'AED', 'Commission revenue (AED)'),
    ('vat_payable',                'platform', NULL, 'AED', 'VAT payable (AED)'),
    ('payment_gateway_receivable', 'platform', NULL, 'AED', 'Gateway receivable (AED)'),
    ('deferred_revenue',           'platform', NULL, 'AED', 'Deferred revenue (AED)');

COMMENT ON TABLE ledger_entries IS
    'Append-only. Enforced by trigger. Corrections are reversing journals, never updates.';
