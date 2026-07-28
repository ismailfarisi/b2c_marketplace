DROP FUNCTION IF EXISTS set_updated_at();

DROP TYPE IF EXISTS ledger_account_type;
DROP TYPE IF EXISTS ledger_direction;
DROP TYPE IF EXISTS payment_status;
DROP TYPE IF EXISTS payment_kind;
DROP TYPE IF EXISTS quote_state;
DROP TYPE IF EXISTS order_status;
DROP TYPE IF EXISTS fulfillment_status;
DROP TYPE IF EXISTS fulfillment_kind;
DROP TYPE IF EXISTS user_status;
DROP TYPE IF EXISTS locale_code;

-- Extensions are deliberately not dropped. They are cheap to keep, other
-- schemas in a shared cluster may depend on them, and dropping postgis on a
-- rollback is a far worse outcome than leaving it installed.
