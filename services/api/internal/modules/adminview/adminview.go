// Package adminview composes read models for the staff console.
//
// PLAN-ADMIN-CONSOLE §3.6: **one endpoint per page, not one per panel.** An
// order detail page touches twelve tables; that is one request returning a
// composed document, not twelve from a browser.
//
// Three rules define this package, and the third is the one that matters:
//
//  1. It **owns no tables.** Every row it reads belongs to another module.
//  2. It **composes**, it does not compute. Business rules live in the module
//     that owns them; if a number needs deriving, the owning module derives it
//     and this package reads the result.
//  3. It **never writes.** Not a discipline — a property. There is no `Exec` in
//     this package and no transaction, so the rule in §3.2 (every mutation goes
//     through the owning service) cannot be broken here even by accident.
//
// Keeping it in one package is what stops twelve modules each growing an
// `AdminSummary` method, which is the shape this would otherwise take.
package adminview

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Money is the shape every amount takes on the wire.
type Money struct {
	AmountMinor int64
	Currency    string
}

// Redaction — the §3.5 permission model, made concrete.
//
// A read model does not get to decide who sees what, but it does get to be
// *told*, so the decision lives at one boundary rather than in each handler's
// serializer. A caller without `commerce.contact` gets a document with the
// contact fields already empty, which means a serializer that forgets to check
// leaks nothing.
//
// The alternative — return everything and filter at the edge — is one forgotten
// field away from a customer's phone number in a moderator's browser.
type Lens struct {
	// Contact permits recipient names, phone numbers and addresses.
	Contact bool
	// Money permits amounts, payments and ledger positions.
	Money bool
	// Identity permits KYC documents and bank details.
	Identity bool
}

func (l Lens) contact(s string) string {
	if l.Contact {
		return s
	}
	return ""
}

// TimelineEntry is one thing that happened, from whichever table recorded it.
//
// The merged timeline is the part of a detail page that gets skipped and the
// part that answers the phone call. Eight tables already record history —
// `fulfillment_events`, `reel_state_history`, `listing_status_history`,
// `subscription_events`, `dispute_events`, `admin_actions`, `ledger_journals`
// and `notifications` — and until now nothing read any of them back.
type TimelineEntry struct {
	At     time.Time
	Source string
	Kind   string
	Detail string
	// Actor is who did it, where that is known. An empty actor means the system
	// did it, which is itself worth seeing: "who cancelled this" and "it expired"
	// look identical in a status column and are different events.
	Actor string
}

// SearchHit is one result from the global lookup.
type SearchHit struct {
	Kind  string
	ID    uuid.UUID
	Label string
	Sub   string
}

