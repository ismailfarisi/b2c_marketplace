# Documentation index

**Current direction:** video-first local discovery for the GCC — a feed is the front door, with services, meal plans and vehicles as actionable outcomes behind it. Go + PostgreSQL backend, Next.js web, **Kotlin Multiplatform mobile**.

> **Mobile changed at Phase 7.** The reel feed went to KMM for video playback performance, replacing the Expo scaffold that was in `apps/mobile` — now deleted. The app lives in [`apps/mobile-kmm`](../apps/mobile-kmm/README.md) — shared Kotlin for networking, paging, event batching and the playback *policy*; native Compose/Media3 and SwiftUI/AVFoundation for the decoder and the surface.
>
> The structural consequence is not the framework: `packages/api-client` (TypeScript) can no longer be the mobile client, so **`api/openapi.yaml` becomes the cross-language contract rather than a description of one.** `make gen` needs a Kotlin target alongside the TypeScript one, and spec drift stops being a documentation problem and becomes a runtime deserialisation failure on whichever platform noticed first. See [PHASE-7 §7](./PHASE-7-CLIENTS-AND-LOGISTICS.md) and [apps/mobile-kmm/README.md §4](../apps/mobile-kmm/README.md).

Read in this order.

| # | Document | Status | What it's for |
|---|---|---|---|
| 1 | [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) | **Authoritative** | Framing, phasing, system centrality. Start here |
| 2 | [PLAN.md](./PLAN.md) | **Partially superseded** | Engine design — Fulfillment, ledger, pricing, capacity, GCC compliance. All still in force. Its §5 phasing is **not** |
| 3 | [DATA-MODEL.md](./DATA-MODEL.md) | Current | 114-table engine catalogue. Reels and vehicles are counted separately — see §"Where the other tables live" |
| 4 | [SCOPE-EXTENSIONS.md](./SCOPE-EXTENSIONS.md) | Analysis; §6 superseded | Why reels and vehicles were scoped the way they were. The §6 recommendation was overtaken by the Framing B decision |
| 5 | [PHASE-2-MEDIA-AND-FEED.md](./PHASE-2-MEDIA-AND-FEED.md) | Built | Media pipeline, moderation gate, geo feed. §10 records what shipped and where it deviates |
| 6 | [PHASE-3-ACTION-LAYER.md](./PHASE-3-ACTION-LAYER.md) | Built | Action layer and conversion attribution. **Read §4 before quoting any conversion number** |
| 7 | [PHASE-4-RANKING-AND-SOCIAL.md](./PHASE-4-RANKING-AND-SOCIAL.md) | Built | Ranking, exploration, interest profiles, likes and comments. §1 records why ranking left SQL and what that cost |
| 8 | [PHASE-5-FOOD-AND-SUBSCRIPTIONS.md](./PHASE-5-FOOD-AND-SUBSCRIPTIONS.md) | Built | The launch vertical, the rolling scheduler, prepaid billing. **§2 is the architecture gate's verdict** |
| 9 | [PHASE-6-VEHICLES.md](./PHASE-6-VEHICLES.md) | Built | Classifieds, seller reels, chat, fraud tooling. §2 is the action seam's gate; **read §5 before quoting any vehicle conversion number** |
| 10 | [PHASE-7-CLIENTS-AND-LOGISTICS.md](./PHASE-7-CLIENTS-AND-LOGISTICS.md) | Built | Dispatch, the driver view, cash on delivery, the vendor console, payouts, disputes. §2 is the Fulfillment primitive's gate; §8 lists what is knowingly unfinished |
| 11 | [PHASE-8-MARKET-READINESS.md](./PHASE-8-MARKET-READINESS.md) | Built | Notifications, VAT and tax invoicing, the ZATCA chain, KYC and the payout gate, content translation, strikes and appeals. **§7 records what happened the first time the migrations were actually run** |
| 12 | [PHASE-9-BOOSTS.md](./PHASE-9-BOOSTS.md) | Built | Paid reach. **§1 is three rules that each cost revenue** — read it before proposing a change to any of them |
| 13 | [PHASE-10-HARDENING.md](./PHASE-10-HARDENING.md) | Built | The two load tests, metrics, SLOs, and **§5, an honest launch-readiness assessment** |
| 14 | [RUNBOOKS.md](./RUNBOOKS.md) | Current | What to do at 02:00. One entry per failure mode that exists in the code today |
| 15 | [PLAN-ADMIN-CONSOLE.md](./PLAN-ADMIN-CONSOLE.md) | **Draft — not started** | The admin console eleven phases never budgeted. **~13 weeks, roughly 70% backend** — 136 tables and not one staff-scoped entity read exists. §2 is the specification: ten questions ops actually asks |
| 16 | [QA-FINDINGS.md](./QA-FINDINGS.md) | Current | A full-surface pass in a real browser against a real database. **Nine defects, three critical, none visible to the test suite.** Its "needs your decision" section is the shortest list of things blocking launch that only you can answer |

> **The admin panel was not in the plan.** PLAN-VIDEO-FIRST §8 lists eleven phases and a staff console is a deliverable in none of them; PLAN.md §5 P4 mentions one in passing and the video-first replan dropped it without saying so. It was built after Phase 10 and is documented in [PHASE-10 §7](./PHASE-10-HARDENING.md). Building it found four review surfaces with a `decide` endpoint and no queue behind them — work that could be acted on and not found.

---

## ⚠️ Phase numbers mean two different things

