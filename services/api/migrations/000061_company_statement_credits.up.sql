-- A refund credit for a lunch that was already billed used to sit "unbilled"
-- as a negative balance while the issued statement still asked for the full
-- amount. Such credits are now applied to the company's open (issued, unpaid)
-- statements as adjustment lines: the credit row is attached to the statement
-- with applied_at set, which reduces what the statement asks for. Only the
-- part that exceeds the open balance carries forward, as a split-off credit
-- row pointing back at the one it came from.
ALTER TABLE company_charges
    ADD COLUMN applied_at   timestamptz,
    ADD COLUMN carried_from uuid REFERENCES company_charges (id);

ALTER TABLE company_charges
    ADD CONSTRAINT company_charges_adjustment_shape CHECK (
        (applied_at IS NULL OR (kind = 'credit' AND statement_id IS NOT NULL))
        AND (carried_from IS NULL OR kind = 'credit'));

-- A cancelled cycle still credits the company once; the carried-forward
-- remainder of that credit is a second row for the same cycle.
DROP INDEX company_charges_credit_key;
CREATE UNIQUE INDEX company_charges_credit_key ON company_charges (cycle_id)
    WHERE kind = 'credit' AND carried_from IS NULL;
