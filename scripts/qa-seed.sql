-- QA fixture: enough data for every surface to render something real.
--
-- Not `000011_dev_seed`. That migration seeds the minimum the *engine* needs to
-- boot — one vendor, one branch, two windows — and it is part of the schema, so
-- it has to stay small and stable. This file is the opposite: it exists to make
-- every screen in the product show a row, including the ones that only appear
-- when something has gone wrong.
--
-- Idempotent. Every insert is keyed on a fixed uuid with ON CONFLICT DO NOTHING,
-- so running it twice is a no-op and running it after a partial failure
-- completes the rest.
--
--   docker compose exec -T postgres psql -U marketplace -d marketplace \
--     -v ON_ERROR_STOP=1 -f - < scripts/qa-seed.sql
--
-- Deliberately seeded *broken* in places — an expired trade licence, a
-- name mismatch on a bank account, a fraud signal, an overdue appeal. A QA pass
-- against clean data only exercises the happy path, and the happy path is not
-- where the bugs are.

BEGIN;

-- ── people ──────────────────────────────────────────────────────────────────

INSERT INTO users (id, phone, display_name, locale, status, phone_verified_at) VALUES
    ('a0000000-0000-0000-0000-000000000001', '+971500000001', 'Layla Haddad',   'ar', 'active', now()),
    ('a0000000-0000-0000-0000-000000000002', '+971500000002', 'Omar Sayegh',    'en', 'active', now()),
    ('a0000000-0000-0000-0000-000000000003', '+971500000003', 'Driver Rashid',  'en', 'active', now()),
    ('a0000000-0000-0000-0000-000000000004', '+971500000004', 'Kitchen Manager','en', 'active', now())
ON CONFLICT (id) DO NOTHING;

-- Console access. Without a vendor_users row the console is an empty list, which
-- is a correct screen and a useless QA target.
INSERT INTO vendor_users (vendor_id, user_id, role, branch_id) VALUES
    ('11111111-1111-1111-1111-111111111111', 'a0000000-0000-0000-0000-000000000004',
     'manager', '22222222-2222-2222-2222-222222222222')
ON CONFLICT DO NOTHING;

INSERT INTO addresses (id, user_id, label, recipient_name, recipient_phone,
                       building, flat, street, landmark, area, city, emirate, point, is_default)
VALUES ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001',
        'Home', 'Layla Haddad', '+971500000001',
        'Marina Heights', '1204', 'Al Marsa St', 'opposite the metro exit',
        'Dubai Marina', 'Dubai', 'Dubai',
        ST_SetSRID(ST_MakePoint(55.1400, 25.0800), 4326)::geography, true)
ON CONFLICT (id) DO NOTHING;

-- ── a driver on shift ───────────────────────────────────────────────────────

INSERT INTO drivers (id, user_id, vendor_id, display_name, phone, vehicle_type, plate_no, status)
VALUES ('c0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000003',
        NULL, 'Rashid', '+971500000003', 'car', 'A 12345', 'active')
ON CONFLICT (id) DO NOTHING;

-- ── reels, published and covering Dubai Marina ──────────────────────────────
--
-- Inserted directly rather than pushed through the media pipeline. The pipeline
-- needs ffmpeg and an object store with real bytes in it; what the feed and the
-- moderation queue actually read is `state`, `coverage` and `published_at`, and
-- those are what this sets.

INSERT INTO reels (id, author_type, author_id, state, visibility, caption, caption_locale,
                   duration_ms, poster_key, published_at, coverage, coverage_kind,
                   coverage_resolved_at, origin_point, metadata_ready_at)
VALUES
    ('d0000000-0000-0000-0000-000000000001', 'vendor_branch', '22222222-2222-2222-2222-222222222222',
     'published', 'public', 'Same-day wash and fold, collected before noon', 'en',
     14000, 'posters/qa-1.jpg', now() - interval '2 hours',
     ST_Buffer(ST_SetSRID(ST_MakePoint(55.1400, 25.0800), 4326)::geography, 8000),
     'radius', now(), ST_SetSRID(ST_MakePoint(55.1400, 25.0800), 4326)::geography, now()),
    ('d0000000-0000-0000-0000-000000000002', 'vendor_branch', '22222222-2222-2222-2222-222222222222',
     'published', 'public', 'إزالة البقع الصعبة قبل وبعد', 'ar',
     21000, 'posters/qa-2.jpg', now() - interval '1 day',
     ST_Buffer(ST_SetSRID(ST_MakePoint(55.1400, 25.0800), 4326)::geography, 8000),
     'radius', now(), ST_SetSRID(ST_MakePoint(55.1400, 25.0800), 4326)::geography, now()),
    -- Awaiting review, so the moderation queue has a row.
    ('d0000000-0000-0000-0000-000000000003', 'user', 'a0000000-0000-0000-0000-000000000002',
     'review_pending', 'public', 'سيارتي للبيع — حالة ممتازة', 'ar',
     30000, 'posters/qa-3.jpg', NULL, NULL, NULL, NULL, NULL,
     now() - interval '3 hours')
