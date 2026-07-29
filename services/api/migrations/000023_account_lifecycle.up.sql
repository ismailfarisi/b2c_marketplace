-- Account merge and erasure.
--
-- Fifty-eight tables carry a foreign key to `users`. The obvious merge —
-- repoint every one of them at the surviving account and delete the duplicate —
-- is not buildable and would not be correct if it were. Invoices name a buyer,
-- and a tax invoice whose buyer changed after issue is a different document
-- from the one the customer holds. The ledger is append-only. Payments carry the
-- identity the gateway authorised against.
--
-- So a merge here **links and freezes** rather than moves.
--
--   * The duplicate keeps every record it earned, exactly as issued.
--   * It is marked as merged into the survivor and can no longer log in.
--   * Account-level belongings that name no financial document — addresses,
--     notification preferences, saved searches, favourites — move across,
--     because those are what a customer would expect to follow them and none of
--     them is evidence of anything.
--   * The console shows both histories under the survivor.
--
-- The cost is stated rather than hidden: a merged customer does not see their
-- old orders in the app. That is a real limitation and the alternative is
-- rewriting issued documents, which is worse.
--
-- Erasure is pseudonymisation for the same reason. A data request cannot delete
-- an invoice the platform is required to retain, so what is erased is everything
-- that identifies the person, and the financial record stays with a subject
-- nobody can resolve to a human.

ALTER TABLE users
    -- The survivor. Self-referential and nullable; set once and never cleared,
    -- because un-merging would need to know which of the moved rows came from
    -- where, and nothing records that.
    ADD COLUMN merged_into_user_id uuid REFERENCES users (id),
    ADD COLUMN merged_at           timestamptz,
    -- Erasure is separate from merge and from deletion. A user can be erased
    -- without ever having been merged, and `deleted_at` already means something
    -- else here: the customer closed their account and may come back.
    ADD COLUMN erased_at           timestamptz;

-- A merged account must name its survivor, and an unmerged one must not have a
-- timestamp. Enforced rather than assumed, because the login check reads one
-- column and the console reads the other.
ALTER TABLE users
    ADD CONSTRAINT users_merge_is_complete
    CHECK ((merged_into_user_id IS NULL) = (merged_at IS NULL));

-- No chains. If A merges into B and B later merges into C, a lookup of A must
-- not have to walk a list — and more importantly, whoever merges B into C has to
-- deal with A explicitly rather than silently acquiring it.
CREATE OR REPLACE FUNCTION users_no_merge_chains() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.merged_into_user_id IS NOT NULL THEN
        IF NEW.merged_into_user_id = NEW.id THEN
            RAISE EXCEPTION 'an account cannot be merged into itself';
        END IF;
        IF EXISTS (SELECT 1 FROM users u
                    WHERE u.id = NEW.merged_into_user_id
                      AND u.merged_into_user_id IS NOT NULL) THEN
            RAISE EXCEPTION 'that account has itself been merged — merge into the surviving account instead';
        END IF;
        IF EXISTS (SELECT 1 FROM users u WHERE u.merged_into_user_id = NEW.id) THEN
            RAISE EXCEPTION 'this account is the survivor of an earlier merge and cannot itself be merged';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER users_no_merge_chains_trg
    BEFORE INSERT OR UPDATE OF merged_into_user_id ON users
    FOR EACH ROW EXECUTE FUNCTION users_no_merge_chains();

CREATE INDEX users_merged_into_idx ON users (merged_into_user_id)
    WHERE merged_into_user_id IS NOT NULL;

COMMENT ON COLUMN users.merged_into_user_id IS
    'The surviving account. Records stay with the row that earned them; only '
    'addresses, preferences, saved searches and favourites move. See migration 23.';
COMMENT ON COLUMN users.erased_at IS
    'Pseudonymised under a data request. The row and its financial records '
    'remain, with nothing left that resolves to a person.';