`PLAN.md` §5 and `PLAN-VIDEO-FIRST.md` §8 both number phases from 0, and they do **not** line up. `DATA-MODEL.md`'s per-table `P` column uses the **PLAN.md** numbering. Use this mapping:

| PLAN.md §5 (engine build order · = `DATA-MODEL` P-tags) | PLAN-VIDEO-FIRST §8 (calendar phasing) |
|---|---|
| P0 · Foundations | **Phase 0** · Foundations |
| P1 · Walking skeleton | **Phase 1** · Transactional skeleton |
| P2 · Meals & subscriptions | **Phase 5** · Food vertical |
| P3 · Clients | **Phase 7** · Clients + logistics |
| P4 · Logistics | **Phase 7** · Clients + logistics |
| P5 · Market readiness | **Phase 8** · Market readiness |
| P6 · Hardening | **Phase 10** · Hardening |
| — | **Phases 2, 3, 4, 6, 9** — media, feed, actions, ranking, vehicles, boosts. No PLAN.md equivalent; these are new in the video-first plan |

Read `DATA-MODEL`'s `P` column as **dependency ordering** ("this table is needed once the engine reaches that stage"), not as a calendar date. The calendar lives in PLAN-VIDEO-FIRST §8 only.

---

## Table counts across documents

| Scope | Tables | Defined in |
|---|---:|---|
| Services engine | 114 | DATA-MODEL.md |
| \+ Reels & feed | +21 → 135 | SCOPE-EXTENSIONS §2, schema in PHASE-2 §6 |
| \+ Vehicles C2C | +25 → 160 | SCOPE-EXTENSIONS §3 |
| \+ Ranking, experiments, creators, ads | +15 → **~175** | PLAN-VIDEO-FIRST §8 |

`reel_views` and all impression/engagement events live in ClickHouse, not Postgres, and are excluded from these counts.

**Actually built, as of Phase 10: 136 tables** across migrations 000001–000019
(150 relations including default partitions). The gap to the ~175 above is what no
phase has needed: support ticketing, loyalty and referrals, wallets, promotions,
dunning, and the experiment and creator-programme tables.

The number is worth tracking because the plan's estimate has held — the phases
have not been quietly inventing tables. Phase 8 added 17 against an unbudgeted
line, Phase 9 added 6 against a ~15 budget for "ranking, experiments, creators,
ads", and Phase 10 added none.

**All 19 migrations apply cleanly, up and down, against `postgis/postgis:16-3.4`.**
That was first true at Phase 8; before it, no Postgres had ever parsed any of them.

---

## Open decisions blocking work

These are unresolved and each changes what gets built. Listed in the order they need answering.

1. **Is the content operator budgeted?** (PLAN-VIDEO-FIRST §11) — if not, the video-first plan does not work. Highest-leverage open item, and Phase 9 made the dependency circular in an uncomfortable way: boosts only sell when there is enough organic demand that reach is worth buying, so the third revenue stream depends on this line item too.
2. **Launch city and district** — operator-led filming needs one district, not one emirate. **Newly blocking a second thing:** `tax_registrations` is empty and a TRN cannot be obtained for a jurisdiction nobody has chosen, so no legal invoice can be issued until this is answered (PHASE-8 §9).
3. **Does laundry ever launch?** It is the Phase 1 engineering skeleton and it is ranked *last* for feed suitability (PLAN-VIDEO-FIRST §5), but **no phase in §8 actually ships it to customers.** Either add it to a phase or drop it from scope explicitly. Phase 5 made this cheaper to answer either way: recurrence is now engine, so a weekly laundry pickup is a plan row rather than a project.
4. **Commission model** (PLAN.md §9) — determines the ledger's account structure. Now blocking two paths, not one: `PostOrderCaptured` and `PostSubscriptionRecognised` split VAT identically on the assumption that the **vendor is the supplier of record**. If the platform is a deemed supplier, both change together. Phase 6 added a third posting path that is *unaffected* by this question — on a listing fee the platform is unambiguously the supplier — which is a useful contrast when the answer is being worked out.
5. **Who delivers** — platform drivers, vendor's own, or 3PL. Phase 7 built the machinery *without* taking this decision: `drivers.vendor_id` is nullable, so a platform driver and a vendor-employed one are the same row shape and the same dispatch query. What is still blocked is commercial, not technical — driver contracts, insurance, and whether the platform carries the fleet cost.
6. **KSA in year one?** If yes, scope ZATCA now (PLAN.md §7).
7. **Vehicles: private sellers, dealers, or both?** (PLAN-VIDEO-FIRST §11 q4) — went live with Phase 6 rather than before it, because the code supports both and the schema does not force the choice. It is now blocking two things: which GTM motion runs first, and whether `seller_verifications` needs a KYC provider at launch (a market of private sellers needs one much sooner than a market of licensed dealers).
8. **Notifications provider.** **Closed as an engineering blocker, open as a purchase.** Phase 8 wired the delivery OTP into the dispatch transaction, so cash-on-delivery now works end to end down to the provider call (PHASE-8 §2). What remains is choosing an SMS aggregator — commercial, not engineering. `LogProvider` prints codes to stdout and is gated on non-production; any channel with nothing configured fails visibly rather than sitting pending.
9. **Is there an in-house data/ML capability?** (PLAN-VIDEO-FIRST §11 q5) — Phase 4 shipped heuristic ranking, which the plan itself scopes at roughly six months of runway. This question became live the moment that phase merged.
