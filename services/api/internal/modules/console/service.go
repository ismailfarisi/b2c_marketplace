// Package console is the vendor dashboard's read layer.
//
// It is the one place in Phase 7 that *is* allowed to know what is being
// delivered, and the distinction from `dispatch` is worth stating because the
// two look superficially similar.
//
// Dispatch answers "where does the van go", which is the same question for a
// meal box and a laundry bag, so it reads the Fulfillment seam and nothing else.
// The console answers "what does my kitchen need to cook by six", which is
// meaningless without knowing what a meal is. Forcing the console through the
// seam would produce a dashboard that shows a vendor a list of addresses — which
// is precisely the screen nobody can run a business from.
//
// So the seam is not a rule about layers, it is a rule about *questions*. That
// is why `internal/app/seam_test.go` guards dispatch and driver and deliberately
// does not guard this package.
package console

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Membership is what a signed-in user may see.
type Membership struct {
	VendorID uuid.UUID
	Role     string
	// BranchID is null for vendor-wide staff. The distinction is load-bearing:
	// a chain delegates by branch, and a manager scoped to one facility must not
	// see another's revenue.
	BranchID uuid.NullUUID
}

// Memberships lists the vendors a user works for.
func (s *Service) Memberships(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT vendor_id, role, branch_id FROM vendor_users WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("load memberships: %w", err)
	}
	defer rows.Close()

	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.VendorID, &m.Role, &m.BranchID); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AuthorizeBranch checks that this user may act on this branch.
