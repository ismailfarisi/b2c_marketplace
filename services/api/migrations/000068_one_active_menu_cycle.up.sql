-- One live menu per kitchen. Reads already take the oldest active cycle, so
-- a stray second one changed nothing; now it cannot exist. Any duplicates
-- (left by tests on a shared database) keep the oldest and lose the rest.
UPDATE menu_cycles c
   SET is_active = false
 WHERE c.is_active
   AND c.id <> (SELECT o.id FROM menu_cycles o
                 WHERE o.branch_id = c.branch_id AND o.is_active
                 ORDER BY o.created_at, o.id LIMIT 1);

CREATE UNIQUE INDEX menu_cycles_one_active_idx ON menu_cycles (branch_id) WHERE is_active;
