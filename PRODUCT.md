# Product

<!-- impeccable:product-schema 1 -->

## Platform

adaptive

One product on three surfaces: the Next.js web app (`apps/web`), Android (Jetpack Compose) and iOS (SwiftUI) from the Kotlin Multiplatform app (`apps/mobile-kmm`). Native apps follow each OS's conventions for navigation, controls and system behaviour; the product language is shared.

## Users

Two primary audiences, equal weight:

- **Everyday eaters in the GCC** — office workers and families who want home-style meals delivered on a schedule instead of cooking or ordering daily. Core cuisines: Indian, Kerala, Pakistani, Arab, Filipino and similar. Job: find a kitchen that delivers to their home or office, subscribe, then live with it week to week (pick a dish, skip, pause, renew).
- **Companies buying office lunches** — a company admin sets a lunch policy (covered days, meal slots, per-meal cap, office addresses), invites employees by phone, and pays monthly against one statement. Employees pick a kitchen and pay nothing.

Supporting audiences (not primary, but the product must work for them):

- **Kitchens** — small home-style kitchens and tiffin services, many of which sell today over WhatsApp with a fixed weekday menu. They manage dishes, weekly menus, plans and delivery areas in the vendor portal.
- **Platform staff** — approve kitchens and companies, run merchandising, promos and referrals, moderate reviews, handle subscriptions, statements and payouts in the admin portal.

## Product Purpose

A marketplace for recurring meal subscriptions from everyday home-style kitchens in the GCC. Customers discover kitchens that deliver to their location, subscribe to a weekly menu, and manage it without phone calls or WhatsApp threads. Kitchens get recurring, prepaid demand and payouts without running their own ordering or billing.

Success: customers stay subscribed and renew; companies keep employees on office lunches; kitchens that sell on WhatsApp today move their subscribers onto the platform.

## Positioning

Existing GCC meal-subscription marketplaces (MealPlanet, HUNDO, Dailymealz, Sally, Liked) and single brands (Calo, Right Bite) sell healthy and fitness meals. This product is for the everyday home-style kitchen that none of them list: the "biryani every Monday" tiffin model at everyday prices, with flexibility layered on top.

Mechanisms that back the claim:

- A fixed weekly menu per kitchen, with a default dish and optional alternatives per day and meal slot.
- The four-kitchen rotation: the customer picks four kitchens for a month, the platform assigns one per week, and the price is the sum of those kitchens' weekly prices.
- Office lunch accounts with company-paid subscriptions and monthly statements.
- Arabic and English throughout, including right-to-left layouts.

The video feed and vehicle classifieds in this repository are secondary to the meals marketplace and must not lead product decisions for it.

## Operating Context

- **Discovery is location-first.** Kitchens show only when they deliver to the customer's saved address or current location, their vendor and branch are active, and they have an active plan.
- **The week is the unit.** Menus repeat by weekday (Sunday = 0) and meal slot (breakfast, lunch, dinner). Dish-choice cutoffs, "tomorrow's menu" and delivery all run on Asia/Dubai dates.
- **Subscription lifecycle:** phone-OTP sign-in, address, days, meals and start date, optional promo or referral code, payment; then dish choice, skip, pause, auto-renew or one-tap renew, and cancel with prorated credit.
- **WhatsApp is the customer's existing channel.** Tomorrow's menu, the dish-choice reminder, renewal due and delivery updates are WhatsApp templates (logged locally until a WhatsApp Business account is connected). Referrals share over WhatsApp.
- **Companies** sign up on the web, are approved by staff with a credit limit, and pay by bank transfer against monthly statements with tax invoices (TRN).
- **Kitchens** run their menu, plans and prep sheet in the vendor portal; staff run everything else in the admin portal, where every state change needs an audit reason.

## Capabilities and Constraints

Built today:

- Kitchen discovery with staff-built merchandising (collections, banners, cuisine tiles), a "tomorrow's dishes" rail, per-meal price on every kitchen card and plan, and a "most popular" plan per kitchen.
- Kitchen page with weekly menu, dish alternatives, plans, subscriber-only ratings and a staff-verified food-safety licence badge.
- Checkout, My subscriptions, the four-kitchen rotation builder, promos and controllable trials, renewal, referrals and office lunch accounts on web; storefront, checkout, My meals and rotation on Android and iOS.
- Vendor portal (dishes, weekly menu, plans, profile, dish-photo prompt) and admin portal (kitchens, plans, subscriptions, merchandising, promos, reviews, referrals, companies).

Constraints future work must preserve:

- Money is in minor units; currency is AED today.
- Per-meal price is the cycle price divided by the meals in it, and is shown up front so kitchens compare at a glance.
- Rotation price equals the sum of the four kitchens' weekly prices; the same kitchen never cooks two weeks in a row across a renewal.
- Ratings come only from paying subscribers after a delivery.
- Trials do not convert to paid subscriptions automatically; the customer is prompted to continue.
- Promo and referral discounts come out of platform commission unless staff choose to share the cost with the kitchen; kitchens are paid in full otherwise.
- Customers cannot save a card yet, so renewal bills are paid with one tap rather than charged automatically.

Undecided:

- Customer-facing product name (the UI uses the working label "Meals" / "وجبات"; the web metadata says "Marketplace").
- Payment methods beyond the current provider (Apple Pay, Tabby, Tamara).
- Light nutrition information (daily calories, high-protein and vegetarian filters).

## Brand Commitments

- Bilingual Arabic and English with full right-to-left support on every customer, kitchen, company and staff surface.
- The product serves everyday home-style food, not fitness or diet culture; copy and merchandising should not frame meals around calorie goals or body outcomes.
- No confirmed brand name, logo or tagline yet.

## Evidence on Hand

- Kitchen and dish photos are whatever kitchens upload (`menu_items.image_url`, kitchen cover images). Many dishes have no photo; never substitute stock food photography for a real kitchen's dish.
- Ratings and review counts are real subscriber data only. No testimonials, press, customer counts or kitchen counts exist; do not fabricate them.
- Competitor research (GCC meal-subscription apps and WhatsApp tiffin sellers charging roughly AED 350–600 a month) informed positioning; it is not a claim to publish.

## Product Principles

1. **Everyday food, every day.** Optimise for the person eating this five days a week, not for a one-off impulse order.
2. **Flexibility without phone calls.** Every change a customer would otherwise WhatsApp the kitchen about (swap a dish, skip, pause, renew, cancel) is self-serve and immediate.
3. **Compare honestly.** Per-meal price, delivery coverage, real ratings and verified food-safety status are visible before commitment.
4. **Kitchens stay recognisable.** Each kitchen keeps its identity, menu and attribution inside the marketplace, including inside a rotation.
5. **Arabic is not a translation pass.** Arabic and RTL ship with every feature, at the same quality as English.

## Accessibility & Inclusion

- Arabic and English, right-to-left and left-to-right, on web, Android and iOS.
- Customer surfaces must work on small phones (320px web width) without clipped controls or horizontal overflow.
- Sign-in is by phone OTP; no email or password is required.
