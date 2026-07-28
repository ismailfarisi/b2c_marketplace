-- Reverse of 000017.

DROP FUNCTION IF EXISTS generate_reference(text);
DROP FUNCTION IF EXISTS ensure_driver_ping_partitions(int);

DROP INDEX IF EXISTS fulfillments_unassigned_idx;
DROP INDEX IF EXISTS fulfillments_driver_idx;
ALTER TABLE fulfillments
    DROP COLUMN IF EXISTS otp_issued_at,
    DROP COLUMN IF EXISTS otp_hash,
    DROP COLUMN IF EXISTS attempt_count,
    DROP COLUMN IF EXISTS route_id,
    DROP COLUMN IF EXISTS driver_id;

DROP TABLE IF EXISTS admin_actions;
DROP TABLE IF EXISTS dispute_events;
DROP TABLE IF EXISTS disputes;

DROP TABLE IF EXISTS payout_statement_lines;
DROP TABLE IF EXISTS payout_statements;
DROP TABLE IF EXISTS payout_runs;

-- Collections reference remittances; drop the constraint first so the order
-- below does not depend on which table Postgres happens to lock.
ALTER TABLE cod_collections DROP CONSTRAINT IF EXISTS cod_collections_remittance_fk;
DROP TABLE IF EXISTS cod_remittances;
DROP TABLE IF EXISTS cod_collections;

DROP TABLE IF EXISTS delivery_attempts;
DROP TABLE IF EXISTS proof_of_delivery;

DROP TABLE IF EXISTS fulfillment_assignments;
DROP TABLE IF EXISTS route_stops;
DROP TABLE IF EXISTS delivery_routes;

-- Partitions go with the parent.
DROP TABLE IF EXISTS driver_location_pings;

DROP TABLE IF EXISTS driver_shifts;
DROP TABLE IF EXISTS driver_documents;
DROP TABLE IF EXISTS drivers;
