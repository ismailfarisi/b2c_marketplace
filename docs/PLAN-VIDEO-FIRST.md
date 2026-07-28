# Plan — Video-First Local Discovery

**Status:** Draft v1 · 2026-07-28
**Supersedes** [PLAN.md](./PLAN.md) **on framing, phasing and system centrality.** PLAN.md's engine design — the Fulfillment primitive, the price state machine, the ledger, capacity buckets — remains correct and unchanged. What changes is that it is no longer the core; it becomes one fulfillment backend behind a shared discovery layer.

```
CORE          Feed + ranking + moderation + media
                     │
     ┌───────────────┼───────────────┐
     ▼               ▼               ▼
  service         meal plan      vehicle
  → book         → subscribe    → inquire
```

---

## 1. What actually inverts

| | PLAN.md (services-first) | This plan (video-first) |
|---|---|---|
| Core system | Order + capacity engine | Feed, ranking, moderation, media |
| Primary risk | Operations reliability | **Content supply** |
| Data pattern | Write-heavy, strongly consistent | Read-heavy, eventually consistent |
| Cost driver | Cost per order (covered by commission) | **Cost per view (covered by nothing directly)** |
| Trust & safety | Phase 5 concern | **Launch requirement** |
| Competitive set | Talabat, Dubizzle, local marketplaces | **TikTok and Instagram, for attention** |
| Revenue | Commission on GMV | Commission + listing fees + **boosts/ads** |

The last row of "competitive set" deserves attention. In the services framing you compete with regional marketplaces on reliability and price. Here you compete for attention against the best-funded ranking teams on earth. See §10 for the wedge that makes this survivable.

---

## 2. The new core primitive: Action

PLAN.md's central abstraction was **Fulfillment** — it still is, downstream. This plan adds a second one upstream.

A reel is content. An **Action** is the bridge from content to an outcome:

```
reels
  └── reel_actions[]        -- ordered CTAs on a reel
        kind:   book_service | subscribe_plan | inquire_listing
                | open_menu  | follow | share | external
        target: (target_type, target_id)
        handler resolves to the owning vertical module
```

Everything the feed knows about commerce is `(kind, target_type, target_id)`. Adding a vertical means registering an action kind and its handler — it does **not** mean touching the feed, ranking, or moderation.

This is the exact mirror of the Fulfillment insight: the driver app never learns what a meal is, and the feed never learns what a car is.

**Health metric:** if adding a vertical requires changing feed, ranking or moderation code, the seam has leaked.

---

## 3. Feed architecture

```
candidate generation → hard filters → ranking → diversification → serve
```

**Candidate sources** (union, then dedupe):
- Fresh + geo-local
- Followed authors
- Trending in city/district
- Category affinity from the user's interest profile
- Exploration slice (see below)

**Hard filters** — non-negotiable, applied before ranking:
- **Geo eligibility.** For services this is `ST_Contains(service_area, user_address)`, not a radius — a vendor who can't deliver to you is noise regardless of distance. For vehicles, radius/emirate is correct. *This asymmetry is a feature; keep it explicit in the filter layer.*
- Moderation state = approved
- Language/locale preference
- Blocked authors, already-converted targets

**Ranking.** Not `ORDER BY created_at`. Score on predicted engagement × predicted conversion, with recency decay and author-quality prior. Heuristic weights first, learned model once you have enough events. Reserve **10–15% of the feed for exploration** — without it, new vendors never get impressions and your supply side churns.

**Diversification.** Cap consecutive reels per author and per category. A feed showing five reels from the same kitchen feels broken even if each one scores well.

### The metric conflict — decide this now

A feed optimized for watch time and a feed optimized for orders are different feeds. Since you monetize transactions (not ad impressions, initially), set:

- **North star:** transactions per weekly active user
- **Guardrail:** session depth / D7 retention

Rank on predicted conversion weighted by engagement — not engagement alone. Teams that pick watch time because it's easier to move end up with an entertainment product that doesn't sell anything.

### Storage — a materially bigger footprint than PLAN.md assumed

| Concern | Store |
|---|---|
| Reel metadata, actions, social graph | PostgreSQL |
| Candidate retrieval + geo + multi-filter ranking | **OpenSearch — index from day one, serve from Postgres until ranking needs it.** See [PHASE-2-MEDIA-AND-FEED.md](./PHASE-2-MEDIA-AND-FEED.md) §4.4 |
| Engagement counters | Redis, write-behind to Postgres |
| View/impression events | Kinesis → **ClickHouse.** Never Postgres — this is your highest-volume data by an order of magnitude |
| Embeddings (personalization v2) | pgvector initially |

