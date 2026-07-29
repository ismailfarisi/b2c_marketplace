-- Restores the looser constraint.
--
-- The deactivated duplicates are *not* reactivated. Reactivating them would
-- recreate the condition where the plain UNIQUE constraint cannot be added
-- back — there would be several active rows per identity again — so the
-- rollback would fail on its last statement having already half-run. Leaving
-- them inactive is both correct and the only ordering that works.
DROP INDEX IF EXISTS ledger_accounts_identity_idx;

ALTER TABLE ledger_accounts
    ADD CONSTRAINT ledger_accounts_type_owner_type_owner_id_currency_key
    UNIQUE (type, owner_type, owner_id, currency);
