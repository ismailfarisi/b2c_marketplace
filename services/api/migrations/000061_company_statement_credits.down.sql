-- Fold carried-forward remainders back into the credit they were split from
-- (carried_from always names the original row)
-- so the one-credit-per-cycle index can come back.
UPDATE company_charges o
   SET amount_minor = o.amount_minor + c.total, statement_id = NULL, applied_at = NULL
  FROM (SELECT carried_from, sum(amount_minor) AS total FROM company_charges
         WHERE carried_from IS NOT NULL GROUP BY carried_from) c
 WHERE o.id = c.carried_from;
DELETE FROM company_charges WHERE carried_from IS NOT NULL;
UPDATE company_charges SET statement_id = NULL, applied_at = NULL WHERE applied_at IS NOT NULL;

DROP INDEX company_charges_credit_key;
CREATE UNIQUE INDEX company_charges_credit_key ON company_charges (cycle_id) WHERE kind = 'credit';

ALTER TABLE company_charges
    DROP CONSTRAINT company_charges_adjustment_shape,
    DROP COLUMN carried_from,
    DROP COLUMN applied_at;
