# Data Model — Full Entity Catalogue

**Status:** Draft v1 · 2026-07-28 · companion to [PLAN.md](./PLAN.md)
**Scope:** the 114-table **services engine**. Reels, vehicles and ranking tables are counted separately — see "Where the other tables live" at the end.

> ⚠️ **The `P` column uses PLAN.md §5's numbering, which is *not* PLAN-VIDEO-FIRST §8's calendar phasing.** They collide — PLAN.md P2 (meals) is video-first Phase 5; PLAN.md P3 (clients) is video-first Phase 7. Read `P` as **dependency ordering** — "this table is needed once the engine reaches that stage" — not as a date. Full mapping in [README.md](./README.md).

**P1 = 15 tables** — that is the entire walking skeleton with real money moving. Everything else is widening.

| Module | Tables | P1 |
|---|---:|---:|
| 1. Identity & access | 10 | 3 |
| 2. Vendor | 10 | 2 |
| 3. Catalog | 14 | 2 |
| 4. Scheduling & capacity | 8 | 2 |
| 5. Ordering | 6 | 2 |
| 6. Subscriptions | 7 | 0 |
| 7. Fulfillment | 5 | 1 |
| 8. Laundry operations | 3 | 0 |
| 9. Money | 19 | 5 |
| 10. Promotions & loyalty | 6 | 0 |
| 11. Logistics | 8 | 0 |
| 12. Trust & disputes | 5 | 0 |
| 13. Support & comms | 6 | 0 |
| 14. Platform | 7 | 2 |
| **Total** | **114** | **~15** |

---

## 1. Identity & access · 10

| # | Table | P | Purpose |
|---|---|---|---|
| 1 | `users` | 1 | Phone (E.164) primary, email optional, locale `en`/`ar`, status |
| 2 | `user_identities` | 3 | Apple/Google links. Apple Sign-In is **mandatory on iOS** if you offer any social login |
| 3 | `otp_codes` | 1 | code_hash, purpose, attempts, expires_at. Rate-limit by phone *and* IP |
| 4 | `sessions` | 1 | Refresh tokens, device, revoked_at. Enables "log out all devices" |
| 5 | `devices` | 3 | FCM/APNs push tokens, platform, app_version. Outlives sessions — keep separate |
| 6 | `roles` | 3 | customer, vendor_owner, vendor_staff, driver, admin, support |
| 7 | `permissions` | 3 | Fine-grained capability strings |
| 8 | `role_assignments` | 3 | **Scoped**: (user, role, scope_type, scope_id). One human can be a customer *and* manager of vendor A |
| 9 | `addresses` | 1 | geo point + building/flat/street/**landmark**/city/emirate. GCC addressing is landmark-based — a postcode-driven form fails here |
| 10 | `customer_profiles` | 3 | Dietary flags, preferences, referral_code, acquisition_source. Keeps `users` thin |

## 2. Vendor · 10

| # | Table | P | Purpose |
|---|---|---|---|
| 11 | `vendors` | 1 | Legal + trade name, slug, status, onboarding_stage, rating cache |
| 12 | `vendor_branches` | 1 | **Capacity, service areas and hours attach here, not to `vendors`.** A laundry chain has 3 facilities |
| 13 | `vendor_members` | 3 | Staff ↔ vendor with role, invite flow |
| 14 | `vendor_documents` | 5 | Trade licence, Emirates ID, VAT cert, IBAN letter. **`expires_at` matters** — trade licences renew annually and an expired one must block payouts |
| 15 | `vendor_bank_accounts` | 5 | IBAN, holder name, verified_at |
| 16 | `vendor_settings` | 3 | Cutoffs, lead times, auto-accept, prep duration, weekend days |
| 17 | `vendor_business_hours` | 3 | Per branch, per weekday |
| 18 | `vendor_holidays` | 3 | Ad-hoc closures |
| 19 | `commission_plans` | 5 | Flat %, per-category, or tiered. **Versioned** — never edit a plan money has been booked against |
| 20 | `vendor_commission_overrides` | 5 | Negotiated deals |

## 3. Catalog · 14

