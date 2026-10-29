-- Office lunch accounts: a company pays for its employees' lunches and settles
-- a monthly statement by bank transfer. Employee subscriptions stay ordinary
-- subscriptions, so kitchens, prep sheets, ledger and payouts are unchanged.
CREATE TABLE companies (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name              text NOT NULL CHECK (length(trim(name)) > 0),
    legal_name        text NOT NULL CHECK (length(trim(legal_name)) > 0),
    trn               text,
    billing_email     text NOT NULL,
    billing_phone     text,
    status            text NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending', 'active', 'suspended', 'rejected')),
    currency          char(3) NOT NULL DEFAULT 'AED',
    credit_limit_minor bigint NOT NULL DEFAULT 0 CHECK (credit_limit_minor >= 0),
    payment_terms_days smallint NOT NULL DEFAULT 15 CHECK (payment_terms_days BETWEEN 0 AND 90),
    -- Lunch policy. Sunday is 0.
    covered_weekdays  smallint[] NOT NULL DEFAULT '{1,2,3,4,5}',
    covered_slots     text[] NOT NULL DEFAULT '{lunch}',
    meal_cap_minor    bigint NOT NULL DEFAULT 3500 CHECK (meal_cap_minor > 0),
    created_by        uuid NOT NULL REFERENCES users (id),
    reviewed_by       uuid REFERENCES users (id),
    reviewed_at       timestamptz,
    review_note       text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE company_members (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id  uuid NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    phone       text NOT NULL,
    user_id     uuid REFERENCES users (id),
    name        text,
    role        text NOT NULL DEFAULT 'employee' CHECK (role IN ('admin', 'employee')),
    status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    invited_by  uuid REFERENCES users (id),
    joined_at   timestamptz,
    removed_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX company_members_phone_key ON company_members (company_id, phone)
    WHERE status = 'active';
CREATE INDEX company_members_phone_idx ON company_members (phone) WHERE status = 'active';
CREATE INDEX company_members_user_idx ON company_members (user_id) WHERE status = 'active';

CREATE TABLE company_offices (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id  uuid NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    label       text NOT NULL,
    building    text,
    area        text,
    city        text NOT NULL,
    point       geography(Point, 4326) NOT NULL,
    notes       text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

CREATE INDEX company_offices_company_idx ON company_offices (company_id) WHERE deleted_at IS NULL;

-- An employee's copy of an office address, so delivery-area checks and
-- fulfilment address snapshots work exactly as for a home address.
ALTER TABLE addresses ADD COLUMN company_office_id uuid REFERENCES company_offices (id);
CREATE UNIQUE INDEX addresses_company_office_key ON addresses (user_id, company_office_id)
    WHERE company_office_id IS NOT NULL AND deleted_at IS NULL;

ALTER TABLE subscriptions ADD COLUMN company_id uuid REFERENCES companies (id);
CREATE INDEX subscriptions_company_idx ON subscriptions (company_id) WHERE company_id IS NOT NULL;

CREATE TABLE company_statements (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    statement_no  text NOT NULL UNIQUE,
    company_id    uuid NOT NULL REFERENCES companies (id),
    period_end    date NOT NULL,
    total_minor   bigint NOT NULL CHECK (total_minor >= 0),
    currency      char(3) NOT NULL,
    status        text NOT NULL DEFAULT 'issued' CHECK (status IN ('issued', 'paid', 'void')),
    issued_by     uuid REFERENCES users (id),
    issued_at     timestamptz NOT NULL DEFAULT now(),
    due_on        date NOT NULL,
    paid_at       timestamptz,
    paid_reference text,
    paid_marked_by uuid REFERENCES users (id)
);

CREATE INDEX company_statements_company_idx ON company_statements (company_id, issued_at DESC);

-- One 'charge' per cycle the company paid for; a 'credit' (negative) returns
-- the unearned part of a cancelled cycle to the company.
CREATE TABLE company_charges (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id      uuid NOT NULL REFERENCES companies (id),
    kind            text NOT NULL DEFAULT 'charge' CHECK (kind IN ('charge', 'credit')),
    subscription_id uuid NOT NULL REFERENCES subscriptions (id),
    order_id        uuid REFERENCES orders (id),
    cycle_id        uuid REFERENCES subscription_cycles (id),
    user_id         uuid NOT NULL REFERENCES users (id),
    amount_minor    bigint NOT NULL,
    currency        char(3) NOT NULL,
    statement_id    uuid REFERENCES company_statements (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT company_charges_sign CHECK (
        (kind = 'charge' AND amount_minor >= 0 AND order_id IS NOT NULL)
        OR (kind = 'credit' AND amount_minor <= 0 AND cycle_id IS NOT NULL))
);

CREATE UNIQUE INDEX company_charges_order_key ON company_charges (order_id) WHERE kind = 'charge';
CREATE UNIQUE INDEX company_charges_credit_key ON company_charges (cycle_id) WHERE kind = 'credit';
CREATE INDEX company_charges_open_idx ON company_charges (company_id) WHERE statement_id IS NULL;
CREATE INDEX company_charges_statement_idx ON company_charges (statement_id);
