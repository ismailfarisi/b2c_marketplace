# Scope Extensions — Reels & Used-Car C2C

**Status:** Analysis · 2026-07-28 · companion to [PLAN.md](./PLAN.md) and [DATA-MODEL.md](./DATA-MODEL.md)

> **Decision taken: Framing B.** §1 posed the fork; the answer was video-first local discovery, and the replan is [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md). **§6's recommendation ("ship reels, defer cars") is superseded** — under Framing B, vehicles is a load-bearing content-supply engine at Phase 6, not a deferral. §1–§5 remain the reference for *why* each piece was scoped the way it was.

Two proposals: (1) short-video reels with location + attribute filters, (2) C2C used-car sales with seller-posted reels.

They differ in kind. Reels is a **feature** that strengthens the existing business. Used-car C2C is a **second business** with different economics, different operations, different trust model and different revenue mechanics. Treating them as one decision is the main risk here.

---

## 1. The strategic fork

### Framing A — Services marketplace with video marketing
Reels are **vendor-authored** content: the cloud kitchen filming today's biryani, the laundry showing their facility. Every reel links to a bookable offering. Video is a discovery channel feeding the existing order engine.

- Reuses vendors, offerings, service areas, identity
- Content supply is bounded and accountable (vendors have trade licences and a contract)
- Directly measurable: reel → offering view → order
- Moderate cost: **+21 tables, ~5–7 weeks**

### Framing B — Video-first local discovery app
The feed is the **front door**. Verticals become different actionable outcomes behind a shared discovery layer: book a service, subscribe to meals, inquire about a car. TikTok Shop / Xiaohongshu shape.

This is a genuinely interesting thesis and it is the only framing where used cars belongs in the same product. But it inverts the architecture — the marketplace engine in PLAN.md becomes *one fulfillment backend among several*, and the feed/ranking/moderation stack becomes the core system rather than an add-on.

**You cannot arrive at B by incrementally bolting features onto A.** Choose deliberately.

### The uncomfortable fact about cross-sell
Laundry/meals is **high-frequency, low-value, ops-heavy** — wins on retention and reliability. Used cars is **low-frequency, high-value, trust-heavy, zero-ops** — wins on inventory liquidity and SEO. A meal subscriber is not buying a car this month; someone browsing Land Cruisers has no reason to book a laundry pickup.

Shared user base transfer value in the short term is close to zero. What *does* transfer: brand, app install, identity/geo infrastructure, the media pipeline, and chat. Real, but thinner than it looks in a pitch deck.

---

## 2. Reels — architecture

### Pipeline
```
client → presigned S3 upload (direct, never through the API)
      → transcode (MediaConvert or Mux) → HLS ladder 360/540/720p + thumbnail
      → auto-moderation (Rekognition video + Transcribe → text moderation)
      → human review queue for flagged
      → publish → feed index
```

Direct-to-S3 upload is non-negotiable — proxying video through a Go API server wastes memory and bandwidth for no gain. **Mux is worth the premium early**: it collapses transcode, storage, delivery and player analytics into one integration. Move to MediaConvert + CloudFront when volume makes the margin matter.

### Feed, filters and ranking

Requirements: geo filter, category/tag/price filters, engagement + recency ranking, no-repeat.

| Stage | Approach |
|---|---|
| MVP | **PostGIS + Postgres.** `ST_DWithin` / `ST_Contains` + score ordering. Good to ~100k reels at modest traffic. Costs you nothing new |
| Scale | **OpenSearch** `geo_distance` + `function_score`. The right answer once multi-filter + ranking + volume converge |

**The important product insight on the location filter:** for services, distance is the wrong primitive. A reel from a vendor who cannot deliver to your address is worthless no matter how close it is. The filter should be a polygon match against data you already have in `service_areas` — strictly better than a radius, and a real differentiator versus generic video feeds.

For cars, radius/emirate *is* correct — there's no service area, and buyers will travel for the right vehicle.

> **Refined in implementation:** rather than branching on reel kind at query time, resolve both semantics into a single `reels.coverage` geography column at publish time — one predicate, one GiST index, one code path. See [PHASE-2-MEDIA-AND-FEED.md](./PHASE-2-MEDIA-AND-FEED.md) §4.1.

### Entities · +21

