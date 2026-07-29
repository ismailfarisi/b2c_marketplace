-- Drops the columns. Any merge or erasure already performed is *not* reversed:
-- the rows that moved during a merge stay moved, because nothing records where
-- they came from, and an erasure is irreversible by construction — that is what
-- makes it an erasure.
DROP TRIGGER IF EXISTS users_no_merge_chains_trg ON users;
DROP FUNCTION IF EXISTS users_no_merge_chains();
DROP INDEX IF EXISTS users_merged_into_idx;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_merge_is_complete;

ALTER TABLE users
    DROP COLUMN IF EXISTS merged_into_user_id,
    DROP COLUMN IF EXISTS merged_at,
    DROP COLUMN IF EXISTS erased_at;