| # | Table | P | Purpose |
|---|---|---|---|
| 21 | `service_categories` | 3 | Hierarchical: laundry, meals, cleaning… |
| 22 | `service_types` | 1 | **The strategy registry.** Each row binds the four strategies (item-selection, fulfillment pattern, capacity resource, pricing). Adding a vertical = a row here plus code for its strategies |
| 23 | `service_offerings` | 1 | branch + service_type + name, images, status, lead time |
| 24 | `offering_variants` | 3 | SKU level: "shirt — wash & iron", "1kg wash & fold" |
| 25 | `offering_options` | 3 | Modifier groups: express, starch level, spice level |
| 26 | `offering_option_values` | 3 | Individual choices with price deltas |
| 27 | `offering_availability` | 3 | Which days/windows an offering is orderable |
| 28 | `subscription_plans` | 2 | Template: duration, days/week, meals/day, allowed pauses, prepaid flag |
| 29 | `plan_tiers` | 2 | 5-day vs 6-day, small vs large portion |
| 30 | `menu_cycles` | 2 | A rotating N-week menu for a kitchen |
| 31 | `menu_cycle_days` | 2 | Week no. + weekday within a cycle |
| 32 | `menu_items` | 2 | Dish, image, calories/macros, allergens, tags (veg/keto) |
| 33 | `menu_day_items` | 2 | Join: which dishes on which cycle day, in which meal slot, with portion caps |
| 34 | `laundry_item_types` | 3 | Shared garment taxonomy (shirt, kandura, abaya, curtain) so pricing is comparable across vendors |

## 4. Scheduling & capacity · 8

| # | Table | P | Purpose |
|---|---|---|---|
| 35 | `service_areas` | 3 | PostGIS polygon per branch + delivery fee + min order |
| 36 | `time_windows` | 1 | Reusable window definitions (08:00–10:00) |
| 37 | `window_availability` | 3 | Which windows run on which weekdays |
| 38 | `capacity_resources` | 3 | Declares what runs out: kg/day, pickup slots, portions of dish X |
| 39 | `capacity_buckets` | 1 | `(branch, resource, date, window, limit, consumed)` + `CHECK (consumed <= limit)`. **The concurrency hotspot — see PLAN.md §2.3** |
| 40 | `capacity_reservations` | 3 | TTL holds during checkout so abandoned carts release. Separate table so expiry is a clean sweep |
| 41 | `blackout_dates` | 3 | Branch/offering closed |
| 42 | `cutoff_rules` | 2 | "Order by 20:00 for next-day lunch" |

## 5. Ordering · 6

| # | Table | P | Purpose |
|---|---|---|---|
| 43 | `carts` | 3 | Server-side so it survives device switches |
| 44 | `cart_items` | 3 | Variant, qty, options, scheduled_for |
| 45 | `orders` | 1 | order_no, type (one-off / subscription-generated), source_subscription_id, status, totals |
| 46 | `order_lines` | 1 | **Snapshot name + price at time of order.** Never FK-only — vendor price changes must not rewrite history |
| 47 | `order_status_history` | 3 | Transitions with actor and reason. Support lives on this table |
| 48 | `order_notes` | 3 | Customer instructions + vendor internal notes |

## 6. Subscriptions · 7

| # | Table | P | Purpose |
|---|---|---|---|
| 49 | `subscriptions` | 2 | Plan, tier, start/end, status (active/paused/past_due/cancelled), renewal mode |
| 50 | `subscription_cycles` | 2 | The billing period. Prepaid = one payment per cycle, revenue recognised daily |
| 51 | `subscription_schedule_rules` | 2 | Days, meal slots, window, **per-day address override** (office Mon–Fri, home Sat) |
| 52 | `subscription_pauses` | 2 | Date range + credit mode: extend end date *or* wallet credit |
| 53 | `subscription_skips` | 2 | Single-day skip — distinct from a range pause |
| 54 | `subscription_modifications` | 2 | Mid-cycle plan/tier change with the proration record |
| 55 | `subscription_meal_selections` | 2 | Chosen dish per scheduled day. **Highest-volume table in the system** — index and partition accordingly |

## 7. Fulfillment · 5 — the universal primitive

| # | Table | P | Purpose |
|---|---|---|---|
| 56 | `fulfillments` | 1 | **The core table.** order or subscription ref, type (pickup/delivery/visit), date, window, address snapshot, sequence, status. Laundry order = 2 rows; meal day = 1 row |
| 57 | `fulfillment_items` | 3 | Generic container: `ref_type + ref_id + qty + snapshot`. Holds garments *or* meal portions — this is why laundry needs zero tables of its own |
| 58 | `fulfillment_status_history` | 3 | Timestamped transitions with geo |
| 59 | `proof_of_delivery` | 4 | Signature/photo, recipient name, geo |
| 60 | `fulfillment_exceptions` | 4 | Not home, wrong address, refused, damaged + resolution |

