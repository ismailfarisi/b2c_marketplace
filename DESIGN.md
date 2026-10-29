---
name: Meals
description: Everyday home-style meal subscriptions, food first.
colors:
  chilli-red: "#c43b16"
  chilli-deep: "#a8300f"
  saffron: "#f6b21b"
  saffron-soft: "#fff1cc"
  saffron-ink: "#7a5200"
  leaf-green: "#276c44"
  leaf-soft: "#e3f2e8"
  blush: "#fde6d8"
  ghee-paper: "#fff8f0"
  surface: "#ffffff"
  tag-cream: "#fbeee2"
  line: "#f0e2d2"
  ink: "#1e1410"
  muted: "#76675b"
  char: "#22160f"
  error: "#b23b2e"
typography:
  display:
    fontFamily: "Bricolage Grotesque, IBM Plex Sans Arabic, system-ui, sans-serif"
    fontSize: "clamp(2.2rem, 5vw, 3.8rem)"
    fontWeight: 800
    lineHeight: 1.03
    letterSpacing: "-0.035em"
  headline:
    fontFamily: "Bricolage Grotesque, IBM Plex Sans Arabic, system-ui, sans-serif"
    fontSize: "clamp(1.4rem, 2.6vw, 1.75rem)"
    fontWeight: 700
    letterSpacing: "-0.02em"
  title:
    fontFamily: "Bricolage Grotesque, IBM Plex Sans Arabic, system-ui, sans-serif"
    fontSize: "1.05rem"
    fontWeight: 800
  body:
    fontFamily: "DM Sans, IBM Plex Sans Arabic, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
  label:
    fontFamily: "DM Sans, IBM Plex Sans Arabic, system-ui, sans-serif"
    fontSize: "0.8rem"
    fontWeight: 700
rounded:
  sm: "0.5rem"
  md: "1rem"
  lg: "1.5rem"
  pill: "999px"
spacing:
  sm: "0.75rem"
  md: "1rem"
  lg: "1.25rem"
  grid: "1.4rem"
components:
  button-primary:
    backgroundColor: "{colors.chilli-red}"
    textColor: "{colors.surface}"
    rounded: "{rounded.pill}"
    padding: "0.8rem 1.4rem"
    typography: "{typography.label}"
  button-primary-hover:
    backgroundColor: "{colors.chilli-deep}"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    padding: "0.8rem 1.4rem"
  button-ghost-hover:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.ghee-paper}"
  chip:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
  chip-selected:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.ghee-paper}"
    rounded: "{rounded.pill}"
  eyebrow:
    backgroundColor: "{colors.saffron-soft}"
    textColor: "{colors.saffron-ink}"
    rounded: "{rounded.pill}"
    padding: "0.35rem 0.8rem"
    typography: "{typography.label}"
  input-search:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    padding: "0.75rem 1.1rem"
  card-kitchen:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.lg}"
  banner-offer:
    backgroundColor: "{colors.chilli-red}"
    textColor: "{colors.surface}"
    rounded: "{rounded.lg}"
---

# Design System: Meals

Scope: the customer meals surfaces on web (`apps/web`, everything inside the `.ms` scope of `src/app/globals.css`) and the meals tab on Android (`MealsParts.kt` / `MealsTab.kt`, palette object `M`) and iOS (`MealsScreen.swift`, palette `Warm`). The vendor console and staff admin use the separate operational shell in `src/app/admin.css` and are not covered here. The video feed keeps its own dark styling.

## Overview

**Creative North Star: "The Hungry Weekday"**

This is food you eat five days a week, so the interface should make an ordinary Tuesday lunch look worth waiting for. Dish and kitchen photos carry the appetite. Chilli red marks every action, saffron and leaf green mark offers and trust, and everything sits on a warm ghee-paper cream rather than clinical white.

The feel is warm and generous: soft, round corners, roomy cards and a gentle lift when something can be tapped. Headings are bold, upright and slightly tight, never italic or serif. Arabic is a first-class citizen with its own face and layout, not a mirrored afterthought.

**Key Characteristics:**
- Food photography leads every card and hero. When a kitchen hasn't uploaded one, a drawn tinted tile stands in.
- Three food colours do all the talking: chilli for action, saffron for offers, leaf for trust.
- Warm cream canvas, white cards, soft brown shadows.
- Pill-shaped controls and big rounded cards.
- Bold, upright grotesque display type with a friendly geometric body face.

## Colors

