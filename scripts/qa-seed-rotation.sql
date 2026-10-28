-- Three more rotation kitchens cloned from the QA Spice Route branch, so a
-- four-kitchen rotation can be bought locally. Run after scripts/qa-seed.sql.
-- Idempotent: fixed ids, ON CONFLICT DO NOTHING.
DO $$
DECLARE
    src uuid := '22222222-2222-2222-2222-222222222223';
    k record;
BEGIN
    FOR k IN SELECT * FROM (VALUES
        ('33333333-3333-3333-3333-333333333301'::uuid, 'Mama''s Kitchen — Al Quoz', 'mamas-kitchen-al-quoz', 30000),
        ('33333333-3333-3333-3333-333333333302'::uuid, 'Green Bowl — Al Quoz',      'green-bowl-al-quoz',    28500),
        ('33333333-3333-3333-3333-333333333303'::uuid, 'Kerala Tiffin — Al Quoz',   'kerala-tiffin-al-quoz', 26000)
    ) AS t(id, name, slug, price)
    LOOP
        INSERT INTO vendor_branches
        SELECT (jsonb_populate_record(NULL::vendor_branches, to_jsonb(b)
                || jsonb_build_object('id', k.id, 'name', k.name, 'name_ar', NULL, 'slug', k.slug))).*
          FROM vendor_branches b WHERE b.id = src
        ON CONFLICT DO NOTHING;

        INSERT INTO time_windows
        SELECT (jsonb_populate_record(NULL::time_windows, to_jsonb(w)
                || jsonb_build_object('id', gen_random_uuid(), 'branch_id', k.id))).*
          FROM time_windows w WHERE w.branch_id = src AND w.is_active
        ON CONFLICT DO NOTHING;

        IF NOT EXISTS (SELECT 1 FROM subscription_plans WHERE branch_id = k.id) THEN
            INSERT INTO subscription_plans
            SELECT (jsonb_populate_record(NULL::subscription_plans, to_jsonb(p)
                    || jsonb_build_object('id', gen_random_uuid(), 'branch_id', k.id,
                                          'menu_cycle_id', NULL, 'name', 'Lunch Sun-Thu',
                                          'rotation_enabled', true,
                                          'rotation_week_price_minor', k.price))).*
              FROM subscription_plans p
             WHERE p.branch_id = src AND p.name = 'Lunch Sun-Thu';
        END IF;
    END LOOP;
END $$;