## 8. Laundry operations · 3

| # | Table | P | Purpose |
|---|---|---|---|
| 61 | `intake_records` | 2 | **Actual weight/count at the facility vs. the estimate.** Drives the price adjustment in PLAN.md §2.1. Carries photos for disputes |
| 62 | `garment_tags` | 4 | Barcode/RFID per item through the facility. Standard in real laundry ops; it's how you answer "where is my shirt" |
| 63 | `processing_stages` | 4 | sorted → washed → pressed → packed |

## 9. Money · 19

| # | Table | P | Purpose |
|---|---|---|---|
| 64 | `price_quotes` | 1 | State machine: estimated → authorized → adjusted → captured → refunded. Versioned |
| 65 | `quote_lines` | 1 | Line-level breakdown **with tax per line** — VAT treatment differs by item class |
| 66 | `tax_rates` | 5 | Jurisdiction, rate, `effective_from/to`. 5% UAE, 15% KSA. Date-versioned so old invoices reprint correctly |
| 67 | `payment_methods` | 3 | Gateway token, brand, last4, **mandate_ref** for off-session recurring |
| 68 | `payments` | 1 | Intent id, type (auth/capture/refund), status, 3DS status, failure_code |
| 69 | `payment_attempts` | 2 | Dunning retries per cycle: attempt_no, scheduled_at, result |
| 70 | `refunds` | 3 | Amount, reason, gateway ref |
| 71 | `ledger_accounts` | 1 | type + owner. vendor_payable, deferred_revenue, vat_payable, driver_float, customer_wallet |
| 72 | `ledger_journals` | 1 | Groups entries into one balanced transaction |
| 73 | `ledger_entries` | 1 | **Immutable, always balanced pairs.** Never update, never delete |
| 74 | `wallets` | 3 | Materialised customer credit balance — the ledger is truth, this is for UI speed |
| 75 | `wallet_transactions` | 3 | Credit from pauses, refunds, referral rewards |
| 76 | `payouts` | 5 | Vendor, period, gross, commission, adjustments, net, status |
| 77 | `payout_lines` | 5 | Itemisation. **Vendors will dispute payouts** — you need the line detail |
| 78 | `invoices` | 5 | **Sequential legal numbering**, tax details, PDF ref |
| 79 | `invoice_lines` | 5 | |
| 80 | `zatca_submissions` | 5 | KSA only: uuid, hash, **previous_hash (chained)**, QR, cleared_at. See PLAN.md §7 |
| 81 | `cod_collections` | 4 | Cash a driver holds on the platform's behalf |
| 82 | `driver_settlements` | 4 | Daily cash reconciliation against `driver_float` |

## 10. Promotions & loyalty · 6

| # | Table | P | Purpose |
|---|---|---|---|
| 83 | `promotions` | 3 | Code, type, value, **budget cap**, validity |
| 84 | `promotion_rules` | 3 | Min order, first-time-only, vendor/category scope, max uses per user |
| 85 | `promotion_redemptions` | 3 | Prevents reuse *and* gives you discount cost accounting |
| 86 | `referrals` | 6 | Referrer, referee, reward state |
| 87 | `loyalty_accounts` | 6 | Points balance |
| 88 | `loyalty_transactions` | 6 | Earn/burn history |

## 11. Logistics · 8

| # | Table | P | Purpose |
|---|---|---|---|
| 89 | `drivers` | 4 | Licence, status, **nullable vendor_id** — platform driver vs vendor's own |
| 90 | `driver_shifts` | 4 | Start/end/branch |
| 91 | `vehicles` | 4 | Type, plate, capacity |
| 92 | `routes` | 4 | Date, driver, branch, planned distance/duration |
| 93 | `route_stops` | 4 | Route + fulfillment + sequence + ETA + actuals |
| 94 | `driver_assignments` | 4 | Kept separate from `route_stops` because ad-hoc assignments happen outside planned routes |
| 95 | `location_pings` | 4 | **High volume — partition by day, retain ~30 days.** This table will be your largest by row count |
| 96 | `delivery_zones` | 4 | Operational dispatch zones, distinct from vendor `service_areas` |

