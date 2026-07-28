# Runbooks

**Status:** Phase 10 · 2026-07-28
**Audience:** whoever is on call at 02:00, who has not read the rest of these documents.

Each entry is: **what you saw**, **what it probably is**, **how to confirm**,
**what to do**. In that order, because at 02:00 nobody reads a design rationale.

Every runbook here corresponds to a failure mode that exists in the code today.
There is no entry for anything hypothetical — a runbook for a system that has not
been built teaches on-call to distrust the whole document.

---

## 0. Before anything

```bash
curl -s https://api.example.ae/healthz    # process is up
curl -s https://api.example.ae/readyz     # dependencies are reachable
curl -s https://api.example.ae/metrics    # everything below
```

`/readyz` returning 503 with `"postgres": "unreachable"` is the only condition
under which the answer is "restart something". Everything else in this document
is a data or capacity problem, and restarting will lose in-flight work without
fixing it.

**Request IDs.** Every response carries `X-Request-Id` and every log line carries
`request_id`. A customer complaint with a screenshot that includes it is worth ten
minutes of grep; one without it is worth ten minutes of guessing.

---

## 1. Cash deliveries are failing at the door

**What you saw:** drivers reporting "the customer's code doesn't work", or
`POST /driver/stops/{id}/complete` returning 409 in a cluster.

**What it probably is:** notifications are not going out, so customers never
received a code. The driver cannot tell that from a customer reading out the
wrong digits.

**Confirm:**

```sql
SELECT status, count(*) FROM notifications
 WHERE template_key = 'delivery.otp' AND created_at > now() - interval '2 hours'
 GROUP BY status;
```

```sql
-- Dispatched cash stops whose code never left.
SELECT f.id, f.otp_issued_at, f.otp_sent_at
  FROM fulfillments f
 WHERE f.status IN ('dispatched', 'arrived')
   AND f.otp_hash IS NOT NULL AND f.otp_sent_at IS NULL;
```

**What to do:**

| `status` | Meaning | Action |
|---|---|---|
| `pending` piling up | The worker's notification loop is not running | Check the worker process; `job_runs_total{job="notify"}` should tick every 10s |
| `failed` with `failure_code` | The SMS provider is rejecting | Provider incident — escalate; the code is in `failure_code` |
| `suppressed` | Consent or template resolution | Read `suppressed_reason`; `no_template` means someone deactivated the `delivery.otp` row |
| No rows at all | `driver.Dispatch` is refusing before enqueue | The stop has no `recipient_phone`; check the order's address record |

**Do not** tell drivers to complete deliveries without proof. The OTP is the only
evidence the customer received goods they paid cash for, and a dispute without it
resolves against the platform.

---

## 2. The feed is empty or thin

**What you saw:** `feed_empty_pages_total` rising, or complaints that the app
shows nothing.

**What it probably is:** almost never a bug. In order of likelihood: no published
reels cover that location; the viewer has seen everything (a feed that can be
exhausted is the design — PLAN-VIDEO-FIRST §10); moderation has a backlog and
nothing is publishing.

**Confirm:**

```sql
-- What covers a point. Substitute the customer's coordinates.
SELECT count(*) FROM reels
 WHERE state = 'published' AND removed_at IS NULL
   AND published_at > now() - interval '30 days'
   AND ST_Intersects(coverage, ST_SetSRID(ST_MakePoint(55.14, 25.08), 4326)::geography);
```

```sql
-- Is the pipeline publishing at all?
SELECT state, count(*) FROM reels
 WHERE created_at > now() - interval '24 hours' GROUP BY state;
```

**What to do:**

- **Zero covering reels** — a content problem, not an incident. It is the
  existential risk §4 names, and the response is the content operator, not a
  deploy.
- **Reels stuck in `processing`** — the media pipeline. Check
  `job_runs_total{job="media"}` and the transcode vendor.
- **Reels stuck in `pending_review`** — the moderation queue. See §3.
- **Reels published but not appearing** — check `coverage_resolved_at`. A NULL
  coverage means the reel is invisible to every feed; the resolution job runs at
  publish, so a NULL means it failed.

---

## 3. The moderation queue is behind

**What you saw:** `sla_breached` items in `GET /moderation/queue`, or authors
complaining about publish delays.

**Confirm:**

```sql
SELECT count(*) FILTER (WHERE now() - entered_at > interval '2 hours') AS breached,
       count(*) AS total,
       max(now() - entered_at) AS oldest
  FROM (SELECT coalesce(metadata_ready_at, created_at) AS entered_at
          FROM reels WHERE state = 'pending_review') q;
```

**What to do:** a deep and *young* queue is a busy day and needs no action. A
shallow and *old* queue is a queue nobody is working, and that is the incident.

Check for claims nobody released:

```sql
SELECT queue, count(*) FROM moderation_assignments
 WHERE released_at IS NULL AND expires_at < now() GROUP BY queue;
```

These self-expire on the next claim attempt (`moderation.Claim` expires stale
holds lazily), so a large number here means nothing is *attempting* to claim —
i.e. no moderator is working, not that the system is stuck.

**Never** bulk-approve to clear a backlog. GCC standards are stricter than the
classifier's defaults (PLAN-VIDEO-FIRST §6) and the consequence of under-blocking
is regulatory or an app-store removal, not a bad review.

---

## 4. Vendors are not being paid

**What you saw:** `POST /vendors/{id}/statements/{id}` issue failing with 422, or
vendors reporting a missing payout.

**What it probably is:** the Phase 8 compliance gate, working.

**Confirm:**

```bash
curl -s /v1/vendors/{vendorId}/compliance -H "Authorization: Bearer $TOKEN"
```

