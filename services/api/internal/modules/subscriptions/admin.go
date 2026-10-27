package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
)

// AdminRow is a subscription as staff see it in a list. The customer is an id
// only: their contact details are read on the customer page, where access is
// logged.
type AdminRow struct {
	ID             uuid.UUID  `json:"id"`
	SubscriptionNo string     `json:"subscription_no"`
	Status         string     `json:"status"`
	UserID         uuid.UUID  `json:"user_id"`
	BranchID       uuid.UUID  `json:"branch_id"`
	BranchName     string     `json:"branch_name"`
	VendorID       uuid.UUID  `json:"vendor_id"`
	VendorName     string     `json:"vendor_name"`
	PlanID         uuid.UUID  `json:"plan_id"`
	PlanName       string     `json:"plan_name"`
	StartDate      time.Time  `json:"start_date"`
	EndDate        *time.Time `json:"end_date"`
	CycleNo        *int       `json:"cycle_no"`
	CycleStatus    *string    `json:"cycle_status"`
	CycleAmount    *int64     `json:"cycle_amount_minor"`
	Currency       *string    `json:"currency"`
	CreatedAt      time.Time  `json:"created_at"`
}

type AdminFilter struct {
	Status   string
	BranchID uuid.NullUUID
	Query    string
	Limit    int
}

var adminStatuses = map[string]bool{
	"": true, "pending_payment": true, "active": true, "paused": true,
	"suspended": true, "cancelled": true, "completed": true,
}

const adminSelect = `
	SELECT s.id, s.subscription_no, s.status, s.user_id, s.branch_id, b.name,
	       s.vendor_id, v.trade_name, s.plan_id, p.name, s.start_date, s.end_date,
	       c.cycle_no, c.status, c.amount_minor, c.currency, s.created_at
	  FROM subscriptions s
	  JOIN vendor_branches b ON b.id = s.branch_id
	  JOIN vendors v ON v.id = s.vendor_id
	  JOIN subscription_plans p ON p.id = s.plan_id
	  LEFT JOIN LATERAL (
	        SELECT cycle_no, status, amount_minor, currency::text AS currency
	          FROM subscription_cycles WHERE subscription_id = s.id
	         ORDER BY cycle_no DESC LIMIT 1) c ON true`

func scanAdminRow(row pgx.Row) (AdminRow, error) {
	var a AdminRow
	err := row.Scan(&a.ID, &a.SubscriptionNo, &a.Status, &a.UserID, &a.BranchID, &a.BranchName,
		&a.VendorID, &a.VendorName, &a.PlanID, &a.PlanName, &a.StartDate, &a.EndDate,
		&a.CycleNo, &a.CycleStatus, &a.CycleAmount, &a.Currency, &a.CreatedAt)
	return a, err
}

func (s *Service) AdminList(ctx context.Context, f AdminFilter) ([]AdminRow, error) {
	if !adminStatuses[f.Status] {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{Field: "status", Code: "invalid_status"}}}
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 100
	}
	q := strings.TrimSpace(f.Query)
	rows, err := s.pool.Query(ctx, adminSelect+`
		 WHERE ($1 = '' OR s.status = $1)
		   AND ($2::uuid IS NULL OR s.branch_id = $2)
		   AND ($3 = '' OR s.subscription_no ILIKE $3 || '%' OR v.trade_name ILIKE '%' || $3 || '%'
		        OR s.id::text = $3 OR s.user_id::text = $3)
		 ORDER BY s.created_at DESC
		 LIMIT $4`, f.Status, f.BranchID, q, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: admin list: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AdminRow, error) { return scanAdminRow(r) })
}

type AdminCycle struct {
	No          int        `json:"cycle_no"`
	StartsOn    time.Time  `json:"starts_on"`
	EndsOn      time.Time  `json:"ends_on"`
	Status      string     `json:"status"`
	AmountMinor int64      `json:"amount_minor"`
	Recognised  int64      `json:"recognised_minor"`
	Currency    string     `json:"currency"`
	OrderID     *uuid.UUID `json:"order_id"`
}

type AdminPause struct {
	From       time.Time `json:"from_date"`
	To         time.Time `json:"to_date"`
	CreditMode string    `json:"credit_mode"`
	State      string    `json:"state"`
	Days       int       `json:"suppressed_days"`
}

type AdminSkip struct {
	Date   time.Time `json:"skip_date"`
	Slot   *string   `json:"meal_slot"`
	Reason *string   `json:"reason"`
}