ON CONFLICT (id) DO NOTHING;

INSERT INTO reel_renditions (reel_id, label, width, height, bitrate_kbps, playlist_url) VALUES
    ('d0000000-0000-0000-0000-000000000001', '720p', 720, 1280, 2200, 'https://cdn.invalid/qa-1/720p.m3u8'),
    ('d0000000-0000-0000-0000-000000000001', '540p', 540,  960, 1200, 'https://cdn.invalid/qa-1/540p.m3u8'),
    ('d0000000-0000-0000-0000-000000000002', '720p', 720, 1280, 2200, 'https://cdn.invalid/qa-2/720p.m3u8')
ON CONFLICT DO NOTHING;

-- A comment awaiting review.
INSERT INTO reel_comments (id, reel_id, author_id, body, state)
VALUES ('e0000000-0000-0000-0000-000000000001', 'd0000000-0000-0000-0000-000000000001',
        'a0000000-0000-0000-0000-000000000002',
        'Do you collect from Marina Heights? واتساب ٠٥٠١٢٣٤٥٦٧', 'pending')
ON CONFLICT (id) DO NOTHING;

-- ── a vehicle listing awaiting review, from a seller with a fraud signal ─────

-- The taxonomy is already seeded by migration 000016, so this looks it up by
-- name rather than inserting fixed ids. A fixture that re-inserts reference data
-- is a fixture that silently no-ops on the insert and then fails on the foreign
-- key three statements later, which is exactly what the first version did.
INSERT INTO vehicle_listings (id, reference_no, seller_kind, seller_id, make_id, model_id, year,
                              mileage_km, regional_spec, vin, price_minor, currency,
                              title, description, emirate, city, point, radius_m, status)
SELECT 'f3000000-0000-0000-0000-000000000001', 'VL-2607-QA0001', 'private', 'a0000000-0000-0000-0000-000000000002',
       md.make_id, md.id, 2021,
       68000, 'gcc', 'JTMHV05J104123456', 21500000, 'AED',
       'Toyota Land Cruiser VXR 2021 — GCC, full service history',
       'One owner, agency maintained.', 'Dubai', 'Dubai',
       ST_SetSRID(ST_MakePoint(55.1400, 25.0800), 4326)::geography, 25000, 'pending_review'
  FROM vehicle_models md
  JOIN vehicle_makes mk ON mk.id = md.make_id
 WHERE mk.slug = 'toyota' AND md.name = 'Land Cruiser'
 LIMIT 1
ON CONFLICT (id) DO NOTHING;

-- An open fraud signal against the same seller, so the listing queue shows a
-- trust band that is not "clean" and the reviewer has a reason to look twice.
INSERT INTO fraud_signals (id, subject_type, subject_id, kind, severity, source, state, detail)
VALUES ('f4000000-0000-0000-0000-000000000001', 'user', 'a0000000-0000-0000-0000-000000000002',
        'odometer_rollback', 45, 'system', 'open',
        '{"previous_reading_km": 91000, "current_reading_km": 68000}'::jsonb)
ON CONFLICT (id) DO NOTHING;

-- ── a held chat message ─────────────────────────────────────────────────────

INSERT INTO conversations (id, listing_id, buyer_id, seller_id, state)
VALUES ('f5000000-0000-0000-0000-000000000001', 'f3000000-0000-0000-0000-000000000001',
        'a0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000002', 'open')
ON CONFLICT (id) DO NOTHING;

INSERT INTO messages (id, conversation_id, sender_id, body, state, findings, review_reason)
VALUES ('f6000000-0000-0000-0000-000000000001', 'f5000000-0000-0000-0000-000000000001',
        'a0000000-0000-0000-0000-000000000002',
        'Send a deposit to secure it, I am travelling. ٠٥٠١٢٣٤٥٦٧',
        'held', ARRAY['advance_fee_language','phone_number'], 'advance_fee_language')
ON CONFLICT (id) DO NOTHING;

-- ── an overdue appeal ───────────────────────────────────────────────────────

INSERT INTO content_appeals (id, subject_type, subject_id, appellant_id, statement,
                             original_reviewer_id, status, due_at, created_at)