## 12. Trust & disputes · 5

| # | Table | P | Purpose |
|---|---|---|---|
| 97 | `reviews` | 3 | Rating + comment, moderation status |
| 98 | `review_responses` | 3 | Vendor reply |
| 99 | `review_criteria_scores` | 3 | Punctuality, quality, packaging sub-ratings |
| 100 | `disputes` | 4 | Category, status, resolution, linked refund |
| 101 | `dispute_messages` | 4 | Thread |

## 13. Support & comms · 6

| # | Table | P | Purpose |
|---|---|---|---|
| 102 | `support_tickets` | 5 | Category, priority, assignee, order ref |
| 103 | `ticket_messages` | 5 | |
| 104 | `notification_templates` | 3 | key + channel + **locale** (en/ar) + body |
| 105 | `notifications` | 3 | Delivery log with status and read_at |
| 106 | `notification_preferences` | 5 | Per channel, per category. **Marketing opt-out is a legal requirement**, not a feature |
| 107 | `whatsapp_sessions` | 5 | WhatsApp Business API 24-hour session windows. Primary channel in GCC |

## 14. Platform · 7

| # | Table | P | Purpose |
|---|---|---|---|
| 108 | `outbox_events` | 1 | Transactional outbox — written in the same tx as the state change |
| 109 | `idempotency_keys` | 1 | key + request_hash + stored response. Mobile networks retry; double-charging is unrecoverable |
| 110 | `audit_log` | 3 | Actor, action, before/after, IP |
| 111 | `feature_flags` | 3 | |
| 112 | `app_settings` | 3 | Runtime config |
| 113 | `translations` | 5 | `(entity_type, entity_id, field, locale, value)`. **Vendors author content** — dish names and descriptions can't live in your i18n JSON files |
| 114 | `media_assets` | 3 | Uploads with generated variants |

---

## The health metric

Vertical-specific tables: `menu_cycles`, `menu_cycle_days`, `menu_items`, `menu_day_items`, `laundry_item_types`, `intake_records`, `garment_tags`, `processing_stages`.

**8 of 114 — 7%.** The other 93% is vertical-agnostic. Adding cleaning or salon should cost **0–2 tables** (probably just a `service_types` row plus its strategy code). If a new vertical starts demanding five or more tables, the strategy pattern has leaked — stop and fix it before shipping.

> **Scope of this metric:** it applies to **service verticals riding the Fulfillment engine** — cleaning, salon, tutoring. It does *not* apply to vehicles, which costs 25 tables. That is not a leak in the abstraction: vehicles is a listing kind with its own domain (taxonomy, inspections, offers, classifieds billing), not a service consumed through fulfillments. Don't read the 25 as a failure of the 0–2 rule; they measure different things. See SCOPE-EXTENSIONS §3.

## Where the other tables live

This document covers the services engine only. The rest of the system:

| Scope | Tables | Defined in |
|---|---:|---|
| Services engine (this doc) | 114 | — |
| Reels, feed, moderation | +21 → 135 | [SCOPE-EXTENSIONS.md](./SCOPE-EXTENSIONS.md) §2 · schema in [PHASE-2-MEDIA-AND-FEED.md](./PHASE-2-MEDIA-AND-FEED.md) §6 |
| Vehicles C2C | +25 → 160 | [SCOPE-EXTENSIONS.md](./SCOPE-EXTENSIONS.md) §3 |
| Ranking, experiments, creators, ads | +15 → **~175** | [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §8 |

`reel_views` and all impression/engagement events go to ClickHouse, not Postgres, and are excluded.

## Reading the counts honestly

Against the **video-first calendar** (PLAN-VIDEO-FIRST §8), not PLAN.md's:

- **~15 tables by Phase 1** — a real order with real money end-to-end.
- **~35 by Phase 2** — plus the 12 media/feed tables from PHASE-2 §6.
- **~50 by Phase 4** — actions, engagement, ranking.
- **~90 by Phase 5** — subscriptions land, and **this is where first revenue is realistic**, not earlier.
- **~175 is the full-scope target** across all four documents, not a starting schema. Roughly a third (logistics, ZATCA, loyalty, support tooling, ads) is deferrable past launch.

Do not create these up front. Each phase's migrations add only what that phase's features need — the value of this list is knowing what's *coming*, so P1 tables don't get shaped in a way that blocks them.