type AdminEvent struct {
	Type       string         `json:"event_type"`
	From       *string        `json:"from_status"`
	To         *string        `json:"to_status"`
	ActorType  string         `json:"actor_type"`
	ActorID    *uuid.UUID     `json:"actor_id"`
	Metadata   map[string]any `json:"metadata"`
	OccurredAt time.Time      `json:"occurred_at"`
}

type AdminDetail struct {
	AdminRow
	CancelledAt        *time.Time   `json:"cancelled_at"`
	CancellationReason *string      `json:"cancellation_reason"`
	PauseDaysUsed      int          `json:"pause_days_used"`
	AllowedPauseDays   int          `json:"allowed_pause_days"`
	Cycles             []AdminCycle `json:"cycles"`
	Pauses             []AdminPause `json:"pauses"`
	Skips              []AdminSkip  `json:"skips"`
	Events             []AdminEvent `json:"events"`
}

func (s *Service) AdminDetail(ctx context.Context, id uuid.UUID) (AdminDetail, error) {
	var d AdminDetail
	row, err := scanAdminRow(s.pool.QueryRow(ctx, adminSelect+` WHERE s.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return d, httpx.ErrNotFound
	}
	if err != nil {
		return d, fmt.Errorf("subscriptions: admin detail: %w", err)
	}
	d.AdminRow = row
	if err := s.pool.QueryRow(ctx, `
		SELECT s.cancelled_at, s.cancellation_reason, s.pause_days_used, p.allowed_pause_days
		  FROM subscriptions s JOIN subscription_plans p ON p.id = s.plan_id
		 WHERE s.id = $1`, id).Scan(&d.CancelledAt, &d.CancellationReason,
		&d.PauseDaysUsed, &d.AllowedPauseDays); err != nil {
		return d, fmt.Errorf("subscriptions: admin detail: %w", err)
	}

	collect := func(sql string, scan func(pgx.CollectableRow) error) error {
		rows, err := s.pool.Query(ctx, sql, id)
		if err != nil {
			return fmt.Errorf("subscriptions: admin detail: %w", err)
		}
		_, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (struct{}, error) { return struct{}{}, scan(r) })
		return err
	}
	d.Cycles, d.Pauses, d.Skips, d.Events = []AdminCycle{}, []AdminPause{}, []AdminSkip{}, []AdminEvent{}
	if err := collect(`SELECT cycle_no, starts_on, ends_on, status, amount_minor, recognised_minor,
		       currency::text, order_id FROM subscription_cycles
		 WHERE subscription_id = $1 ORDER BY cycle_no DESC`, func(r pgx.CollectableRow) error {
		var c AdminCycle
		err := r.Scan(&c.No, &c.StartsOn, &c.EndsOn, &c.Status, &c.AmountMinor, &c.Recognised, &c.Currency, &c.OrderID)
		d.Cycles = append(d.Cycles, c)
		return err
	}); err != nil {
		return d, err
	}
	if err := collect(`SELECT from_date, to_date, credit_mode, state, suppressed_days
		  FROM subscription_pauses WHERE subscription_id = $1 ORDER BY from_date DESC`, func(r pgx.CollectableRow) error {
		var p AdminPause
		err := r.Scan(&p.From, &p.To, &p.CreditMode, &p.State, &p.Days)
		d.Pauses = append(d.Pauses, p)
		return err
	}); err != nil {
		return d, err
	}
	if err := collect(`SELECT skip_date, meal_slot, reason FROM subscription_skips
		 WHERE subscription_id = $1 ORDER BY skip_date DESC LIMIT 100`, func(r pgx.CollectableRow) error {
		var k AdminSkip
		err := r.Scan(&k.Date, &k.Slot, &k.Reason)
		d.Skips = append(d.Skips, k)
		return err
	}); err != nil {
		return d, err
	}
	if err := collect(`SELECT event_type, from_status, to_status, actor_type, actor_id, metadata, occurred_at
		  FROM subscription_events WHERE subscription_id = $1 ORDER BY occurred_at DESC LIMIT 200`, func(r pgx.CollectableRow) error {
		var e AdminEvent
		err := r.Scan(&e.Type, &e.From, &e.To, &e.ActorType, &e.ActorID, &e.Metadata, &e.OccurredAt)
		d.Events = append(d.Events, e)
		return err
	}); err != nil {
		return d, err
	}
	return d, nil
}
