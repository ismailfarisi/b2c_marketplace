-- Reverse of 000020.

DROP INDEX IF EXISTS payout_accounts_iban_tail_idx;
DROP INDEX IF EXISTS users_phone_idx;
DROP INDEX IF EXISTS vehicle_listings_ref_idx;
DROP INDEX IF EXISTS boost_campaigns_ref_idx;
DROP INDEX IF EXISTS disputes_ref_idx;
DROP INDEX IF EXISTS payout_statements_ref_idx;
DROP INDEX IF EXISTS vehicle_listings_vin_idx;
DROP INDEX IF EXISTS orders_order_no_idx;

DROP INDEX IF EXISTS audit_log_actor_idx;
DROP INDEX IF EXISTS audit_log_target_idx;
DROP INDEX IF EXISTS admin_actions_actor_idx;
DROP INDEX IF EXISTS admin_actions_subject_idx;

DROP TABLE IF EXISTS app_settings;
DROP TABLE IF EXISTS feature_flags;

DROP TABLE IF EXISTS admin_grants;
DROP TABLE IF EXISTS admin_role_permissions;
DROP TABLE IF EXISTS admin_roles;
DROP TABLE IF EXISTS admin_permissions;
