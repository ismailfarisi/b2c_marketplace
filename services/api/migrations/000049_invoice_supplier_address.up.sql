-- A UAE tax invoice must carry the supplier's address as well as its name and
-- TRN. The issuer already loads it (the platform's tax registration, or the
-- vendor's registration / branch address) but had nowhere to store it, so
-- every printed copy left it off. Snapshotted like the rest of the parties:
-- invoices issued before this column existed stay as they were issued.
ALTER TABLE invoices ADD COLUMN supplier_address text;