| Group | Tables | Notes |
|---|---:|---|
| Core | `reels`, `reel_renditions`, `reel_state_history`, `reel_tags` | 4 · `reels` is **polymorphic from day one** on `author_type`/`author_id` and `subject_type`/`subject_id` (see §5) |
| Actions | `reel_actions` | 1 · reel → offering/plan/listing CTA. **This is the revenue link** — an unactionable reel is a cost centre. Defined in PLAN-VIDEO-FIRST §2 |
| Discovery | `hashtags`, `reel_hashtags` | 2 |
| Engagement | `reel_likes`, `reel_comments`, `reel_comment_likes`, `reel_saves`, `reel_shares`, `reel_counters` | 6 · counters materialised from Redis |
| Feed | `feed_impressions`, `follows`, `user_blocks` | 3 · impressions for analytics; serving dedup uses a Redis seen-set |
| Moderation | `moderation_jobs`, `moderation_labels`, `moderation_decisions`, `author_trust`, `reel_reports` | 5 |

`reel_views` and all engagement events go to **ClickHouse, not Postgres** — that stream dwarfs everything else and is excluded from the count. `media_assets` already exists as DATA-MODEL #114 and is reused, not added.

Phase 2's subset of these (12 tables) is specified with full DDL in [PHASE-2-MEDIA-AND-FEED.md](./PHASE-2-MEDIA-AND-FEED.md) §6.

### Cost — the part that gets missed

Video is bandwidth, and bandwidth in the Middle East is expensive (~$0.11–0.17/GB, meaningfully above US rates).

A 30s 720p reel ≈ 5–6 MB per full view. At **100k views/day** that is ~550 GB/day of egress, landing roughly **$2–4k/month all-in** including transcode. Model this against your actual laundry commission per order before committing — a 15% commission on a AED 60 laundry order is AED 9, so the feed needs to be provably driving orders, not just impressions.

Mitigations: preload only 1–2 reels ahead (autoplay feeds burn bandwidth on views nobody wanted), cap at 720p, start on a low rendition, aggressive CDN caching, evaluate H.265/AV1 later.

### Two risks that are legal, not technical

1. **Music licensing.** Letting users overlay popular tracks is copyright infringement at scale — TikTok and Instagram have label deals, you don't. Ship **original audio only**, or integrate a licensed library (Epidemic Sound, Artlist).
2. **GCC content standards** are stricter than the US/EU baseline — nudity, alcohol, gambling, and religious/political content all carry regulatory and app-store risk. Auto-moderation tuned to Western norms will under-block. Budget for human review with local context.

---

## 3. Used cars C2C — why it's a different business

### What it does *not* reuse
No vendors. No capacity. No scheduling. No subscriptions. No fulfillment. No delivery. **No commission** — the transaction happens offline at an RTA counter, so you never touch the money.

That last point is the structural one: **your entire money module (19 tables, the ledger, payouts, VAT on GMV) does not apply.** Car classifieds revenue is listing fees, bump/feature fees, lead fees, and dealer subscription packages — the Dubizzle/YallaMotor model. That's a different billing surface you'd have to build: `listing_packages`, `dealer_subscriptions`, credit/quota tracking.

### What it *does* reuse
Users, identity, addresses/geo, media pipeline, reels, chat, reviews, notifications, moderation. Genuinely useful — roughly the infrastructure third of the system.

### Entities · +25

| Group | Tables |
|---|---|
| Listings | `vehicle_listings`, `listing_photos`, `listing_videos`, `listing_status_history`, `price_history` |
| Vehicle taxonomy | `vehicle_makes`, `vehicle_models`, `vehicle_trims`, `vehicle_specs` — **reference data you buy or license, don't hand-build** |
| Attributes | year, mileage, transmission, fuel, body, colour, VIN, and **regional spec (GCC / US / Japanese import)** — spec alone swings GCC resale value 20–30% and must be a first-class filter |
| Trust | `inspection_reports`, `vehicle_history_checks`, `seller_verifications`, `fraud_signals`, `trust_scores`, `listing_moderation` |
| Demand side | `leads`, `offers`, `saved_searches`, `search_alerts`, `favorites` |
| Chat | `conversations`, `messages`, `message_attachments` — **required**, not optional: exposing seller phone numbers invites spam and advance-fee fraud |
| Monetisation | `listing_packages`, `listing_promotions`, `dealer_subscriptions` |
| Valuation | `market_valuations` — "what's this car worth" is the highest-value feature in the category |

