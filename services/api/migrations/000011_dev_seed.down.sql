-- Removes only the seeded vendor, by its fixed id. Deleting every vendor would
-- turn a routine rollback into data loss on any environment that has real ones.
DELETE FROM vendors WHERE id = '11111111-1111-1111-1111-111111111111';