A spice-cupboard palette on cream: one hot red, one golden yellow, one fresh green, and warm browns instead of greys.

### Primary
- **Chilli Red** (`chilli-red`): every primary action (Subscribe, See plans, Apply), the offer banner background, selected bottom-nav items on mobile. Hover and pressed deepen to **Chilli Deep** (`chilli-deep`), which also anchors the rotation promo gradient with Char. Chilli Red is tuned to clear 4.5:1 for white text on it and for red text on Cream Paper; do not lighten it.

### Secondary
- **Saffron** (`saffron`): small highlight accents and offer moments. Its soft tint **Saffron Soft** (`saffron-soft`) with **Saffron Ink** (`saffron-ink`) text forms the eyebrow pill above headings and the offer tags.

### Tertiary
- **Leaf Green** (`leaf-green`): the food-safety verified badge, confirmations and "delivers to you" states, on **Leaf Soft** (`leaf-soft`) backgrounds.

### Neutral
- **Ghee Paper** (`ghee-paper`): the page canvas everywhere in meals.
- **Surface** (`surface`): cards, inputs, chips, the sticky plans bar.
- **Blush** (`blush`) and **Tag Cream** (`tag-cream`): quiet fills for tags and soft accent panels.
- **Line** (`line`): hairline borders on inputs, chips and dividers.
- **Ink** (`ink`): body text and headings; also the selected-chip fill.
- **Muted** (`muted`): secondary text, metadata, inactive nav labels.
- **Char** (`char`): the dark promo panels (rotation promo, footer-style bands).
- **Error** (`error`): validation and failure messages only.

A drawn-cover set of four tint/ink pairs (peach, mint, butter, rose: `--ms-cover-1..4`) colours the fallback tiles for kitchens and dishes without photos; the colour is picked deterministically from the name so a kitchen always gets the same tile.

On web, `prefers-color-scheme: dark` swaps the canvas to a deep roasted brown with a brighter chilli (`#f0643a`). The mobile meals tab is light-only.

**The Chilli Means Go Rule.** Chilli red is reserved for things the customer can do, plus the rating star. Prices, names and metadata stay in Ink and Muted.

**The No Grey Rule.** Neutrals are warm browns. Never introduce a cool or pure grey.

## Typography

**Display Font:** Bricolage Grotesque (weights 500/700/800), falling back to IBM Plex Sans Arabic and system-ui
**Body Font:** DM Sans (400–700)
**Arabic:** IBM Plex Sans Arabic (400–700) replaces both faces on Arabic pages

**Character:** a chunky, slightly quirky grotesque for headlines that feels like a menu board, paired with a clean, rounded geometric body face.

### Hierarchy
- **Display** (800, fluid 2.2–3.8rem, line-height 1.03, tracking −0.035em): page hero headlines on home and the kitchen hero.
- **Headline** (700, fluid 1.4–1.75rem, tracking −0.02em): section titles such as "On the menu tomorrow" and "Plans".
- **Title** (800, 1.05rem, display face): card titles, kitchen names, the price in the sticky plans bar.
- **Body** (400, 1rem): descriptions, menu text, form copy. Metadata steps down to 0.9rem in Muted.
- **Label** (700, 0.8rem): eyebrows, badges, tags and button text.

On mobile, Android uses the system sans (`FontFamily.SansSerif`) and iOS the system font (`.system(size:weight: .bold)`) at Bold/ExtraBold for headings. The web fonts are not bundled in the apps.

**The Upright Rule.** Headings are bold and upright. No italics, no serif display faces.

**The Arabic Reset Rule.** On Arabic pages, letter-spacing goes to 0 and heading line-height opens to 1.3. Tight Latin tracking breaks Arabic joins.

## Layout

Content sits in a centred page column (`--page-max`, 84rem for wide pages). Kitchen cards flow in an auto-fill grid (minimum 17rem per card, 1.4rem gap) that collapses to a single column on phones. The kitchen page is a two-column layout (menu plus sticky plans column) that stacks below 52rem. At that width the plans column stops being sticky and a fixed "See plans" bar appears at the bottom, padded for the device's safe-area inset.

