# Phase 8 — Market Readiness

**Status:** Built · 2026-07-28 · implements [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §8 Phase 8 and [PLAN.md](./PLAN.md) §7
**Scope:** notifications, VAT and tax invoicing, ZATCA's hash chain, vendor KYC, the payout gate, content translation, author strikes and appeals.
**Out of scope:** the WhatsApp Business integration itself (§2.4), ZATCA signing (§3.4), support ticketing, loyalty.

PLAN.md §7 is headed *"GCC specifics — treat as blockers, not polish"*. Seven
phases have now been built past that heading. This is the one that takes it
literally: every table and every module below exists because something is not
permitted without it, not because anyone asked for it.

---

## 1. The phase in one line each

| | Was | Is |
|---|---|---|
| Delivery OTP | Issued, hashed, and sent nowhere | Enqueued in the same transaction that hashes it |
| VAT | A `tax_rate_bp` column on quote lines | Date-versioned rates, per-line decomposition that closes exactly |
| Invoices | None | Gapless legal numbering, snapshotted parties, credit notes |
| KSA e-invoicing | Named as a risk | The hash chain, live and empty — the part that cannot be retrofitted |
| Vendor KYC | `vendors.trade_licence_expires`, unread | A gate across the only path to a bank transfer |
| Arabic | Layout and direction (Phase 0) | Vendor-authored content, staleness tracking, SMS segment cost |
| Strikes and appeals | Reporting only | A decaying ladder and an SLA-bound appeal that cannot be self-reviewed |

**17 new tables**, migration `000018_market_readiness`.

---

## 2. The blocker: a code nobody sent

Phase 7 §8.1 recorded this plainly, and docs/README.md turned it into open
decision #8:

> Phase 7 issues a delivery OTP that nothing sends. Until a provider is wired,
> cash-on-delivery cannot complete, because cash requires OTP proof.

Cash on delivery is not a niche payment method here — PLAN.md §6 calls it *"still
meaningful in the region"* — so the honest summary of Phase 7 was that one of the
two payment methods did not work.

### 2.1 Where the enqueue goes, and why it is not "log and continue"

Inside `driver.Dispatch`'s transaction, alongside the OTP hash:

```go
if err := s.sendCode(ctx, tx, current, code); err != nil {
    return err          // fails the whole dispatch
}
```

The instinct is to log the failure and let the stop dispatch anyway. That
instinct is wrong here, and the reason is worth stating because it is the sort
of severity call that gets softened in review: **a dispatched cash stop whose
code was never enqueued is unfinishable.** The driver reaches the door, the
customer has nothing to read out, and the only remedy is a support call while
someone stands in a corridor. Leaving the stop undispatched costs a retry.

The code itself no longer leaves the module. Phase 7 returned it to the HTTP
handler with a comment saying sending it was Phase 8's job; that return value is
gone.

### 2.2 What `driver` knows about notifications

One sentence, expressed as an interface it declares itself:

```go
type Notifier interface {
    NotifyDeliveryCode(ctx context.Context, tx pgx.Tx, in DeliveryCode) (uuid.UUID, error)
}
```

It does not know what a template is, which channel is used, that locales exist,
or that consent exists. `internal/app/notify.go` holds the four-line adapter.

### 2.3 The consent rule, in three places

Marketing can be refused. A delivery OTP cannot. Enforced at three depths, which
is not redundancy — the three catch different failures:

| Where | Catches |
|---|---|
| `CHECK (category IN ('marketing'))` on `notification_preferences` | An insert from a migration, a support tool, or a well-meaning "let users turn off all notifications" ticket |
| `notify.Allowed` consults `Category.Optional()` *before* the stored rows | A row that arrived before the constraint did |
| `notify.Service.SetPreference` returns a 422 with the reason | A customer wondering why the switch is missing |

`TestOnlyMarketingCanBeSwitchedOff` builds an opt-out row for *every* category
on *every* channel in its ladder and asserts only marketing takes effect. It is
the one test in this phase that would catch a regression a reviewer would
otherwise wave through.

### 2.4 Two channel decisions that are not cosmetic

**The ladder is per category, not global.**

- A delivery OTP goes to **SMS first**. It has to reach a phone that may have no
  app, no data connection in a basement car park, and may not belong to the
  buyer — a building watchman receiving a delivery is the normal case here, not
  the edge case.
- Marketing **never uses SMS**. Most expensive channel per message, strictest
  consent rules in both markets: a marketing SMS is regulatory risk bought at a
  premium.
- Moderation notices **avoid WhatsApp**. Telling someone their content was
  removed inside a conversation window they opened for a delivery update reads
  as harassment.

**WhatsApp's 24-hour window is modelled, not discovered.** PLAN.md §7 calls
WhatsApp the primary GCC channel. Its rule — free-form inside 24 hours of a
user-initiated message, pre-approved templates outside it — shapes the sender
rather than merely being logged by it. `whatsapp_sessions` exists so the choice
is made before the send. Without it the design is "try WhatsApp, fall back on
rejection", which means the fallback path is exercised for the first time during
whatever incident made it matter.

The integration itself is not built. The window, the template requirement and
the `provider_template_name` constraint are.

### 2.5 The Arabic SMS cost, surfaced where it can be acted on

`notify.SMSSegments` counts billable segments. GSM-7 fits 160 characters in a
segment; **any Arabic character forces UCS-2, where the limit drops to 70.**

That is more than a factor of two on the cost of every Arabic message, and it is
invisible until the bill arrives. The function exists so the console can show a
segment count next to the Arabic body *while it is being written*, which is the
only moment anyone can shorten it.

---

## 3. Tax and invoicing

### 3.1 Prices are tax-inclusive, so the primitive is subtraction

PLAN.md §7: *"Consumer prices must be displayed tax-inclusive. Bake into the
pricing engine, don't add at checkout."*

So `tax.Split(gross, rateBP)` is the primitive, not `Add`. Building it the other
way round produces a checkout page where the total changes after the customer
has decided, which is prohibited rather than merely unpopular.

**The invariant is exact, not approximate:**

```go
net, tax := Split(gross, rate)
// net + tax == gross, for every gross, for every rate
```

Achieved by rounding the tax and *deriving* the net by subtraction. Rounding
both is the classic way to produce an invoice that is one fils out — and at 5%
it happens on roughly half of all amounts, not on rare edge cases. The invoice
table's own `CHECK (net_minor + tax_minor = gross_minor)` would reject those.

Rounding is **half away from zero**, so a refund is the exact negative of the
charge it reverses. Half-up leaves a fils behind on every reversal, and those
accumulate in `vat_payable` where nobody is looking.

### 3.2 Per line, then summed

`SumLines` decomposes each line and adds the results. Never the document total
at once — the two differ by a fils or two on multi-line orders, and only the
per-line version matches what the invoice prints. A total computed independently
of its own lines is the discrepancy an auditor finds first.

### 3.3 Gapless numbering is the one thing a UUID cannot do

`invoice_series` holds one row per (country, kind, year), taken with
`SELECT ... FOR UPDATE` inside the transaction that inserts the invoice.

A Postgres sequence will not do: sequences skip on rollback *by design*, and a
skipped number is a gap an auditor asks about. So invoice issue serialises per
series, and that is the correct trade — an invoice is issued once per order
rather than once per request, the lock is held for one insert, and the
alternative is a finding rather than a performance problem.

Everything else follows from the same requirement:

- **Issued invoices are never updated.** A correction is a credit note: a new
  document, its own number, pointing at the old one. `CreditNote` copies the
  lines with their signs flipped so both documents close and the pair nets to
  zero.
- **Supplier and buyer are snapshotted.** A vendor who changes their legal name
  has not changed what last month's invoice said, and reprinting from live rows
  would quietly forge it.
- **The tax point is the supply date, not the issue date.** An order delivered
  on 31 December and invoiced on 2 January belongs to the earlier period and
  takes the rate in force then.

### 3.4 ZATCA: the half that cannot be deferred

docs/README.md decision #6 — *"KSA in year one?"* — is still open, and PLAN.md
§7 calls ZATCA *"weeks of work"*. Both halves of that were taken seriously, and
they were split:

**Deferred: the cryptography.** Signing needs a CSID issued by ZATCA against a
real commercial registration, obtained through an onboarding flow that cannot be
run speculatively. Writing an unsigned XML generator now produces code nobody
can test against anything.

**Not deferrable: the hash chain.** Phase 2 e-invoicing requires each invoice's
hash to include the hash of the one before it. The chain is sound only if it has
been unbroken since the first KSA invoice ever issued — and **there is no
retrofit.** You cannot go back and chain invoices that were issued without one,
because altering an issued invoice is precisely what the chain exists to detect.
If KSA launches and the first month has no ICV, the remedy is re-issuing those
invoices to customers.

So the chain ships now, empty, and starts working the day a KSA invoice is first
issued. `VerifyChain` also treats a **gap in the ICV counter as a break even when
the hashes line up** — which is what a deleted-then-reinserted row looks like.

The TLV QR encoder is written out by hand rather than pulled from a library: it
is thirty lines, it is exactly specified, and a dependency's correctness could
not be checked either. `TestQRRoundTrips` uses an Arabic seller name, because
TLV lengths are in **bytes** and a rune-counted implementation passes every
English test.

### 3.5 Whose TRN goes on the invoice

`invoicing.NewService(pool, tax, supplierIsVendor bool)`.

This is docs/README.md decision #4 expressed as a boolean. If the vendor is the
supplier of record, the vendor's TRN goes on the customer's invoice; if the
platform is a deemed supplier, the platform's does. **The two produce different
legal documents from the same order**, and discovering that after launch means
re-issuing every invoice ever sent — so both paths exist and the flag selects.

It is set `true`, matching what `ledger.PostOrderCaptured` already assumes, so
the two beliefs are set from one place rather than drifting.

---

## 4. KYC and the payout gate

PLAN.md §7, one line: *"Trade licence, Emirates ID / national ID, IBAN
verification before first payout."*

The load-bearing words are **before first payout**. This is a precondition on a
bank transfer, not a profile-completeness score.

### 4.1 The gate is in front of the transfer, not beside it

`payouts.Service` declares an `Eligibility` interface and `Issue` refuses without
one. Nil is not a bypass — a deployment that cannot verify a vendor must not pay
one.

The check sits at **issue** rather than at **pay**, which is the less obvious
placement. Pay is where money moves. But a statement issued to a vendor is a
document saying *"we owe you this and will send it"*, and issuing one the
platform is not permitted to settle creates an expectation someone then has to
explain away. Blocking at issue means the vendor sees the reason a week before
the money was due, not on the day it did not arrive.

It runs inside the transaction that flips the state, so a case revoked
concurrently cannot slip past between the read and the write.

### 4.2 Eligibility is computed, never cached

```go
// what changes a vendor's standing is a date passing,
// and no write happens when a licence expires
```

A cached `is_verified` flag keeps paying a vendor whose trade licence lapsed last
Tuesday. Every call recomputes from the documents. The daily sweep that flips
lapsed cases to `expired` changes **no payout outcome** — it exists so the vendor
sees it in their console the next morning.

### 4.3 Reasons, not a boolean

`Status.Reasons` is the whole value of the endpoint. "Not eligible" opens a
support ticket; *"your trade licence expired on 12 June"* gets it renewed.

### 4.4 IBAN check digits, and why the effort is justified

ISO 7064 MOD-97-10, implemented rather than length-checked, because of what the
failure looks like: a payout to a mistyped IBAN is **not** bounced immediately.
It is accepted by the sending bank, rejected somewhere in the correspondent chain
days later, and returned minus fees — or, when the typo happens to produce
another valid account, not returned at all.

`TestASingleWrongCharacterIsCaught` mutates every position through every digit
and asserts none passes.

**Every IBAN change re-enters verification, unconditionally.** "The vendor
changed their IBAN" is the most common payout-fraud pattern there is — an
attacker inside a vendor's console does not need to steal anything, only to
redirect the next statement — and an account inheriting the previous one's
verified state is exactly what they want. The response masks the IBAN even back
to the person who just typed it; responses are logged, cached and screenshotted
in ways request bodies are not.

### 4.5 What is stored, and what is deliberately not

`kyc_documents.document_ref` is a pointer into object storage. **Never the
document.** The same rule `seller_verifications` and `driver_documents` took, and
it is restated in that table's comment because this is where the temptation is
strongest: a scan of an Emirates ID is the highest-value object in this system to
an attacker, PDPL treats it accordingly, and Postgres backups go to more places
than object-storage buckets do.

---

## 5. Arabic — what Phase 0 did and what this phase adds

PLAN.md §7 warns that Arabic is *"not a translation pass"* and is *"cheapest if
adopted in Phase 0, brutal in Phase 5"*. That advice was taken at the time:
locale is a route segment, `dir` is set on `<html>` before the first byte of
layout renders, and the design system uses CSS logical properties. None of that
had to be redone.

What was missing is the half that cannot live in a JSON file. **Dish names, plan
descriptions, branch names and listing titles are written by vendors**, and a
deploy is not an acceptable prerequisite for a kitchen adding a dish in Arabic.

`translations` is keyed `(entity_type, entity_id, field, locale)`. Three columns
on it are doing real work:

- **`source`** — machine translations are labelled. An automatic translation
  displayed as though a human wrote it is how a menu ends up describing a dish in
  terms that are merely wrong in English and offensive in Arabic.
- **`stale_since`** — set when the source-language field changes underneath.
  Without it the failure is silent: a vendor edits the English description, the
  Arabic keeps the old text, and Arabic-speaking customers read a description of
  a product that is no longer sold. **Nobody notices, because the people who
  would notice cannot read the version that changed.** Stale rows are still
  served — a stale Arabic string beats an English one for an Arabic reader — but
  they are countable.
- **`reviewed_at`** — so "a person read this" is a fact rather than an
  assumption.

`Resolve` is batched by design. The shape of the demand is a menu: thirty dishes
with a name and a description each is sixty lookups on one screen. **A product
where Arabic is slower than English is a product that has decided which language
matters.**

---

## 6. Trust & safety scale-up

PLAN-VIDEO-FIRST §6 lists four things. Auto-moderation, the local-context review
queue and in-feed reporting shipped in Phase 2. *"Author strikes, and appeals"*
did not, and they are the two that turn moderation from a sequence of per-item
decisions into something an author experiences as a policy.

### 6.1 The ladder, and the number that had to change

```
minor 1 · major 4 · severe 10
warn ≥1 · review-all ≥4 · suspend ≥8 · ban ≥14
```

**One severe strike suspends and does not ban.** Deliberate: a single decision by
a single moderator must not be able to end a business. A ban needs either a
second severe violation or a sustained pattern, which means at least two people
have looked.

The first attempt used 1/3/8 — and `TestSeveritiesAreDistinguishable` failed,
because three minor caption violations were then arithmetically identical to one
major one. That equality is not a rounding detail: it decides whether someone
careless three times is treated like someone who did something seriously wrong
once, and those need different answers. The gaps were widened.

### 6.2 Strikes expire, and that is the point

A permanent record makes the ladder a one-way ratchet. On a marketplace,
"permanent" means a vendor's business. Severity buys **duration** as well as
weight — 60 days, 180 days, a year — because "forget a minor caption violation in
two months" and "let a severe one shape the next year" are different statements.

Voided strikes stay in the table. A moderation function that deletes its own
reversed decisions cannot be reviewed, and **the rate at which appeals succeed is
the single most useful measure of whether policy is being applied correctly.**

### 6.3 The constraint worth reading

```sql
CONSTRAINT content_appeals_not_self_reviewed
    CHECK (reviewer_id IS NULL OR original_reviewer_id IS NULL
           OR reviewer_id <> original_reviewer_id)
```

An appeal reviewed by the person whose decision is being appealed is not an
appeal. In a moderation team of four, that person is the obvious next reviewer
most of the time. One column removes the failure mode entirely.

### 6.4 The queue only shows what is late

`GET /moderation/appeals` returns **overdue appeals only**. The failure mode of
an appeals process is not that appeals are decided wrongly — it is that they are
never decided, and a queue showing every open appeal buries the breached ones
among them.

### 6.5 Local-context routing

`platform_admins` gains `locales text[]`. PLAN-VIDEO-FIRST §6 warns that a
Western-tuned classifier will under-block against GCC standards; a queue that
routes Arabic content to a reviewer who does not read Arabic produces exactly
that, and no amount of classifier tuning fixes it.

---

## 7. What running the migrations for the first time found

**Every migration in this repository had been written and never executed.** 113
tables across 17 files, no Postgres had parsed any of them, and every phase doc
before this one carried that as an open caveat.

They were all applied in this phase, in order, against `postgis/postgis:16-3.4`:

```
1/u extensions … 17/u logistics_console … 18/u market_readiness  (1.66s total)
150 relations
```

**All 18 applied clean, up and down, first attempt.** That is a better result
than expected and worth recording honestly rather than claiming as design: the
schemas were reviewed carefully, and PostgreSQL was forgiving about the handful
of things that could have gone either way.

Two things did break, and both were in the tooling rather than the schema:

1. **`make migrate-up` had never worked.** `go tool migrate` builds
   golang-migrate's CLI without its database driver build tags, so it fails with
   `unknown driver postgres`. Every migration target had been wrong since Phase
   0 and nothing had exercised them. Fixed with `go run -tags 'postgres'`.

2. **The API had never been booted against a real database.** It boots. `/readyz`
   reports Postgres healthy, an OTP round-trips, and the Phase 8 endpoints answer
   — including the consent refusal, verified live rather than only in a unit test:

   ```
   PUT /v1/me/notification-preferences {"category":"delivery","allowed":false}
   → 422 delivery, account and transactional messages cannot be switched off
   PUT /v1/me/notification-preferences {"category":"marketing","allowed":false}
   → 204
   ```

---

## 8. Knowingly unfinished

Listed rather than discovered.

1. **No real notification provider.** `LogProvider` prints message bodies —
   including delivery codes — and is gated on `!IsProduction()` by the same check
   that gates the sandbox payment gateway. Any channel with nothing configured
   gets `RefusingProvider`, so its messages **fail visibly with a reason** rather
   than sitting pending forever. Wiring an SMS aggregator is one implementation
   of a two-method interface.

2. **ZATCA submission is `pending` and stays there.** The chain is written; the
   XML generator, the signature and the Fatoora client are not (§3.4). No KSA
   invoice can be legally issued yet, and nothing in the code pretends otherwise.

3. **`invoices.pdf_ref` is always null.** The row is the invoice; the PDF is a
   rendering of it. Rendering is not built.

4. **"Your driver is on the way" is not sent.** The trigger exists —
   `driver.Dispatch` already enqueues for cash stops — and extending it is two
   lines. It is not done because a delivery notification with no ETA and no map
   is one people switch off, and the ETA needs live tracking the web driver view
   cannot provide (PHASE-7 §3).

5. **Translations have no writer.** `i18n.Set`, `MarkStale` and `Coverage` exist;
   no console screen calls them, and no vendor-edit path calls `MarkStaleTx`. The
   staleness machinery is therefore correct and dormant.

6. **Strikes are issued by nothing.** `StrikeService.Issue` is not called from
   `moderation.SaveDecision` yet. A moderator removing a reel records the
   decision; it does not yet record a strike, which means the ladder is built and
   unclimbed. This is the largest gap in the phase and it is one function call —
   it is left out because deciding *which* rejections carry which severity is a
   policy question for whoever runs the moderation function, and inventing that
   mapping in code would bury it.

7. **`kyc_documents` has no upload endpoint.** The presigned-upload machinery from
   Phase 2 is the right mechanism and is not wired to this table.

8. **Support tickets, WhatsApp sessions and loyalty are not built.** DATA-MODEL
   §13's `support_tickets`/`ticket_messages` and §10's loyalty tables remain
   unbuilt; `whatsapp_sessions` exists as a table and nothing writes to it.

---

## 9. Open decisions, updated

- **#8 (notifications provider)** — **closed as a blocker, open as a purchase.**
  The delivery OTP is now enqueued transactionally and cash on delivery works
  end-to-end down to the provider call. What remains is choosing an SMS
  aggregator, which is a commercial decision rather than an engineering one.
- **#4 (commission model)** — unchanged, but now visible in a second place:
  `supplierIsVendor` decides whose TRN prints on the customer's invoice (§3.5).
- **#6 (KSA in year one)** — sharper. The chain is in place, so a KSA launch no
  longer requires re-issuing invoices; what it requires is CSID onboarding and a
  signing implementation, which is still the "weeks of work" PLAN.md predicted.
- **#2 (launch city)** — newly relevant: `tax_registrations` needs a real TRN per
  country before a single tax invoice can be issued, and nobody can obtain one
  for a jurisdiction that has not been chosen.
