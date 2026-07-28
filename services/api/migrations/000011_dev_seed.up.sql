-- Development seed: one vendor, one branch, two offerings, 14 days of capacity.
--
-- Guarded so it is a no-op on any database that already has a vendor. That
-- makes it safe to leave in the migration chain — CI applies it, staging
-- applies it once, and production (which will have real vendors before anyone
-- runs this) skips it.
--
-- Everything uses fixed UUIDs so integration tests and the seeded API agree on
-- identifiers without a lookup round-trip.

DO $$
DECLARE
    v_vendor_id uuid := '11111111-1111-1111-1111-111111111111';
    v_branch_id uuid := '22222222-2222-2222-2222-222222222222';
    v_window_morning uuid := '33333333-3333-3333-3333-333333333331';
    v_window_evening uuid := '33333333-3333-3333-3333-333333333332';
    v_day date;
BEGIN
    IF EXISTS (SELECT 1 FROM vendors LIMIT 1) THEN
        RAISE NOTICE 'vendors already exist — skipping dev seed';
        RETURN;
    END IF;

    INSERT INTO vendors (
        id, slug, legal_name, trade_name, trade_name_ar, status,
        onboarding_stage, trade_licence_no, trade_licence_expires,
        country, default_currency
    ) VALUES (
        v_vendor_id, 'sparkle-laundry', 'Sparkle Laundry Services LLC',
        'Sparkle Laundry', 'سباركل للغسيل', 'active',
        'complete', 'CN-1234567', current_date + interval '1 year',
        'AE', 'AED'
    );

    -- Al Barsha 1, Dubai. A real-ish point so the geography columns and the
    -- GiST indexes are exercised by the seed rather than only in production.
    INSERT INTO vendor_branches (
        id, vendor_id, name, name_ar, timezone, point,
        address_line, city, emirate, country, phone, is_active
    ) VALUES (
        v_branch_id, v_vendor_id, 'Al Barsha Facility', 'منشأة البرشاء',
        'Asia/Dubai',
        ST_SetSRID(ST_MakePoint(55.2000, 25.1100), 4326)::geography,
        'Warehouse 4, Al Barsha 1', 'Dubai', 'Dubai', 'AE', '+97143334444', true
    );

    -- Sunday (0) through Thursday (4) plus Saturday. Friday is the region's
    -- primary day off; assuming a Monday-Friday week here would silently
    -- schedule pickups on a day the facility is shut.
    INSERT INTO branch_hours (branch_id, weekday, opens_at, closes_at, is_closed)
    SELECT v_branch_id, d, TIME '08:00', TIME '22:00', false
      FROM unnest(ARRAY[0,1,2,3,4,6]) AS d;
    INSERT INTO branch_hours (branch_id, weekday, opens_at, closes_at, is_closed)
    VALUES (v_branch_id, 5, TIME '14:00', TIME '22:00', false);

    INSERT INTO time_windows (id, branch_id, label, label_ar, starts_at, ends_at, sort_order)
    VALUES
        (v_window_morning, v_branch_id, '08:00 – 10:00', '٠٨:٠٠ – ١٠:٠٠', TIME '08:00', TIME '10:00', 1),
        (v_window_evening, v_branch_id, '18:00 – 20:00', '١٨:٠٠ – ٢٠:٠٠', TIME '18:00', TIME '20:00', 2);

    INSERT INTO service_offerings (
        id, branch_id, service_type, name, name_ar, description,
        base_price_minor, currency, price_is_estimate, tax_rate_bp,
        lead_time_minutes, sort_order, is_active
    ) VALUES
        ('44444444-4444-4444-4444-444444444441', v_branch_id, 'laundry_wash_fold',
         'Wash & Fold (per kg)', 'غسيل وطي (للكيلو)',
         'Priced by weight — the amount is confirmed after pickup.',
         1_200, 'AED', true, 500, 1440, 1, true),
        ('44444444-4444-4444-4444-444444444442', v_branch_id, 'laundry_dry_clean',
         'Dry Clean — Shirt', 'تنظيف جاف — قميص',
         'Fixed price per garment.',
         1_800, 'AED', false, 500, 2880, 2, true);

    -- Two weeks of capacity. 20 slots per window is roughly one van's day.
    FOR v_day IN
        SELECT generate_series(current_date, current_date + 13, interval '1 day')::date
    LOOP
        INSERT INTO capacity_buckets (branch_id, resource_key, bucket_date, window_id, capacity_limit)
        VALUES
            (v_branch_id, 'pickup_slot', v_day, v_window_morning, 20),
            (v_branch_id, 'pickup_slot', v_day, v_window_evening, 20);
    END LOOP;

    RAISE NOTICE 'dev seed applied: vendor %, branch %', v_vendor_id, v_branch_id;
END $$;