The Go modular monolith still holds for the transactional core. The feed read path is a separate service boundary with its own scaling characteristics — this is the one place where splitting early is justified.

---

## 4. Content supply — the existential risk

**This kills video-first marketplaces far more often than technology does.**

A transactional marketplace works with 10 vendors and a search box. A feed does not: it needs hundreds of fresh, watchable videos per city per week or it reads as abandoned within one session. And unlike a marketplace, you cannot fill it with backfilled catalog data.

Supply sources, ranked by reliability:

1. **Operator-generated.** Your own team films in the launch district. Expensive, unscalable, and *the only thing that reliably works at cold start*. Budget for it explicitly — 2 people with phones and a gimbal for the first 3 months.
2. **Seller-generated (vehicles).** Intrinsically motivated — a seller wants their car sold. Near-zero acquisition cost.
3. **Vendor-generated (food).** Motivated but needs enablement: templates, lighting kits, a simple in-app editor, and someone chasing them weekly.
4. **Creator program.** Pay local food creators per post. Reliable, costs real money, buys you time.
5. **User-generated.** Video reviews, unboxings. The dream, and the last to arrive. Do not plan around it.

---

## 5. The vertical re-prioritisation — this reverses my earlier advice

Under the services-first framing I recommended laundry first and cars deferred indefinitely. **Under this framing, that ordering is wrong**, and your instinct to pair cars with reels was better than my initial read. Here's the honest reassessment:

| Vertical | Content quality | Supply motivation | Verdict |
|---|---|---|---|
| **Meals / food** | Excellent — food is the most watchable category on every platform | Good, with enablement | **Launch here** |
| **Vehicles** | Strong — walkarounds, engine starts, interiors all perform | **Intrinsic — sellers want to sell** | **Second.** It is a genuine supply engine, not a distraction |
| **Laundry** | Weak for routine service | Low — no one films a pickup | **Last** |

The nuance on laundry: *transformation* content performs well — stain removal, restoration, before/after. Routine "we collected your bag" content does not. So laundry can work in the feed, but only as occasional hero content, never as volume supply. It cannot carry a feed.

**Consequence:** PLAN.md Phase 1 used laundry for the walking skeleton. Keep that — it's the simplest transactional flow and it's an internal engineering milestone, not a launch decision. But the **launch vertical becomes food**, and vehicles moves from "Phase 7, probably never" to "Phase 6, load-bearing".

---

## 6. Trust & safety is now a launch requirement

Vendor content is accountable — every author holds a trade licence and a contract. UGC is not. The moment sellers and users can upload, you need:

- Auto-moderation on every asset (video frames, audio transcript, caption, comments) before publish
- A human review queue with **local context** — GCC standards on nudity, alcohol, gambling and religious/political content are stricter than the US/EU defaults these services ship with. A Western-tuned classifier will under-block and the consequence is regulatory or app-store removal, not a bad review.
- In-feed reporting, author strikes, and appeals
- For vehicles specifically: fraud detection — odometer inconsistency, stolen-vehicle checks, duplicate listings, advance-fee scam patterns in chat

This is **headcount from Phase 2**, scaling at Phase 6. Budget it as a function, not a feature.

---

## 7. Revenue

Three streams, and the third only exists in this framing:

1. **Commission on services/meals GMV** — the PLAN.md ledger, unchanged.
2. **Vehicle listing fees, bumps, dealer packages** — the classifieds model. No commission; the transaction closes offline.
3. **Boosted reels** — vendors and sellers pay for reach. This is a natural, high-margin unlock that Framing A never had: you own an attention surface with hard geo targeting.

Stream 3 is why the per-view cost problem in §9 is solvable — but only after you have enough organic demand that reach is worth buying.

---

## 8. Revised phasing

**Assumption:** ~5–6 engineers (2 Go, 1 web, 1 mobile, 1 data/ML, 1 full-stack), plus designer, plus a content operator from Phase 2 and a moderator from Phase 2.