VALUES ('f7000000-0000-0000-0000-000000000001', 'reel',
        'd0000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000002',
        'The video shows my own car. Nothing in it breaks the rules.',
        NULL, 'open', now() - interval '2 days', now() - interval '9 days')
ON CONFLICT (id) DO NOTHING;

-- ── KYC: an approved-looking case with a lapsed licence ─────────────────────

INSERT INTO kyc_cases (id, subject_type, subject_id, tier, status)
VALUES ('f8000000-0000-0000-0000-000000000001', 'vendor',
        '11111111-1111-1111-1111-111111111111', 'enhanced', 'submitted')
ON CONFLICT DO NOTHING;

INSERT INTO kyc_documents (id, case_id, kind, document_ref, number_last4, expires_on, status) VALUES
    ('f9000000-0000-0000-0000-000000000001', 'f8000000-0000-0000-0000-000000000001',
     'trade_licence', 'kyc/qa-licence.pdf', '4821', CURRENT_DATE - 20, 'verified'),
    ('f9000000-0000-0000-0000-000000000002', 'f8000000-0000-0000-0000-000000000001',
     'authorised_signatory', 'kyc/qa-signatory.pdf', NULL, NULL, 'pending')
ON CONFLICT (id) DO NOTHING;

-- A bank account whose holder name does not match the vendor's legal name.
-- This is the case the reviewer exists to catch.
INSERT INTO payout_accounts (id, owner_type, owner_id, iban, bank_name, holder_name, currency, country, status)
VALUES ('fa000000-0000-0000-0000-000000000001', 'vendor', '11111111-1111-1111-1111-111111111111',
        'AE070331234567890123456', 'Emirates NBD', 'GULF TRADING FZE', 'AED', 'AE', 'pending')
ON CONFLICT (id) DO NOTHING;

-- ── a campaign, funded and awaiting review ──────────────────────────────────

INSERT INTO boost_campaigns (id, advertiser_type, advertiser_id, reel_id, objective,
                             bid_cpm_minor, funded_minor, daily_cap_minor,
                             starts_on, status)
VALUES ('fb000000-0000-0000-0000-000000000001', 'vendor', '11111111-1111-1111-1111-111111111111',
        'd0000000-0000-0000-0000-000000000001', 'reach',
        1500, 50000, 10000, CURRENT_DATE, 'pending_review')
ON CONFLICT (id) DO NOTHING;

INSERT INTO boost_targeting (campaign_id, centre, radius_m, locales, hours_of_day)
VALUES ('fb000000-0000-0000-0000-000000000001',
        ST_SetSRID(ST_MakePoint(55.1400, 25.0800), 4326)::geography, 10000,
        ARRAY['en','ar'], ARRAY[17,18,19,20])
ON CONFLICT DO NOTHING;

-- ── an open dispute ─────────────────────────────────────────────────────────

INSERT INTO disputes (id, reference_no, subject_type, subject_id, raised_by_type, raised_by_id,
                      kind, state, due_at)
VALUES ('fc000000-0000-0000-0000-000000000001', 'DSP-2607-QA0001', 'fulfillment',
        'd0000000-0000-0000-0000-000000000001', 'customer', 'a0000000-0000-0000-0000-000000000001',
        'not_delivered', 'open', now() + interval '1 day')
ON CONFLICT (id) DO NOTHING;

COMMIT;

\echo '--- seeded ---'
SELECT 'reels published' AS what, count(*) FROM reels WHERE state = 'published'
UNION ALL SELECT 'reels awaiting review', count(*) FROM reels WHERE state = 'review_pending'
UNION ALL SELECT 'comments pending', count(*) FROM reel_comments WHERE state = 'pending'
UNION ALL SELECT 'messages held', count(*) FROM messages WHERE state = 'held'
UNION ALL SELECT 'listings pending', count(*) FROM vehicle_listings WHERE status = 'pending_review'
UNION ALL SELECT 'fraud signals open', count(*) FROM fraud_signals WHERE state = 'open'
UNION ALL SELECT 'appeals overdue', count(*) FROM content_appeals WHERE status = 'open' AND due_at < now()
UNION ALL SELECT 'kyc submitted', count(*) FROM kyc_cases WHERE status = 'submitted'
UNION ALL SELECT 'accounts pending', count(*) FROM payout_accounts WHERE status = 'pending'
UNION ALL SELECT 'campaigns pending', count(*) FROM boost_campaigns WHERE status = 'pending_review'
UNION ALL SELECT 'disputes open', count(*) FROM disputes WHERE state = 'open';
