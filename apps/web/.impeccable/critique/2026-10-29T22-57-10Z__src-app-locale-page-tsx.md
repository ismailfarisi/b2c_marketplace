---
target: home mobile
total_score: 28
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 2
target_identity: "file:/home/ubuntu/repos/b2c_marketplace/apps/web/src/app/[locale]/page.tsx"
target_fingerprint: "sha256:0f10eb353a2f0504cfb167da5359f3cc5c067347359b59f116e1b17e1cbba639"
target_path: /home/ubuntu/repos/b2c_marketplace/apps/web/src/app/[locale]/page.tsx
timestamp: 2026-10-29T22-57-10Z
slug: src-app-locale-page-tsx
---
⚠️ DEGRADED: single-context (no sub-agent tool exposed)

Target: meals home on phones (390px and 320px, EN + AR), PR #22 build.

## Design Health Score: 28/40 (Good)
| # | Heuristic | Score | Key issue |
|---|---|---|---|
| 1 | Visibility of system status | 2 | No delivery area shown anywhere on home; "4 kitchens" says nothing about where |
| 2 | Match with the real world | 3 | "a week in rotation" on cards before rotation is explained |
| 3 | User control and freedom | 3 | Search/chips near top, clear CTAs |
| 4 | Consistency | 3 | Kitchen names red on some cards, black on others; same kitchens repeat in staff rail |
| 5 | Error prevention | 3 | n/a-ish on home; per-meal price up front |
| 6 | Recognition | 3 | Dish thumbnails on cards |
| 7 | Flexibility | 3 | Search at screen 1 bottom; meal chips scroll |
| 8 | Minimal design | 3 | Hero + cuisines + banner = ~2.3 screens before first kitchen |
| 9 | Error recovery | 3 | Not exercised on home |
| 10 | Help | 2 | Rotation price precedes the explanation band |

## Priority issues
1. [P1] First kitchen arrives at ~2.3 phone screens: half-screen photo collage, two stacked CTAs, three trust bullets, search, cuisines, full-width banner. Fix: compact hero on phones, banner after first kitchens. /impeccable layout
2. [P1] Location is invisible: no "Delivering to Al Quoz · Change" in header or list heading. /impeccable clarify
3. [P2] Hero photos are salad/power bowls, reading fitness brand (PRODUCT.md says not). Use real kitchens' dish photos. /impeccable polish
4. [P2] Repeats below the fold: staff rail repeats the grid's kitchens; cuisine circles all say "4 Kitchens" and 2 of 3 are letter tiles. /impeccable distill
5. [P2] "AED 320.00 a week in rotation" on the first three cards before the band explains rotation. /impeccable clarify

## Smaller
- ".00" on every price; kitchen-name colour inconsistent; ~250px empty gap above a 2-link footer; test data (UI QA banners, every kitchen "Spice Route Kitchen" + Mediterranean).

Detector: 0 findings on /en and /ar.
