# Phase 9 — Boosts

**Status:** Built · 2026-07-28 · implements [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §8 Phase 9 and §7 stream 3
**Scope:** campaigns, targeting, pacing, the auction, delivery, confirmation, hourly billing, and the ledger postings behind them.
**Out of scope:** self-serve creative tools, audience builders, conversion-optimised bidding, the advertiser console UI (§7).

PLAN-VIDEO-FIRST §7 lists three revenue streams and says of the third:

> Boosted reels — vendors and sellers pay for reach. This is a natural,
> high-margin unlock that Framing A never had: you own an attention surface with
> hard geo targeting.

§9 says why it matters more than "a third revenue line":

> **Cost per view with no conversion** — ~$0.11–0.17/GB egress in the region;
> you pay for every view, converting or not.

Every other stream earns on transactions. This is the only one that earns on the
views themselves, which is the cost the feed generates.

**6 tables**, migration `000019_boosts`, plus one column on `payments`.

---

## 1. Three rules, each of which costs revenue

This is the phase where the incentives point the wrong way, and the design is
mostly a set of refusals. Stated up front because every one of them is a
decision someone will later propose reversing, and each would make more money
next quarter.

### 1.1 A boosted reel passes the same hard filters as an organic one

Not a copy of the rule — the same predicate, on the same column:

```sql
AND ST_Intersects(r.coverage, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography)
```

`reels.coverage` is resolved once at publish time from the author's service
area. `feed.Service.retrieve` tests it; `boosts.Service.Fill` tests it. There is
no parameter, flag or admin override that relaxes it, and the absence is the
design.

**Why it is worth the revenue it costs.** §10 states the product promise as
*"everything here is available to you right now"* and closes:

> If a feature makes the feed more entertaining but less transactable, it belongs
> in a different product.

Selling reach into feeds where the advertiser cannot deliver would be the most
profitable thing in this schema. It would also be the moment the product stops
being distinguishable from an ad network, and the thing it has against TikTok is
precisely that a reel in this feed is *deliverable to you*.

Campaign targeting **narrows and never widens**. A vendor who sets a 50 km radius
does not thereby deliver to 50 km — that sentence is in the API response on
campaign creation, because it is the most common misunderstanding about how this
works.

### 1.2 Paid slots never come out of the exploration slice

§3 reserves 10–15% of the feed for reels that have not earned their place,
because without it new vendors get no impressions and the supply side churns.

Injecting three sponsored items into a full page pushes three organic items off
the end — and the ones at the end are disproportionately the exploration slots,
which sort last precisely because they have *no score to sort on*. The naive
`items[:limit]` funds advertising by deleting exactly the reels that give new
vendors a chance.

So `feed.truncate` drops earned items before exploratory ones. The cost is borne
by reels that were going to be shown anyway.

`TestPaidSlotsDoNotComeOutOfTheExplorationSlice` is the guard, and the failure it
prevents is unusually slow: it would show up two quarters later as an empty feed
rather than as a bug anyone could trace back here.

### 1.3 Sponsored engagement does not feed the ranker

A boosted reel accumulates views it did not earn. Letting those into
`author_quality` or `reel_features` means **money buys organic rank** — first as
a bug, then as a product feature nobody decided to build.

`Item.Sponsored` is carried through to the client and the analytics for two
different reasons: labelling advertising is a legal requirement in both markets,
and an impression that counted as organic would corrupt the signal Phase 4's
ranking runs on.

A sponsored item also never carries `Explore`. It was not chosen to discover
whether it performs — it was chosen because someone paid, and letting the flag
through would put paid impressions into the measurement of what exploration
costs. That number is what decides whether exploration survives its first budget
review.

---

## 2. The auction

### 2.1 Quality is a multiplier, not a tiebreak

```go
func EffectiveCPM(b Bid) float64 {
    if b.Quality < QualityFloor { return 0 }
    return float64(b.BidCPMinor) * b.Quality
}
```

A pure highest-bid auction fills the feed with the worst content from the richest
advertiser. **That is the default outcome, not an edge case** — the advertisers
most willing to overpay are the ones whose content nobody watches voluntarily.

`TestMoneyDoesNotBeatRelevanceOutright` pits a 50 AED CPM at 0.2 quality against
a 12 AED CPM at 0.9 and asserts the cheap one wins.

The floor (0.15) is a second defence: below it, no price competes. It costs the
revenue of exactly the campaigns that would do the damage.

Quality is the author's organic view-through rate, computed in the candidate
query rather than stored. An author with fewer than 100 impressions gets 0.5 —
good enough to compete, not good enough to beat a proven one. The alternative is
that a first campaign either never serves or always wins, and both are wrong.

### 2.2 First price, not second

Second-price is the right mechanism when bidders are sophisticated and repeated.
It is the wrong one here: a restaurant owner buying 200 AED of reach cannot
reason about truthful bidding, and *"you bid 12 and paid 9"* reads as a bug to
them. First price with a published floor is explicable, which matters more at
this scale than the theoretical efficiency.

### 2.3 What the feed sells

```
one in six · never position 0 · never adjacent
```

- **`MaxSponsoredShare = 6`** — a 20-item page carries three. Set low
  deliberately: the feed's value to an advertiser is that people watch it, and
  the fastest way to stop them is to sell a quarter of it. Every point of it
  should be argued against retention, not against a revenue target.
- **`FirstSponsoredPosition = 2`** — the first reel is the answer to "is this
  feed worth staying in", it is the one every session sees, and it is the one an
  advertiser would pay most for. Which is exactly why it is not for sale.
- **`MinGapBetweenSponsored = 4`** — two adjacent sponsored reels read as an ad
  break, and an ad break is where a scroll ends.

One campaign and one author per page. A viewer who sees the same advertiser
three times in one scroll has been shown one advertiser three times, and the
reaction to that is not more clicks.

**A short organic page stays short.** A feed that runs out of local content must
not fill the remainder with advertising — that is the exact product §10 argues
against, and `TestAShortOrganicPageStaysShort` enforces it.

---

## 3. Money

### 3.1 Micro-minor units, and the bug they exist to prevent

A 12 AED CPM is 1200 minor per thousand impressions — **1.2 fils per
impression**. Rounded down to an integer fils the platform has silently
discounted every campaign by 17%; rounded up, every advertiser is overcharged by
67%. Neither is a rounding error; both are a pricing bug that surfaces in a
quarterly reconciliation.

So impressions accumulate in **millionths of a fils** and the division happens
exactly once, when a charge is posted. `boost_charges.amount_minor` is the only
place a division occurs.

`TestAMillionImpressionsDoNotDrift` asserts a million impressions at a 12 AED CPM
cost exactly 12,000 AED.

There is deliberately no `MicrosFromMinor`. Converting back would let a rounded
figure re-enter the accumulator, and the whole point of the fixed point is that
it never does.

### 3.2 Charged on confirmation, not on serve

```
served    → the auction picked it and the feed returned it
confirmed → the client reported the view threshold was met; charged
expired   → served, never confirmed, reservation released
```

Charging on `served` would be simpler and would bill for impressions nobody saw:
a page prefetched and abandoned costs the advertiser twenty views that never
rendered. Charging on `confirmed` costs this table an extra state and is the
difference between an ad product advertisers trust and one they audit.

The confirmation rides the existing events endpoint — a `view` event, never an
`impression`. That distinction *is* what advertisers are buying.

### 3.3 Reservations, and the failure without them

`boost_daily_spend.reserved_micros` holds the gap between serving and
confirmation. Without it a campaign one impression from its cap can be served
five hundred times in that gap.

The mirror failure is worse and is why `ExpireReservations` runs on a **30-second
tick**: every served-but-unconfirmed impression holds budget, a client that
fetches a page and closes the app leaves a hold behind, and without a sweep those
accumulate until a campaign's daily cap is entirely reservations. The campaign
then stops serving **while its spend reads zero**. That is the failure that looks
like "the ad system is broken" and is arithmetic.

### 3.4 Pacing is linear, and that is a choice

`PacedAllowance` spreads a daily cap linearly across the local day, plus a
one-hour burst so a campaign created at 00:01 can serve at all.

Not because demand is flat — it is emphatically not; iftar is two hours — but
because the alternative is a demand curve this system does not have the data to
estimate, and **a wrong curve paces worse than no curve**. A campaign that wants
iftar buys iftar through `hours_of_day` targeting, which is a statement of intent
rather than a guess.

The failure this prevents is the one every ad platform ships first: a campaign
spends its entire daily budget between 08:00 and 09:00, and the advertiser sees a
day's spend against a morning's reach.

### 3.5 Prepaid, with no credit line anywhere

```sql
CONSTRAINT boost_campaigns_cannot_outspend_funding CHECK (spent_minor <= funded_minor)
```

An advertiser cannot spend money they have not paid. That removes the entire
category of "campaign overran its budget over a weekend", which is the most
common way an ad product turns into a collections problem.

A daily cap above total funding is rejected at creation. It is not a campaign —
it is one day of spending with a misleading label, and the advertiser who set it
meant "no daily limit". Rejecting it makes them say which.

### 3.6 The ledger

Boost money takes the listing-fee shape, which is not a coincidence: both sell
platform inventory rather than a cut of somebody else's supply.

```
funded   Dr gateway receivable   Cr deferred revenue    prepaid, nothing delivered
                                 Cr VAT payable
spend    Dr deferred revenue     Cr platform revenue    impressions delivered
refund   Dr deferred revenue     Cr customer wallet     unspent balance returned
         Dr VAT payable
```

**No vendor payable, ever** — not a payable of zero, no posting. A payout run
must never see these journals, which is why they reference `boost_campaign` and
never an order.

**VAT is booked at funding, not at delivery.** UAE VAT on a supply is due at the
earlier of invoice or payment, and the advertiser pays up front. Deferring the
output tax to match delivery would understate it on every return filed
mid-campaign — the same trap `ledger/listings.go` warns about, repeated in a
second file because it is invisible until a VAT return is filed.

Refunds go to the wallet rather than the card. Advertisers cancel and re-fund
constantly, and routing every unspent 40 AED back through a gateway costs a fee
and a week for money about to be spent on the next campaign.

Charges roll up **hourly**. Posting a double-entry journal for 1.2 fils would
produce a ledger nobody can read and a table two orders of magnitude larger than
`ledger_entries`. Delivery rows are the evidence; charges are the accounting.

---

## 4. The seam, again

`internal/app/seam_test.go` now forbids `modules/feed` (and the rest of core)
from importing `modules/boosts`.

`boosts` is not a vertical — it serves any of them — so the entry needs its own
justification, and it is sharper than the others. The claim the whole design
rests on is §1.1: a sponsored reel passes the same hard filters. That is
enforceable only while the boosts query applies the coverage test *itself*. The
moment the feed can call into the ad system directly, someone passes it a
candidate set that has already skipped the geo check, and the guarantee becomes a
paragraph in a document.

The dependency runs the safe way: the feed hands over a **location** and gets
back reel ids. It never hands over a candidate set for the ad system to rank.

`feed.sponsoredSlots` duplicates `boosts.SponsoredSlots` rather than importing
it. Three lines of arithmetic against the precedent of the first import — the
second is always easier than the first.
`TestSponsoredSlotsMatchTheAdModulesLayout` keeps them in step.

---

## 5. What running it found

Two schema mismatches, both caught by planning the candidate query against the
real database rather than by reading it:

1. **`reels.deleted_at` does not exist** — the column is `removed_at`. Written
   from memory of every other table in this schema, which does use `deleted_at`.
2. **`resolve_reel_coverage(uuid)` returns `boolean`, not a geography.** It
   resolves and stores coverage at publish time; the feed reads `r.coverage`
   directly. The first version of the boosts query passed its boolean result to
   `ST_Intersects`, which is a type error Postgres catches and a code review
   would not have — the name reads exactly like a function that returns a shape.

   The fix made the design claim stronger rather than weaker: the two queries
   now share a *column*, not merely a rule.

Both were found before any of this ran, which is the argument for having a
database available at all. Every migration in this repository had been written
and never executed until Phase 8; that is no longer true.

---

## 6. Not built

1. **No advertiser console.** `POST /v1/boosts`, `GET /v1/boosts` and the
   status/review endpoints exist; there is no screen. An ad platform's console is
   where scope expands forever — audience builders, creative variants,
   dayparting UIs — and none of it is worth building before anyone has bought a
   campaign.

2. **Funding is not wired to a payment.** `boosts.Fund` records a completed
   payment and `payments.campaign_id` exists, but no endpoint takes a card for a
   campaign. The payment path is the one from Phase 1 and adding a third subject
   to `payments.Authorize` is the remaining work; it was left out because the
   sandbox gateway would make it untestable in the only way that matters.

3. **`boost_charges` are not invoiced.** Phase 8's invoicing module can issue
   against a `package_purchase`; the campaign subject is not wired. An advertiser
   gets a ledger entry and no document.

4. **`emirates` targeting is stored and unread.** The column exists;
   `Fill` does not filter on it, because emirate boundaries are not in this
   database and the radius filter covers the launch district. Filtering on a
   column with no data behind it would silently exclude everything.

5. **Conversion-optimised bidding.** `objective` accepts `conversion` and nothing
   changes when it is set. Optimising for conversion needs a model of P(order |
   impression) that Phase 4's heuristic ranker does not have — the same open
   question as docs/README.md #9.

6. **No frequency cap across sessions.** One campaign per page is enforced; a
   viewer who scrolls four pages can see the same campaign four times. The
   `boost_deliveries` dedupe index is per (campaign, viewer, position, day),
   which guards the retry rather than the repeat.

---

## 7. Open decisions, updated

- **#1 (content operator budgeted)** — sharper, and in an uncomfortable
  direction. Boosts only work when there is enough organic demand that reach is
  worth buying (§7). If the operator is not funded, the feed is thin, and reach
  into a thin feed sells nothing — the third revenue stream depends on the first
  line item.
- **#4 (commission model)** — unaffected, and usefully so. Boost revenue is a
  third posting path where the platform is unambiguously the supplier, joining
  listing fees. Whatever the commission answer turns out to be, two of the four
  money paths do not move.
- **#9 (in-house data/ML)** — newly load-bearing. The auction's quality
  multiplier is a view-through rate, which is a defensible proxy and not a
  model. `objective = conversion` needs a real one.