One genuine synergy: **test drive bookings reuse `fulfillments` + `capacity_buckets` unchanged.** The Fulfillment primitive from PLAN.md §1 holds up here, which is a good sign for the abstraction.

### GCC regulatory
Ownership transfer runs through the emirate authority (RTA in Dubai) or Absher/Tam in KSA and requires both parties, valid insurance and testing. Vehicle history lookups may be integratable via Emirates Vehicle Gate / Absher — worth investigating, since it's a moat.

**Fraud is the defining operational problem**: odometer rollback, concealed accident history, stolen vehicles, and advance-fee scams targeting buyers. This needs a trust & safety function — headcount, not a feature flag. Escrow would solve much of it but implies payment-institution licensing.

---

## 4. What this does to the plan

| | Tables | Added timeline |
|---|---:|---|
| Services engine alone | 114 | ~23 weeks |
| \+ Reels & feed | +21 → **135** | +5–7 weeks |
| \+ Used cars C2C | +25 → **160** | +10–14 weeks |
| \+ Ranking, experiments, creators, ads¹ | +15 → **~175** | included below |
| **All of it (Framing B)** | **~175** | **~43 weeks** |

¹ Only exists under Framing B — a boosted-reels ad surface and a ranking stack have no place in Framing A. Itemised in PLAN-VIDEO-FIRST §8.

Plus: ~$2–4k/month video opex, a trust & safety function, and a second go-to-market motion with no shared playbook.

---

## 5. The cheap seams — build these now

Two changes cost approximately nothing today and save a painful migration later. Do these even if you never ship either feature.

**1. Make reels polymorphic from the first migration.**
```sql
reels(
  id,
  author_type, author_id,      -- 'vendor_branch' | 'user'
  subject_type, subject_id,    -- 'service_offering' | 'vehicle_listing' | NULL
  ...
)
```
Retrofitting polymorphism onto a `reels.offering_id` column after launch means a data migration across your highest-volume tables. Writing it this way on day one is free.

**2. Keep `service_offerings` discovery-separable.**
Hold the discovery fields — `slug`, `status`, `published_at`, `geo`, `visibility` — as a clean, self-contained group. If a second listing kind ever arrives, extracting a shared `listings` supertype becomes a mechanical migration instead of a redesign.

Do **not** create the `listings` supertype now. With one listing kind it's premature abstraction that adds a join to every catalog query. Just keep the door unlocked.

---

## 6. Recommendation — ⚠️ SUPERSEDED

> **This section is kept for the record only.** It recommended Framing A. **Framing B was chosen**, which inverts points 2 and 3 below: vehicles is no longer a deferral but a load-bearing content-supply engine at Phase 6, and reels is the core system rather than a Phase 4–5 feature. The live plan is [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §8. Point 1 (apply the §5 seams in Phase 0) still stands.
>
> The reasoning below is still worth reading for *why* the sequencing argument cuts the way it does — it's the strongest case against the path now being taken, and it names the risks that Framing B has to actively manage (content supply, two GTM motions, unproven ops).

**Ship reels. Defer cars. Decide the framing consciously.**

1. **Now (Phase 0):** apply the two seams in §5. Free.
2. **Phase 4–5:** reels, **vendor-authored only**. Prove the video pipeline, the cost model, and moderation on content where every author has a trade licence and a contract. Measure reel → order conversion honestly; if the feed doesn't move orders, you've learned that for ~6 weeks instead of ~20.
3. **Phase 7+:** revisit cars as a decision, not a feature. If the answer is yes, build it as a **separate product surface sharing identity, media, chat and moderation as internal services** — not as another vertical inside the services engine.

**The sequencing argument matters more than the architecture argument.** Both features are buildable and neither is a mistake in isolation. But laundry and meals is an operations business that wins on reliability, and you have not yet proven you can get a customer's shirts back clean and on time. Every week spent on a car feed is a week not spent on the thing that makes people stay.

Careem, Noon and Talabat all became super-apps — every one of them *after* winning a single vertical, and usually by acquisition rather than by building.

### The question to answer before writing any of this code
Is the reel feature **marketing for the services business** (Framing A), or **the actual product** with services as one of several outcomes (Framing B)?

If A — everything above stands, defer cars indefinitely.
If B — stop and replan. The feed becomes the core system, PLAN.md's engine becomes a subordinate module, and used cars is a reasonable second listing kind rather than a distraction. That's a legitimate product but it's a different document.
