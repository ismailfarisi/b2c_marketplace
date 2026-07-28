package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/invoicing"
	"github.com/marketplace/api/internal/modules/kyc"
	"github.com/marketplace/api/internal/modules/notify"
)

// The Phase 8 jobs.
//
// A ten-second tick, which is the fastest loop in this process and needs
// justifying against the five-minute logistics loop next door. The thing on
// this loop is the delivery OTP: a driver taps "dispatch" and then drives to a
// door, and the code has to arrive before they do. On a short route that is
// four minutes. A one-minute tick would mean the message is sometimes sent
// after the driver has knocked, and the driver has no way to tell that from a
// customer who is ignoring their phone.
//
// Everything else here is daily work riding the same loop with its own
// interval, because a second ticker for two jobs a day is more machinery than
// the jobs are worth.
const (
	notifyTick     = 10 * time.Second
	complianceTick = 6 * time.Hour
)

type complianceJobs struct {
	Notify   *notify.Service
	KYC      *kyc.Service
	Invoices *invoicing.Service
	Pool     *pgxpool.Pool
}

func runComplianceLoop(ctx context.Context, j complianceJobs) {
	fast := time.NewTicker(notifyTick)
	defer fast.Stop()
	slow := time.NewTicker(complianceTick)
	defer slow.Stop()

	// Once at startup rather than only on the first tick. A worker that has
	// just been restarted after an incident is exactly when a backlog of
	// unsent delivery codes exists, and waiting six hours to create next
	// month's partition is how rows end up in the DEFAULT one.
	dispatchNotifications(ctx, j)
	runDailyCompliance(ctx, j)

	for {
		select {
		case <-ctx.Done():
			return
		case <-fast.C:
			dispatchNotifications(ctx, j)
		case <-slow.C:
			runDailyCompliance(ctx, j)
		}
	}
}

// dispatchNotifications drains the pending queue.
//
// A bounded batch rather than "everything pending". The bound is what keeps one
// enormous marketing send from starving the delivery codes behind it: the queue
// is ordered by creation time, so a batch limit means the next tick gets a turn
// rather than the first send holding the loop until it finishes.
func dispatchNotifications(ctx context.Context, j complianceJobs) {
	ctx, cancel := context.WithTimeout(ctx, notifyTick*3)
	defer cancel()

	sent, failed, err := j.Notify.Dispatch(ctx, 100)
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.ErrorContext(ctx, "notification dispatch failed", "error", err)
		return
	}
	if sent > 0 || failed > 0 {
		slog.InfoContext(ctx, "notifications dispatched", "sent", sent, "failed", failed)
	}
}

func runDailyCompliance(ctx context.Context, j complianceJobs) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Two months ahead. Same reasoning as the impression and ping partitions:
	// a row that lands in DEFAULT blocks the later creation of any partition
	// covering its range, so the recovery from being late is manual.
	if err := j.Notify.EnsurePartitions(ctx, 2); err != nil {
		slog.ErrorContext(ctx, "notification partitions", "error", err)
	}

	// Lapsed KYC. Changes no payout outcome — `Eligible` already refuses an
	// expired case — but it changes what a vendor sees. Someone whose licence
	// lapsed should find out in their console the next morning, not on the day
	// a payout does not arrive.
	if n, err := j.KYC.ExpireLapsedCases(ctx); err != nil {
		slog.ErrorContext(ctx, "kyc expiry sweep", "error", err)
	} else if n > 0 {
		slog.InfoContext(ctx, "kyc cases expired", "count", n)
	}

	issueMissingInvoices(ctx, j)
}

// issueMissingInvoices closes the gap between a captured order and its invoice.
//
// Driven by "captured and has no invoice" rather than by an event, and the
// choice is deliberate. An invoice issued from a webhook is issued once, and if
// that once fails — a rate row missing for a new country, a vendor with no TRN
// on file — the order is captured, the money is taken, and no document exists.
// Nothing retries, because nothing knows it should.
//
// A query for the gap is self-healing: whatever caused the failure is fixed,
// and the next tick issues the backlog.
func issueMissingInvoices(ctx context.Context, j complianceJobs) {
	rows, err := j.Pool.Query(ctx, `
		SELECT o.id
		  FROM orders o
		  JOIN payments p ON p.order_id = o.id AND p.kind = 'capture' AND p.status = 'succeeded'
		 WHERE NOT EXISTS (
		         SELECT 1 FROM invoices i
		          WHERE i.order_id = o.id AND i.status = 'issued' AND i.kind <> 'credit_note')
		 ORDER BY o.created_at
		 LIMIT 200`)
	if err != nil {
		slog.ErrorContext(ctx, "find uninvoiced orders", "error", err)
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			slog.ErrorContext(ctx, "scan uninvoiced order", "error", err)
			return
		}
		ids = append(ids, id)
	}
	rows.Close()

	issued := 0
	for _, id := range ids {
		if _, err := j.Invoices.IssueForOrder(ctx, id); err != nil {
			// Per-order logging rather than aborting the batch. The common
			// causes are per-vendor data problems — no TRN, no tax rate for a
			// new country — and the other 199 orders have customers waiting for
			// a receipt.
			slog.WarnContext(ctx, "invoice not issued", "order_id", id, "error", err)
			continue
		}
		issued++
	}
	if issued > 0 {
		slog.InfoContext(ctx, "invoices issued", "count", issued)
	}
}
