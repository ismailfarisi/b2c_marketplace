# Documentation index

**Current direction:** video-first local discovery for the GCC — a feed is the front door, with services, meal plans and vehicles as actionable outcomes behind it. Go + PostgreSQL backend, Next.js web, Expo mobile.

Read in this order.

| # | Document | Status | What it's for |
|---|---|---|---|
| 1 | [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) | **Authoritative** | Framing, phasing, system centrality. Start here |
| 2 | [PLAN.md](./PLAN.md) | **Partially superseded** | Engine design — Fulfillment, ledger, pricing, capacity, GCC compliance. All still in force. Its §5 phasing is **not** |
| 3 | [DATA-MODEL.md](./DATA-MODEL.md) | Current | 114-table engine catalogue. Reels and vehicles are counted separately — see §"Where the other tables live" |
| 4 | [SCOPE-EXTENSIONS.md](./SCOPE-EXTENSIONS.md) | Analysis; §6 superseded | Why reels and vehicles were scoped the way they were. The §6 recommendation was overtaken by the Framing B decision |
| 5 | [PHASE-2-MEDIA-AND-FEED.md](./PHASE-2-MEDIA-AND-FEED.md) | Built | Media pipeline, moderation gate, geo feed. §10 records what shipped and where it deviates |
| 6 | [PHASE-3-ACTION-LAYER.md](./PHASE-3-ACTION-LAYER.md) | Built | Action layer and conversion attribution. **Read §4 before quoting any conversion number** |

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

---

## Open decisions blocking work

These are unresolved and each changes what gets built. Listed in the order they need answering.

1. **Is the content operator budgeted?** (PLAN-VIDEO-FIRST §11) — if not, the video-first plan does not work. Highest-leverage open item.
2. **Launch city and district** — operator-led filming needs one district, not one emirate.
3. **Does laundry ever launch?** It is the Phase 1 engineering skeleton and it is ranked *last* for feed suitability (PLAN-VIDEO-FIRST §5), but **no phase in §8 actually ships it to customers.** Either add it to a phase or drop it from scope explicitly.
4. **Commission model** (PLAN.md §9) — determines the ledger's account structure.
5. **Who delivers** — platform drivers, vendor's own, or 3PL. Reshapes Phase 7.
6. **KSA in year one?** If yes, scope ZATCA now (PLAN.md §7).
