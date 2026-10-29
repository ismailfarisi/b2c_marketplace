DROP TABLE IF EXISTS company_charges;
DROP TABLE IF EXISTS company_statements;
DROP INDEX IF EXISTS subscriptions_company_idx;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS company_id;
DROP INDEX IF EXISTS addresses_company_office_key;
ALTER TABLE addresses DROP COLUMN IF EXISTS company_office_id;
DROP TABLE IF EXISTS company_offices;
DROP TABLE IF EXISTS company_members;
DROP TABLE IF EXISTS companies;