| Phase | Weeks | Deliverable |
|---|---:|---|
| **0 · Foundations** | 2 | Monorepo, OpenAPI + codegen, auth, schema core, **both seams from SCOPE-EXTENSIONS §5** |
| **1 · Transactional skeleton** | 3 | One vendor, one order, real payment, ledger entry. Proves the engine cheaply. Laundry is fine here |
| **2 · Media + feed v1** | 5 | Upload → transcode → moderate → publish. Geo-filtered chronological feed. **Moderation live. Content operator starts filming** |
| **3 · Action layer** | 3 | `reel_actions`, first conversion loop reel → book. **Instrument conversion honestly before building ranking** |
| **4 · Ranking + social** | 4 | Likes, comments, follows, saves. Real ranking with exploration. Interest profiles. OpenSearch + ClickHouse in place |
| **5 · Food vertical** | 4 | Meal plans, subscriptions, the rolling scheduler. **Launch vertical** |
| **6 · Vehicles** | 7 | Listings, vehicle taxonomy, seller reels, chat, offers, listing packages. Fraud tooling. **Second supply engine** |
| **7 · Clients + logistics** | 5 | Mobile polish, vendor dashboard, driver app or vendor-managed delivery |
| **8 · Market readiness** | 4 | Arabic/RTL, VAT, ZATCA if KSA, KYC, payouts, T&S scale-up |
| **9 · Boosts** | 3 | Paid reach — campaigns, budgets, delivery, billing |
| **10 · Hardening** | 3 | **Load-test capacity contention and the feed read path.** Observability, on-call runbooks, launch readiness |

**~43 weeks.** Roughly **~175 tables** at full scope — 114 engine (DATA-MODEL.md) + 21 reels + 25 vehicles (SCOPE-EXTENSIONS.md) + ~15 for ranking features, experiments, creator programs and ad delivery.

Phase 10 was missing from the first draft of this plan — an oversight, not a decision. PLAN.md §2.3 calls for a targeted load test of capacity reservation, and the video-first plan adds a second contention point the services plan never had: the feed read path under a Ramadan-scale signup spike. Neither survives being skipped.

First real signal arrives at **Phase 3** (~13 weeks): does a reel actually produce a booking? Everything after that is a bet on the answer being yes. Design Phase 3's instrumentation so the answer is unambiguous.

---

## 9. Risks, ranked

| Risk | Why it's serious | Mitigation |
|---|---|---|
| **Content supply cold start** | A thin feed dies in one session. No backfill exists | Operator-generated content from Phase 2. Budget it as opex, not a favour |
| **Cost per view with no conversion** | ~$0.11–0.17/GB egress in the region; you pay for every view, converting or not | Instrument conversion at Phase 3 *before* scaling reach. Preload ≤2 ahead, cap 720p |
| **Competing for attention, not orders** | TikTok has thousands of engineers on ranking | Don't compete on entertainment — see §10 |
| **T&S underestimated** | GCC standards + UGC + anonymous sellers | Moderator from Phase 2, local-context review, fraud tooling by Phase 6 |
| **Metric conflict** | Watch time and orders diverge; watch time is easier to move | North star = transactions/WAU, set in §3 before anyone tunes ranking |
| **Two GTM motions** | Food vendors and car sellers share no playbook | Sequence them (Phase 5, then 6). Never run both cold starts at once |

---

## 10. The wedge

You will not out-entertain TikTok. You don't need to.

**TikTok doesn't know your service area, and it can't take the order.** Every reel in this product is filtered to what is actually deliverable to the viewer's address and terminates in a transaction rather than a link in bio. That is a narrower, more useful feed — not a better-entertaining one.

Which means the product promise is **"everything here is available to you right now"**, not "endless scroll". Concretely, that argues for:

- A feed that can be **exhausted** — finite and locally relevant beats infinite and irrelevant
- Ranking that weights **deliverability and availability** as hard filters, never soft signals
- Success measured in **transactions per session**, never minutes per session

Every design argument in this document should be checked against that sentence. If a feature makes the feed more entertaining but less transactable, it belongs in a different product.

---

## 11. Open questions

1. **Launch city and district?** Operator-led content requires a geographically tight start — one district, not one emirate.
2. **Is the content operator budgeted?** If not, this plan does not work. It's the single highest-leverage line item.
3. **Food vendors: aggregator-style breadth or curated depth?** Curated fits a feed far better; breadth fits search.
4. **Vehicles: private sellers, dealers, or both?** Dealers give volume and pay for packages; private sellers give authenticity and trust. Different products.
5. **Do you have an in-house data/ML capability?** Phase 4 needs one. Heuristic ranking buys ~6 months, no more.
