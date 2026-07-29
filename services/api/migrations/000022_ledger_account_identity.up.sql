-- Platform ledger accounts were duplicating, one per posting.
--
-- Found the first time a trial balance was rendered, which is what a trial
-- balance is for.
--
-- `resolveAccount` upserts with
--     ON CONFLICT (type, owner_type, owner_id, currency)
-- and the constraint behind it was a plain UNIQUE. In Postgres a UNIQUE
-- constraint treats NULLs as *distinct* — two rows with the same type,
-- owner_type and currency but a NULL owner_id do not conflict, because NULL is
-- not equal to NULL. Platform-level accounts (commission revenue, VAT payable,
-- gateway receivable) all carry a NULL owner_id.
--
-- So ON CONFLICT never fired for them, and every capture, refund and COD
-- collection minted a *fresh* account holding exactly one entry. Vendor and
-- driver accounts were fine — they have a real owner_id, so the constraint did
-- its job — which is exactly why this survived: the numbers anyone had actually
-- looked at were the per-vendor ones, and those were correct.
--
-- Nothing was lost and nothing was mis-stated. Every entry landed on an account
-- of the right type and the books balanced to the fils. But "what is our
-- commission revenue this month" was unanswerable without summing an unbounded
-- set of identically-named accounts, and the set grew by one row per order.

-- The first attempt at this migration repointed the entries onto one surviving
-- account per identity. The database refused:
--
--     ledger_entries is append-only: UPDATE is not permitted.
--     Post a reversing journal instead.
--
-- Which is correct, and a better answer than the one being attempted. Merging
-- entries would have rewritten history to make a report tidier — exactly the
-- thing an append-only ledger exists to prevent. The trigger was right and the
-- migration was wrong.
--
-- So the duplicates stay. They are real accounts that really hold those
-- entries, and the balance of each is a fact. What changes is that no *new*
-- ones can be created: the superseded rows are deactivated, and the uniqueness
-- rule applies to active accounts only.
--
-- The trial balance groups by identity rather than by row, so these aggregate
-- into the single account they always logically were. That is a presentation
-- decision made in the read model, where it belongs, rather than a data change
-- made here, where it would be a lie.

UPDATE ledger_accounts a
   SET is_active = false
  FROM (
    SELECT id,
           first_value(id) OVER (
               PARTITION BY type, owner_type,
                            coalesce(owner_id, '00000000-0000-0000-0000-000000000000'::uuid),
                            currency
               ORDER BY created_at, id
           ) AS keep_id
      FROM ledger_accounts
  ) g
 WHERE a.id = g.id
   AND g.keep_id <> g.id;

-- A partial unique index rather than a table constraint, because the rule is
-- "one *active* account per identity" and a constraint cannot carry a WHERE.
-- NULLS NOT DISTINCT (Postgres 15+) states the other half directly: a missing
-- owner is one identity, not infinitely many.
CREATE UNIQUE INDEX ledger_accounts_identity_idx
    ON ledger_accounts (type, owner_type, owner_id, currency)
    NULLS NOT DISTINCT
    WHERE is_active;

-- The old constraint has to go, or it keeps permitting what the index forbids
-- — and worse, ON CONFLICT would still be able to bind to it.
ALTER TABLE ledger_accounts
    DROP CONSTRAINT IF EXISTS ledger_accounts_type_owner_type_owner_id_currency_key;

COMMENT ON INDEX ledger_accounts_identity_idx IS
    'One active account per (type, owner, currency). NULLS NOT DISTINCT because '
    'a platform-level account has no owner and that absence is a single '
    'identity. Plain UNIQUE let ON CONFLICT miss, minting an account per posting.';