// Search resolves whatever a support agent typed.
//
// One endpoint over eight identifier shapes, because the person typing has a
// customer on the phone and does not know whether `AE-G3CRYK` is an order
// number or a reference — they know it is what the customer read out.
//
// Ordered by how specific the match is, not by table. An exact reference number
// is what was meant; a phone-number prefix might be one of several.
func (s *Service) Search(ctx context.Context, q string, lens Lens) ([]SearchHit, error) {
	q = normalise(q)
	if len(q) < 3 {
		return nil, nil
	}

	var out []SearchHit

	// A uuid is unambiguous — resolve it against every table that could own it
	// rather than making the agent say which kind of thing it is.
	if id, err := uuid.Parse(q); err == nil {
		hits, err := s.searchByID(ctx, id)
		if err != nil {
			return nil, err
		}
		return hits, nil
	}

	// Each branch is its own CTE rather than a chain of UNION ALL with per-branch
	// LIMITs. Postgres applies a bare LIMIT to the whole union, so the first
	// version was a syntax error — and the CTE form is what the query wanted to
	// be anyway: eight independent lookups, each bounded, merged at the end.
	rows, err := s.pool.Query(ctx, `
		WITH by_order AS (
		    SELECT 'order' AS kind, o.id, o.order_no AS label,
		           coalesce(u.display_name, u.phone, '') AS sub, 1 AS rank
		      FROM orders o LEFT JOIN users u ON u.id = o.user_id
		     WHERE upper(o.order_no) = upper($1)
		), by_listing AS (
		    SELECT 'listing', l.id, l.reference_no, coalesce(l.title, ''), 1
		      FROM vehicle_listings l
		     WHERE upper(l.reference_no) = upper($1)
		        OR upper(coalesce(l.vin, '')) = upper($1)
		), by_dispute AS (
		    SELECT 'dispute', d.id, d.reference_no, d.kind, 1
		      FROM disputes d WHERE upper(d.reference_no) = upper($1)
		), by_campaign AS (
		    SELECT 'campaign', c.id, c.reference_no, c.status, 1
		      FROM boost_campaigns c WHERE upper(c.reference_no) = upper($1)
		), by_statement AS (
		    SELECT 'statement', p.id, p.reference_no, p.state, 1
		      FROM payout_statements p WHERE upper(p.reference_no) = upper($1)
		), by_invoice AS (
		    SELECT 'invoice', i.id, i.number, i.status, 1
		      FROM invoices i WHERE upper(i.number) = upper($1)
		), by_phone AS (
		    -- Suffix match rather than exact: an agent types the digits the
		    -- customer says, and the country code is usually omitted.
		    SELECT 'user', u.id, coalesce(u.display_name, u.phone), u.phone, 2
		      FROM users u
		     WHERE u.deleted_at IS NULL AND u.phone LIKE '%' || $1
		     LIMIT 10
		), by_vendor AS (
		    SELECT 'vendor', v.id, v.trade_name, v.status, 3
		      FROM vendors v
		     WHERE v.deleted_at IS NULL
		       AND (v.trade_name ILIKE '%' || $1 || '%'
		         OR v.legal_name ILIKE '%' || $1 || '%')
		     LIMIT 10
		), by_iban AS (
		    -- The last four of an IBAN, which is how a vendor identifies their
		    -- account on the phone rather than reading out twenty-three
		    -- characters.
		    SELECT 'vendor', a.owner_id, coalesce(v2.trade_name, 'vendor'),
		           'account ending ' || right(a.iban, 4), 2
		      FROM payout_accounts a
		      LEFT JOIN vendors v2 ON v2.id = a.owner_id
		     WHERE a.owner_type = 'vendor' AND right(a.iban, 4) = $1
		     LIMIT 5
		)
		SELECT * FROM by_order
		UNION ALL SELECT * FROM by_listing
		UNION ALL SELECT * FROM by_dispute
		UNION ALL SELECT * FROM by_campaign
		UNION ALL SELECT * FROM by_statement
		UNION ALL SELECT * FROM by_invoice
		UNION ALL SELECT * FROM by_phone
		UNION ALL SELECT * FROM by_vendor
		UNION ALL SELECT * FROM by_iban
		ORDER BY 5, 3
		LIMIT 25`, q)
	if err != nil {
		return nil, wrap("search", err)
	}
	defer rows.Close()

	for rows.Next() {
		var h SearchHit
		var rank int
		if err := rows.Scan(&h.Kind, &h.ID, &h.Label, &h.Sub, &rank); err != nil {
			return nil, err
		}
		if h.Kind == "user" && !lens.Contact {
			// A moderator searching by phone number would otherwise confirm that
			// a number belongs to a customer, which is the disclosure the
			// permission exists to prevent.
			continue
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Service) searchByID(ctx context.Context, id uuid.UUID) ([]SearchHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT 'order', order_no, status FROM orders WHERE id = $1
		UNION ALL SELECT 'user', coalesce(display_name, phone), status::text FROM users WHERE id = $1
		UNION ALL SELECT 'vendor', trade_name, status FROM vendors WHERE id = $1
		UNION ALL SELECT 'reel', coalesce(caption, '(no caption)'), state::text FROM reels WHERE id = $1
		UNION ALL SELECT 'listing', coalesce(title, reference_no), status FROM vehicle_listings WHERE id = $1
		UNION ALL SELECT 'driver', display_name, status FROM drivers WHERE id = $1
		UNION ALL SELECT 'campaign', reference_no, status FROM boost_campaigns WHERE id = $1
		UNION ALL SELECT 'dispute', reference_no, state FROM disputes WHERE id = $1
		UNION ALL SELECT 'branch', name, '' FROM vendor_branches WHERE id = $1`, id)
	if err != nil {
		return nil, wrap("search by id", err)
	}
	defer rows.Close()

	var out []SearchHit
	for rows.Next() {
		h := SearchHit{ID: id}
		if err := rows.Scan(&h.Kind, &h.Label, &h.Sub); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// notFoundOr turns a missing row into a 404 and everything else into a 500.
//
// The distinction matters more here than in most packages: a console operator
// following a link from a timeline to a deleted record should be told the record
// is gone, not shown an internal error that reads like the console is broken.
func notFoundOr(what string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	return wrap(what, err)
}
