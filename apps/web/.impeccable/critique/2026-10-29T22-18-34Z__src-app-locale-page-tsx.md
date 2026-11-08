---
target: meals storefront home + kitchen page
total_score: 26
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 3
target_identity: "file:/home/ubuntu/repos/b2c_marketplace/apps/web/src/app/[locale]/page.tsx"
target_fingerprint: "sha256:10b2e3a3d34883dd4809c9036e767276a63240e5bd4c627f37df387af65fe320"
target_path: /home/ubuntu/repos/b2c_marketplace/apps/web/src/app/[locale]/page.tsx
timestamp: 2026-10-29T22-18-34Z
slug: src-app-locale-page-tsx
---
⚠️ DEGRADED: single-context (no sub-agent tool exposed in this session)

Target: meals storefront home `/[locale]` (apps/web/src/app/[locale]/page.tsx) plus kitchen page `/[locale]/kitchens/[branchId]`, EN + AR, 1280px and 390px. Detector: source scan clean (0), live overlay on both pages.

## Design Health Score
| # | Heuristic | Score | Key Issue |
|---|---|---|---|
| 1 | Visibility of System Status | 3 | "Delivers to your location", cutoff copy and the sticky "See plans" bar keep state clear |
| 2 | Match System / Real World | 3 | "per 28-day cycle" and "a week in rotation" are platform terms, not how people buy tiffin |
| 3 | User Control and Freedom | 3 | Back to all kitchens, sticky plans jump, skip/pause promised up front |
| 4 | Consistency and Standards | 2 | The same kitchen appears in 3 card styles on one page; Arabic pages show English cuisines, tags and dish names |
| 5 | Error Prevention | 3 | Plans state days, cutoff and pause limits before Subscribe |
| 6 | Recognition Rather Than Recall | 3 | Week-at-a-glance and day tabs keep the menu visible |
| 7 | Flexibility and Efficiency | 2 | Search and meal filters sit at the very bottom of home, after banners and promos |
| 8 | Aesthetic and Minimalist Design | 2 | Kitchen cards stack 8 rows and state the price 3 ways |
| 9 | Error Recovery | 3 | Checkout refusals give reasons (fixed in earlier passes), not re-tested here |
| 10 | Help and Documentation | 2 | Rotation is explained inline once; no help for "2 dishes", "or", trials |
| **Total** | | **26/40** | **Acceptable** |

## Design Specificity Verdict
**LLM assessment:** Mostly authored for this product. The "Home-cooked lunch, every weekday" hero, the tomorrow dish rail, the week-at-a-glance and the 4-kitchen rotation band could not be lifted onto a generic food app. What weakens it: the eyebrow pill above every hero and band (a stock AI-landing pattern), cuisine circles that all reuse the same photo, and a hero collage of stock-looking bowls that reads as a fitness brand, the exact positioning PRODUCT.md rejects.
**Deterministic scan:** source scan 0 findings. Live overlay: home 12 (8 low-contrast, hero-eyebrow-chip, kicker-above-heading, dark-glow, em-dash-overuse), kitchen 14 (13 low-contrast, 1 straggler). Agrees with the eyebrow critique. Caught contrast misses I did not see: white on Chilli Red is 4.4:1 on every CTA and banner; the selected day tab's "or" label is 3.3:1; Chilli text on the popular plan's tint is 4.1:1; the kitchen hero subtitle over the photo measured 1.4:1. Em-dash finding is mostly a false positive (kitchen names in seed data, "Spice Route — Al Quoz").
**Visual overlays:** injected on /en and the kitchen page; both are open as **[Human]** tabs in the VM browser.

## Overall Impression
Appetising and coherent: food leads, the palette is warm and the type is confident. The biggest opportunity is subtraction. Home repeats the same four kitchens three times before the customer can search, and each card asks them to parse three prices.

## What's Working
- Photo-first kitchen cards with per-meal price on the photo: comparison at a glance, as PRODUCT.md demands.
- Kitchen page on phones: day tabs, dish photo cards, week-at-a-glance and a sticky "from AED 60 a meal · See plans" bar in the thumb zone.
- The rotation band (charcoal, four dish circles, progress segments) is the most distinctive moment on the site.

## Priority Issues
- **[P1] Kitchen cards carry too much.** Name, vendor, cuisines, description, 4 dish thumbs, slot chip, cycle price, rotation price: 8 rows, ~1.2 screens per card at 390px. "AED 320.00 a week in rotation" on every card adds a third price before the customer knows what a rotation is. **Fix:** photo + per-meal price, name, cuisines·area, rating/safety, dish thumbs. Move cycle and rotation prices to the kitchen page. **Command:** /impeccable distill
- **[P1] Home order buries the decision.** Order is hero → dish rail → banners → picks → cuisines → rotation → search/filters → all kitchens. Search and meal filters arrive after ~6 phone screens, and the same kitchens appear in the dish rail, Picks and the grid. **Fix:** put search + meal chips directly under the hero; one kitchen grid; collapse Picks into a filter chip or a single rail; keep the rotation band after the first grid row. **Command:** /impeccable layout
- **[P1] Arabic pages are half English.** Cuisine names ("Indian · Arabic · Mediterranean"), "Halal", card descriptions, dish names in thumbnails and "or" under day tabs stay English on /ar. Some is missing `*_ar` data, but cuisine and dietary labels are code-side. **Fix:** translate cuisine/dietary vocab in code; fall back visibly and push kitchens for Arabic names in the vendor portal. **Command:** /impeccable harden
- **[P2] Chilli Red fails AA as a text background.** White on #d9441c is 4.4:1 on every primary button, banner and "2 dishes" chip; Chilli text on cream is 4.2:1; selected day-tab sub-label 3.3:1; kitchen hero subtitle over the photo 1.4:1. **Fix:** a text-safe fill (Chilli Deep #a8300f, or ~#c63c16) for buttons/banners, keep #d9441c for large display and decoration; stronger scrim under the hero text. **Command:** /impeccable audit
- **[P2] Plan cards are dense.** 9 lines plus a trial box; per-meal, cycle and rotation prices all in red. **Fix:** one big price (per meal), cycle price secondary, rotation price behind "In a rotation?" disclosure. **Command:** /impeccable distill

## Persona Red Flags
**Casey (phone, one hand):** filters and full list ~6 screens down; each card ~1.2 screens; on the kitchen page the sticky bar is good but the menu takes 4 screens before Plans.
**Jordan (first-timer):** "AED 320.00 a week in rotation", "per 28-day cycle", the "2 dishes" badge and "or" under day names all need explaining; none do.
**Riley (edge cases):** Green Bowl's dish-thumbnail row shows blank cream squares where photos are missing (the drawn fallback is not applied to thumbnails); cuisine circles all reuse one photo; QA banner text ("UI QA Ramadan") renders as-is.
**Priya, company admin (project persona):** "Office lunches" is in the desktop header only; the phone header shows My meals / العربية / Sign in, so a company admin on a phone cannot find the office sign-up.

## Minor Observations
- Page title is still "Marketplace".
- Coloured glow under the Build a rotation button on the charcoal band (dark-glow).
- Straggler: "cutoff." alone on the weekly-menu intro; add `text-wrap: pretty`.
- Reviews show stars + date + text with no reviewer context (even first name or "Subscriber since").
- Lone "Halal" tag floats on its own row on the kitchen page.

## Questions to Consider
- What if the home page were one searchable kitchen list with merchandising woven in, not stacked above it?
- Does a kitchen card need any price besides "AED 60 a meal"?
- What would the hero look like with a real kitchen's thali instead of a salad bowl?