The `blocking` array is the answer, phrased as something the vendor can act on.

**What to do:** the common causes, in order —

1. **Trade licence expired.** `kyc_documents.expires_on` has passed. The vendor
   uploads a renewal; ops re-approves the case.
2. **Bank account awaiting verification.** Someone changed the IBAN. This is
   *deliberately* not fast-tracked: "the vendor changed their IBAN" is the most
   common payout-fraud pattern there is. Verify against the KYC case's legal
   name, by a human, before approving.
3. **No approved case at all.** Onboarding never completed.

**Do not** bypass the gate to unblock a vendor. There is one path to a bank
transfer in this system and the check sits across it; routing around it once
means it can be routed around.

---

## 5. Capacity looks wrong — a branch shows full with no orders

**What you saw:** a vendor says they have no deliveries but the app shows no
slots.

**What it probably is:** leaked reservations. Every abandoned checkout holds
inventory for 15 minutes; if the sweeper is not running, they hold it forever.

**Confirm:**

```sql
SELECT count(*), min(expires_at) FROM capacity_reservations
 WHERE state = 'held' AND expires_at < now();
```

```sql
SELECT b.id, b.capacity_limit, b.consumed, b.reserved
  FROM capacity_buckets b
 WHERE b.branch_id = '…' AND b.bucket_date >= CURRENT_DATE
 ORDER BY b.bucket_date, b.window_id;
```

**What to do:** if expired-but-held reservations exist, the worker's capacity
sweep is not running — that is the incident, and the sweep is what fixes the
symptom. `SELECT release_capacity(id, 'expired')` per reservation is safe to run
manually; it is the same function the sweeper calls, and it is transactional per
reservation.

If `consumed + reserved` is at the limit with no held reservations and no orders,
the count has drifted — which `TestCapacityReleaseIsNotLossy` exists to prevent
and would be a genuine bug. Do not adjust the numbers by hand before capturing
the state; the drift is more valuable than the fix.

---

## 6. The API is slow and the database looks idle

**What you saw:** rising `http_request_duration_seconds`, low database CPU.

**What it probably is:** connection-pool saturation. Every request is waiting for
a connection, so the database is genuinely idle — it has nothing to do.

**Confirm:** `db_pool_acquired_connections` at `db_pool_max_connections`, and
`db_pool_acquire_wait_seconds_total` climbing.

**What to do:** find the query holding connections, not the pool size. Raising
`MaxConns` moves the queue from the application to the database and makes it
worse. The usual culprit is a long transaction — check `pg_stat_activity` for
`state = 'idle in transaction'`, which is a code path that opened a transaction
and then did something slow inside it.

---

## 7. A campaign is not delivering, and its spend reads zero

**What you saw:** an advertiser reports no impressions; `GET /boosts` shows the
campaign active with zero spend.

**What it probably is:** reservations that were never confirmed, filling the daily
cap. Documented in PHASE-9 §3.3 as the failure that looks like "the ad system is
broken" and is arithmetic.

**Confirm:**

```sql
SELECT day, served, charged, spent_micros, reserved_micros, cap_micros
  FROM boost_daily_spend WHERE campaign_id = '…' ORDER BY day DESC LIMIT 7;
```

`reserved_micros` large and `spent_micros` near zero is the signature.

**What to do:** the sweep in the worker (`boosts.ExpireReservations`, every 30
seconds) releases them. If it is not running, that is the incident. Check
`job_runs_total{job="boosts"}`.

If reservations are being *created* and never confirmed while the sweep runs
normally, clients are not reporting view events — which is a client bug, and the
advertiser is being under-served rather than over-charged. That is the right
direction to fail but it still needs fixing.

---

## 8. Ledger imbalance

**What you saw:** `ledger_postings_total{outcome="error"}` non-zero, or a
statement that does not reconcile.

**What to do:** **stop and read before acting.** This is the one section where
the correct first move is to change nothing.

```sql
-- Every journal must balance. This should return zero rows, always.
SELECT j.id, j.event_type, sum(CASE WHEN e.direction = 'debit' THEN e.amount_minor
                                    ELSE -e.amount_minor END) AS imbalance
  FROM ledger_journals j JOIN ledger_entries e ON e.journal_id = j.id
 GROUP BY j.id HAVING sum(CASE WHEN e.direction = 'debit' THEN e.amount_minor
                               ELSE -e.amount_minor END) <> 0;
```

If it returns rows, `ledger.Post`'s validation was bypassed — which should be
impossible, because nothing outside that package writes to `ledger_entries`.
Capture the rows and escalate; do not correct them.

**Never UPDATE or DELETE a ledger entry.** Entries are immutable by design. A
correction is a new balancing journal, and any other approach destroys the
evidence of what actually happened.

If a *payout statement* disagrees with the ledger, the **ledger is right** — the
statement is derived (`payout_statements` carries that as a table comment).
Regenerate the statement.

---

## 9. Restoring from backup

Not written, because there is no backup configuration in this repository to
document. That absence is itself a launch blocker, and it is listed as one in
[PHASE-10 §5](./PHASE-10-HARDENING.md).

A restore runbook written against a backup nobody has taken and nobody has tested
is worse than no runbook: it reads as reassurance.

---

## Escalation

| Condition | Wake someone |
|---|---|
| `/readyz` 503 for > 2 min | Yes |
| Ledger imbalance, any | Yes, immediately |
| Cash deliveries failing (§1) | Yes — customers are at doors |
| Moderation queue old and unworked | Yes if > 6 hours; GCC content risk is regulatory |
| Feed empty in the launch district | No — content problem, morning |
| Campaign not delivering | No — advertiser is under-served, not over-charged |
| p99 latency above SLO | No unless sustained 30 min |