//
// `vu.branch_id IS NULL OR vu.branch_id = b.id` is the rule that a
// vendor-wide staff member covers every branch. It is repeated in the comment
// moderation path in Phase 4, and it was wrong there first — a vendor-wide
// manager could not delete a comment on their own reel because the join
// demanded an exact branch match.
func (s *Service) AuthorizeBranch(ctx context.Context, userID, branchID uuid.UUID) (uuid.UUID, error) {
	var vendorID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT b.vendor_id
		  FROM vendor_branches b
		  JOIN vendor_users vu ON vu.vendor_id = b.vendor_id
		 WHERE b.id = $1 AND vu.user_id = $2
		   AND (vu.branch_id IS NULL OR vu.branch_id = b.id)
		   AND b.deleted_at IS NULL`,
		branchID, userID).Scan(&vendorID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not found rather than forbidden: a 403 confirms the branch exists.
		return uuid.Nil, httpx.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("authorize branch: %w", err)
	}
	return vendorID, nil
}

func (s *Service) AuthorizeVendor(ctx context.Context, userID, vendorID uuid.UUID) error {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT true FROM vendor_users WHERE user_id = $1 AND vendor_id = $2`,
		userID, vendorID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("authorize vendor: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// The order queue
// ─────────────────────────────────────────────────────────────────────────────

// QueueItem is one line on the kitchen screen.
type QueueItem struct {
	FulfillmentID  uuid.UUID
	OrderID        uuid.NullUUID
	SubscriptionID uuid.NullUUID
	OrderNo        string
	ServiceType    string
	Kind           string
	Status         string
	WindowLabel    string
	WindowStart    time.Time
	RecipientName  string
	AddressArea    string
	// PayloadRef is what the fulfillment carries for its vertical to resolve —
	// a meal slot, a laundry bag count. Passed through as text rather than
	// interpreted, so the console gains a new vertical's work for free even
	// though it cannot pretty-print it.
	PayloadRef   string
	ItemSummary  string
	DriverName   string
	ETA          *time.Time
	AttemptCount int
}

// Queue is the vendor's day, by window.
//
// One query, and the LEFT JOIN to `order_lines` is where the console
// deliberately crosses the line dispatch does not: a kitchen needs the item
// names. It is a LEFT JOIN and a `string_agg` rather than a required join,
// because a subscription-generated fulfillment has no order lines at all and a
// dashboard that showed nothing for those would hide half a meal vendor's day.
func (s *Service) Queue(ctx context.Context, branchID uuid.UUID, date time.Time) ([]QueueItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.order_id, f.subscription_id,
		       COALESCE(o.order_no, ''), COALESCE(o.service_type, ''),
		       f.kind::text, f.status::text,
		       w.label, w.starts_at,
		       COALESCE(f.recipient_name, ''), COALESCE(f.address_area, ''),
		       COALESCE(f.payload_ref, ''),
		       COALESCE((SELECT string_agg(ol.name_snapshot || ' ×' || ol.quantity, ', ')
		                   FROM order_lines ol WHERE ol.order_id = f.order_id), ''),
		       COALESCE(d.display_name, ''), rs.eta, f.attempt_count
		  FROM fulfillments f
		  JOIN time_windows w ON w.id = f.window_id
		  LEFT JOIN orders o ON o.id = f.order_id
		  LEFT JOIN drivers d ON d.id = f.driver_id
		  LEFT JOIN route_stops rs ON rs.fulfillment_id = f.id
		 WHERE f.branch_id = $1 AND f.scheduled_date = $2
		   AND f.status <> 'cancelled'
		 ORDER BY w.starts_at, f.sequence, f.id`,
		branchID, date)
	if err != nil {
		return nil, fmt.Errorf("load order queue: %w", err)
	}
	defer rows.Close()

	var out []QueueItem
	for rows.Next() {
		var q QueueItem
		if err := rows.Scan(&q.FulfillmentID, &q.OrderID, &q.SubscriptionID,
			&q.OrderNo, &q.ServiceType, &q.Kind, &q.Status,
			&q.WindowLabel, &q.WindowStart, &q.RecipientName, &q.AddressArea,
			&q.PayloadRef, &q.ItemSummary, &q.DriverName, &q.ETA, &q.AttemptCount); err != nil {
			return nil, fmt.Errorf("scan queue item: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// The capacity calendar
// ─────────────────────────────────────────────────────────────────────────────

// CapacityCell is one bucket: a date, a window, a resource.
type CapacityCell struct {
	Date        time.Time
	WindowID    uuid.UUID
	WindowLabel string
	ResourceKey string
	Capacity    int
	Reserved    int
	Consumed    int
}

// Remaining is what the customer-facing availability query would return.
func (c CapacityCell) Remaining() int {
	left := c.Capacity - c.Reserved - c.Consumed
	if left < 0 {
		return 0
	}
	return left
}

// Calendar is the screen where a vendor discovers they are fully booked on
// Thursday and can do something about it.
//
// PLAN.md §2.3 calls capacity the system's concurrency hotspot. This is the read
// side of that, and it is deliberately a plain aggregate rather than anything
// clever: the write path holds the correctness guarantee in a CHECK constraint,
// and a read that tried to be authoritative would just be a second, slower
// version of the same rule.
func (s *Service) Calendar(ctx context.Context, branchID uuid.UUID, from, to time.Time) ([]CapacityCell, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cb.bucket_date, cb.window_id, w.label, cb.resource_key,
		       cb.capacity_limit, cb.reserved, cb.consumed
		  FROM capacity_buckets cb
		  JOIN time_windows w ON w.id = cb.window_id
		 WHERE cb.branch_id = $1
		   AND cb.bucket_date BETWEEN $2 AND $3
		 ORDER BY cb.bucket_date, w.starts_at, cb.resource_key`,
		branchID, from, to)
	if err != nil {
		return nil, fmt.Errorf("load capacity calendar: %w", err)
	}
	defer rows.Close()

	var out []CapacityCell
	for rows.Next() {
		var c CapacityCell
		if err := rows.Scan(&c.Date, &c.WindowID, &c.WindowLabel, &c.ResourceKey,
			&c.Capacity, &c.Reserved, &c.Consumed); err != nil {
			return nil, fmt.Errorf("scan capacity cell: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// The subscription roster
// ─────────────────────────────────────────────────────────────────────────────

// RosterEntry is one subscriber's meal for a day.
type RosterEntry struct {
	SubscriptionID uuid.UUID
	SubscriptionNo string
	CustomerName   string
	Slot           string
	WindowLabel    string
	AddressArea    string
	FulfillmentID  uuid.NullUUID
	Status         string
	// SelectionName is what they chose, when the plan allows a choice. Empty
	// means the cycle's default for that day, which is the common case and is
	// rendered by the client rather than looked up per row.
	SelectionName string
}

// Roster is the kitchen's prep list.
//
// The single most-used screen in a meal business and the reason Phase 5's
// scheduler materialises fulfillments 14 days ahead: a kitchen that cannot see
// Thursday until Thursday cannot buy for Thursday.
func (s *Service) Roster(ctx context.Context, branchID uuid.UUID, date time.Time) ([]RosterEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sub.id, sub.subscription_no, COALESCE(u.display_name, u.phone),
		       COALESCE(sms.meal_slot, split_part(COALESCE(f.payload_ref, ''), ':', 1)),
		       w.label, COALESCE(f.address_area, ''),
		       f.id, f.status::text,
		       COALESCE(sms.item_name_snapshot, mi.name, '')
		  FROM fulfillments f
		  JOIN subscriptions sub ON sub.id = f.subscription_id
		  JOIN users u ON u.id = sub.user_id
		  JOIN time_windows w ON w.id = f.window_id
		  LEFT JOIN subscription_meal_selections sms
		         ON sms.subscription_id = sub.id
		        AND sms.service_date = f.scheduled_date
		        AND sms.meal_slot = split_part(COALESCE(f.payload_ref, ''), ':', 1)
		  LEFT JOIN menu_items mi ON mi.id = sms.menu_item_id
		 WHERE f.branch_id = $1
		   AND f.scheduled_date = $2
		   AND f.subscription_id IS NOT NULL
		   AND f.status <> 'cancelled'
		 ORDER BY w.starts_at, sub.subscription_no`,
		branchID, date)
	if err != nil {
		return nil, fmt.Errorf("load roster: %w", err)
	}
	defer rows.Close()

	var out []RosterEntry
	for rows.Next() {
		var r RosterEntry
		if err := rows.Scan(&r.SubscriptionID, &r.SubscriptionNo, &r.CustomerName,
			&r.Slot, &r.WindowLabel, &r.AddressArea,
			&r.FulfillmentID, &r.Status, &r.SelectionName); err != nil {
			return nil, fmt.Errorf("scan roster entry: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Summary
// ─────────────────────────────────────────────────────────────────────────────

// Summary is the top of the dashboard.
type Summary struct {
	Date          time.Time
	Scheduled     int
	InProgress    int
	Completed     int
	Failed        int
	Unassigned    int
	CashHeldMinor int64
	PayableMinor  int64
	Currency      string
}

// Today is one query per number rather than one screen-wide join.
//
// Deliberate: these are five independent counts over the same index and the
// planner handles each one cleanly, whereas the single query that produces all
// of them needs a FULL OUTER JOIN and produces a plan nobody can read six months
// later. The dashboard polls this every thirty seconds and it is still cheap.
func (s *Service) Today(ctx context.Context, branchID uuid.UUID, date time.Time) (*Summary, error) {
	sum := &Summary{Date: date, Currency: "AED"}

	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'scheduled'),
		       count(*) FILTER (WHERE status IN ('dispatched', 'en_route', 'arrived')),
		       count(*) FILTER (WHERE status = 'completed'),
		       count(*) FILTER (WHERE status = 'failed'),
		       count(*) FILTER (WHERE status = 'scheduled' AND route_id IS NULL)
		  FROM fulfillments
		 WHERE branch_id = $1 AND scheduled_date = $2`,
		branchID, date,
	).Scan(&sum.Scheduled, &sum.InProgress, &sum.Completed, &sum.Failed, &sum.Unassigned); err != nil {
		return nil, fmt.Errorf("load summary counts: %w", err)
	}

	// Cash the vendor's drivers are carrying against today's work. A vendor
	// running their own fleet needs this number at the end of the day and has no
	// other way to get it.
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(sum(c.collected_minor), 0)
		  FROM cod_collections c
		  JOIN fulfillments f ON f.id = c.fulfillment_id
		 WHERE f.branch_id = $1 AND f.scheduled_date = $2 AND c.state = 'held'`,
		branchID, date,
	).Scan(&sum.CashHeldMinor); err != nil {
		return nil, fmt.Errorf("load held cash: %w", err)
	}

	// The vendor's current payable balance, straight from the ledger. Not from
	// a statement: statements close weekly and a vendor asking "what am I owed"
	// on a Wednesday wants the live number.
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(sum(CASE WHEN e.direction = 'credit' THEN e.amount_minor
		                         ELSE -e.amount_minor END), 0)
		  FROM ledger_entries e
		  JOIN ledger_accounts a ON a.id = e.account_id
		  JOIN vendor_branches b ON b.vendor_id = a.owner_id
		 WHERE b.id = $1 AND a.type = 'vendor_payable'`,
		branchID,
	).Scan(&sum.PayableMinor); err != nil {
		return nil, fmt.Errorf("load payable balance: %w", err)
	}

	return sum, nil
}

// BranchRole is AuthorizeBranch plus the caller's role, for the routes that
// distinguish reading a branch from changing it.
func (s *Service) BranchRole(ctx context.Context, userID, branchID uuid.UUID) (uuid.UUID, string, error) {
	var vendorID uuid.UUID
	var role string
	// The strongest membership wins when someone holds both a vendor-wide and a
	// branch-scoped row.
	err := s.pool.QueryRow(ctx, `
		SELECT b.vendor_id, vu.role
		  FROM vendor_branches b
		  JOIN vendor_users vu ON vu.vendor_id = b.vendor_id
		 WHERE b.id = $1 AND vu.user_id = $2
		   AND (vu.branch_id IS NULL OR vu.branch_id = b.id)
		   AND b.deleted_at IS NULL
		 ORDER BY CASE vu.role WHEN 'owner' THEN 0 WHEN 'manager' THEN 1 ELSE 2 END
		 LIMIT 1`,
		branchID, userID).Scan(&vendorID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", httpx.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("authorize branch role: %w", err)
	}
	return vendorID, role, nil
}

// BranchAccess is one branch the caller can open in the vendor portal.
type BranchAccess struct {
	BranchID     uuid.UUID `json:"branch_id"`
	BranchName   string    `json:"branch_name"`
	City         *string   `json:"city"`
	VendorID     uuid.UUID `json:"vendor_id"`
	VendorName   string    `json:"vendor_name"`
	VendorStatus string    `json:"vendor_status"`
	Role         string    `json:"role"`
}

// Branches expands memberships into concrete branches: a vendor-wide row
// grants every branch of that vendor, which is what the portal's switcher
// needs and what Memberships deliberately does not do.
func (s *Service) Branches(ctx context.Context, userID uuid.UUID) ([]BranchAccess, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (b.id) b.id, b.name, b.city, v.id, v.trade_name, v.status, vu.role
		  FROM vendor_users vu
		  JOIN vendors v ON v.id = vu.vendor_id AND v.deleted_at IS NULL
		  JOIN vendor_branches b ON b.vendor_id = v.id AND b.deleted_at IS NULL
		       AND (vu.branch_id IS NULL OR vu.branch_id = b.id)
		 WHERE vu.user_id = $1
		 ORDER BY b.id, CASE vu.role WHEN 'owner' THEN 0 WHEN 'manager' THEN 1 ELSE 2 END`, userID)
	if err != nil {
		return nil, fmt.Errorf("load branches: %w", err)
	}
	defer rows.Close()
	out := []BranchAccess{}
	for rows.Next() {
		var b BranchAccess
		if err := rows.Scan(&b.BranchID, &b.BranchName, &b.City, &b.VendorID, &b.VendorName,
			&b.VendorStatus, &b.Role); err != nil {
			return nil, fmt.Errorf("scan branch: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
