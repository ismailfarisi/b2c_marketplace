-- Three more rotation kitchens cloned from the QA Spice Route branch, so a
-- four-kitchen rotation can be bought locally. Run after scripts/qa-seed.sql
-- and scripts/demo-meals-seed.sql.
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

-- The QA customer's Home (JLT, 25.08 N 55.14 E) is otherwise inside only one
-- rotation kitchen's area (Teta's), so the builder could never reach four
-- picks there. Stretch three demo rotation kitchens (scripts/demo-meals-seed.sql)
-- to also cover 5 km around it. Idempotent: ST_Union with a disc already
-- covered leaves the area as it is.
UPDATE vendor_branches
   SET service_area = ST_Union(service_area::geometry,
           ST_Buffer(ST_SetSRID(ST_MakePoint(55.14, 25.08), 4326)::geography, 5000)::geometry)::geography
 WHERE id IN ('de010001-0000-4000-8000-000000000001',   -- Amma's Kerala Kitchen
              'de010006-0000-4000-8000-000000000001',   -- Lola's Lutong Bahay
              'de010007-0000-4000-8000-000000000001')   -- Udupi Veg Thali
   AND service_area IS NOT NULL;