Horizontal rails (tomorrow's dishes, cuisine circles) scroll sideways instead of wrapping. Breakpoints in use: 22rem, 30rem, 36rem, 40rem, 52rem and 56rem. Layouts must hold at 320px wide in both languages.

All positioning uses logical properties (`inset-inline`, `margin-inline-start`, `padding-block-end`) so RTL works without overrides.

**The Logical Properties Rule.** Never write `left`, `right`, `margin-left` or `padding-right` in meals CSS. Every physical property is a future RTL bug.

## Elevation & Depth

Hybrid: surfaces are white cards on a cream canvas, lifted by soft, warm-brown shadows rather than borders.

### Shadow Vocabulary
- **Rest** (`0 1px 2px rgb(30 20 16 / 0.06), 0 12px 32px rgb(30 20 16 / 0.08)`): kitchen cards and panels at rest.
- **Lift** (`0 2px 4px rgb(30 20 16 / 0.08), 0 20px 44px rgb(30 20 16 / 0.14)`): hovered or focused cards.
- **Chilli glow** (`0 6px 16px` of chilli at 30%): under primary buttons.
- **Sticky edge** (`0 -8px 24px rgb(30 20 16 / 0.08)`): the bottom "See plans" bar.

**The Gentle Lift Rule.** Tappable cards rise 3px and gain the Lift shadow on hover or focus; buttons rise 1px. Motion is 150–200ms and switches off under `prefers-reduced-motion`.

## Shapes

Soft and round. Cards and banners use generous 1.5rem corners (`rounded.lg`), smaller panels 1rem (`rounded.md`), small inner elements 0.5rem. Buttons, chips, search fields, eyebrows and tags are full pills. Cuisine tiles are circles. Photos inside cards bleed to the card edge at 4:3 and are clipped by the card's radius.

On mobile the same language maps to 18dp card corners, 12–16dp for inner panels and fully rounded chips and buttons.

## Components

### Buttons
- **Shape:** full pill (999px).
- **Primary:** Chilli Red with white bold label, 0.8rem × 1.4rem padding and the chilli glow shadow.
- **Hover / Focus:** deepens to Chilli Deep and rises 1px.
- **Ghost:** transparent with a 1.5px ink outline; fills with ink on hover.

### Chips
- **Style:** white pill, 1.5px Line border, 600 weight.
- **State:** selected chips fill with Ink and Ghee Paper text. Used for meal-slot filters and weekday tabs.

### Cards / Containers
- **Kitchen card:** white, 1.5rem corners, no border, Rest shadow. A full-bleed 4:3 photo on top carries the per-meal price and rating; below are the name, cuisines, area, the food-safety badge and a row of this week's dish thumbnails.
- **Dish card:** photo or drawn tile, dish name, meal slot, and an "alternative available" marker.
- **Plan card:** plan name, meals per cycle, per-meal price, cycle price; the most popular plan carries a "Most popular" tag.

### Inputs / Fields
- **Search:** white pill, 1.5px Line border, 0.75rem × 1.1rem padding.
- **Error:** message in Error red under the field.

### Navigation
- **Web header:** sticky, translucent Ghee Paper (90%) bar with the delivery area and search.
- **Mobile bottom bar:** white while the meals tab is active, with Chilli Red for the selected item and Muted for the rest; it returns to the dark gradient on video tabs.

### Eyebrow
Saffron Soft pill with Saffron Ink 0.8rem bold label above section headlines ("Tomorrow", "New kitchens").

### Food Photo with Drawn Fallback
The signature component. Every kitchen and dish image renders through it: the real upload when present, otherwise a tile in one of the four cover tints with the dish or kitchen initial in the matching ink. It is the same on web (`KitchenCover`), Android and iOS (`FoodPhoto`).

### Sticky "See plans" Bar
Phone-width kitchen pages only. White bar fixed to the bottom with the starting price in Title type and a primary button, clearing the safe area and the mobile navigation bar.

## Do's and Don'ts

### Do:
- **Do** lead cards and heroes with the kitchen's real dish photos, falling back to the drawn tile.
- **Do** show the per-meal price on the photo of every kitchen card.
- **Do** keep chilli red for actions, saffron for offers and eyebrows, leaf green for trust and success.
- **Do** use logical CSS properties and test every screen in Arabic at 320px.
- **Do** use the Rest shadow at rest and the Lift shadow on hover/focus, and honour reduced motion.

### Don't:
- **Don't** use stock food photography in place of a kitchen's own dishes.
- **Don't** use italic or serif headings.
- **Don't** introduce cool greys or pure black; use Ink, Muted and Char.
- **Don't** put fake browser bars or phone frames around screenshots or showcases.
- **Don't** reuse the meals palette in the vendor console or admin; they keep the operational shell in `admin.css`.
