-- The immutability trigger blocks DELETE on ledger_entries, so a rollback must
-- drop it before the table can go. DROP TABLE itself is not blocked by a row
-- trigger, but the trigger function is dropped explicitly to keep the schema
-- clean either way.
DROP TRIGGER IF EXISTS ledger_entries_balanced ON ledger_entries;
DROP TRIGGER IF EXISTS ledger_entries_no_update ON ledger_entries;
DROP FUNCTION IF EXISTS ledger_journal_balanced();
DROP FUNCTION IF EXISTS ledger_entries_immutable();

DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS ledger_journals;
DROP TABLE IF EXISTS ledger_accounts;
