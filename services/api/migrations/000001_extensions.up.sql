-- Extensions. Every one of these is load-bearing; none is speculative.

-- Geography for service areas, address points and (from Phase 2) reel coverage.
CREATE EXTENSION IF NOT EXISTS postgis;

-- btree_gist lets a scalar ride inside a GiST index alongside a geometry.
-- Phase 2's feed index is `gist (coverage, published_at)` and cannot exist
-- without it. Installed now so the extension is never a migration blocker on
-- the hot path.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Trigram search for vendor/offering name lookup in Arabic and English.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Case-insensitive email without a functional index on every query.
CREATE EXTENSION IF NOT EXISTS citext;

-- gen_random_uuid() is core since PG13; pgcrypto is here for digest() used by
-- the OTP code hashing in 000002.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Shared enums. Postgres enums are used where the value set is genuinely closed
-- and owned by the platform; open sets (service types, capacity resource keys)
-- are text columns backed by registry rows so a new vertical is a data change.
CREATE TYPE locale_code AS ENUM ('en', 'ar');

CREATE TYPE user_status AS ENUM ('active', 'suspended', 'deleted');

CREATE TYPE fulfillment_kind AS ENUM ('pickup', 'delivery', 'visit');

CREATE TYPE fulfillment_status AS ENUM (
    'scheduled', 'dispatched', 'en_route', 'arrived',
    'completed', 'failed', 'cancelled'
);

CREATE TYPE order_status AS ENUM (
    'pending_payment', 'confirmed', 'in_progress',
    'completed', 'cancelled', 'failed'
);

-- Price is a state machine, not a column. Laundry priced by weight moves
-- authorized -> adjusted after pickup; that is normal operation, not an
-- exception path.
CREATE TYPE quote_state AS ENUM (
    'estimated', 'authorized', 'adjusted', 'captured', 'refunded', 'void'
);

CREATE TYPE payment_kind AS ENUM ('authorization', 'capture', 'refund', 'void');

CREATE TYPE payment_status AS ENUM (
    'pending', 'requires_action', 'succeeded', 'failed', 'cancelled'
);

-- Sign convention for the double-entry ledger.
CREATE TYPE ledger_direction AS ENUM ('debit', 'credit');

CREATE TYPE ledger_account_type AS ENUM (
    'platform_cash',
    'vendor_payable',
    'deferred_revenue',
    'platform_revenue',
    'vat_payable',
    'driver_float',
    'customer_wallet',
    'payment_gateway_receivable'
);

-- Reusable trigger for updated_at. Doing this in the database rather than in Go
-- means a manual SQL fix during an incident still leaves an honest timestamp.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
