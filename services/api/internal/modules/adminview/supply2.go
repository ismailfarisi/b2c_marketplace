package adminview

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Listing and driver detail — the two entity pages Phase C left unbuilt.
//
// Both follow the shape the order page set: one query set, one composed
// document, redaction at this boundary. What differs is which question each page
// is opened with, and the layout follows from that rather than from the tables.
//
// A **listing** is opened with "should this stay up", so fraud signals and the
// seller's history come before the specification of the car.
//
// A **driver** is opened with "where is my delivery" or "why is this person
// holding four hundred dirhams", so position and cash come before documents.

// ── listing detail ──────────────────────────────────────────────────────────

// ListingDetail is one vehicle listing and the case for or against it.
type ListingDetail struct {
	ID          uuid.UUID
	ReferenceNo string
	Status      string
	SellerID    uuid.UUID
	SellerKind  string
	SellerName  string
	SellerPhone string
	DealerID    *uuid.UUID
	Dealer      string

	Make     string
	Model    string
	Year     int
	Mileage  int
	VIN      string
	Spec     string
	BodyType string
	Fuel     string
	Colour   string

	PriceMinor int64
	Currency   string
	Emirate    string
	City       string

	RegistrationExpiry *time.Time
	WarrantyUntil      *time.Time

	PublishedAt   *time.Time
	ExpiresAt     *time.Time
	FeaturedUntil *time.Time
	BumpedAt      *time.Time
	SoldAt        *time.Time
	SoldPrice     *int64
	RemovedAt     *time.Time
	RejectionNote string
	CreatedAt     time.Time

	Photos int
	// SellerListings is how many other listings this seller has. A private
	// seller with eleven cars is a dealer who has not said so, and that is the
	// single most useful number on the page — it is invisible from the listing
	// alone.
	SellerListings int
	SellerSold     int
	Signals        []FraudSignal
	Entitlement    *Entitlement
}

// FraudSignal is one recorded concern about a listing or seller.
type FraudSignal struct {
	Kind string
	// Severity is 0–100, not a word. Kept numeric because the thresholds that
	// decide whether a signal blocks publication are numeric, and bucketing it
	// into low/medium/high here would put the boundary in two places.
	Severity int
	Detail   string
	State    string
	At       time.Time
}

// Entitlement is the package quota the seller is publishing against.
//
// Attached to the *buyer*, not the listing — a purchase grants a quota of
// listings rather than paying for one. That is worth stating on the page,
// because "why did this listing expire early" is usually answered by the
// purchase window ending rather than by anything about the listing, and nobody
// reading the listing row would find it.
type Entitlement struct {
	PackageCode  string
	PackageName  string
	PurchasedAt  time.Time
	StartsAt     *time.Time
	EndsAt       *time.Time
	ListingQuota int
	ListingsUsed int
	BumpQuota    int
	BumpsUsed    int
	Status       string
}

// LoadListing composes the listing, its seller history and its signals.
func (s *Service) LoadListing(ctx context.Context, id uuid.UUID, lens Lens) (*ListingDetail, error) {
	var l ListingDetail
	var vin, spec, body, fuel, colour, emirate, city, rejection, dealer *string
	var sellerName, sellerPhone *string

	err := s.pool.QueryRow(ctx, `
		SELECT l.id, l.reference_no, l.status, l.seller_id, l.seller_kind,
		       u.display_name, u.phone, l.dealer_id, d.trade_name,
		       mk.name, md.name, l.year, l.mileage_km, l.vin, l.regional_spec,
		       l.body_type, l.fuel_type, l.exterior_colour,
		       l.price_minor, l.currency, l.emirate, l.city,
		       l.registration_expiry, l.warranty_until,
		       l.published_at, l.expires_at, l.featured_until, l.bumped_at,
		       l.sold_at, l.sold_price_minor, l.removed_at, l.rejection_reason,
		       l.created_at,
		       (SELECT count(*) FROM listing_photos m WHERE m.listing_id = l.id),
		       (SELECT count(*) FROM vehicle_listings o
		         WHERE o.seller_id = l.seller_id AND o.id <> l.id),
		       (SELECT count(*) FROM vehicle_listings o
		         WHERE o.seller_id = l.seller_id AND o.sold_at IS NOT NULL)
		  FROM vehicle_listings l
		  LEFT JOIN users u ON u.id = l.seller_id
		  LEFT JOIN vendors d ON d.id = l.dealer_id
		  LEFT JOIN vehicle_makes mk ON mk.id = l.make_id
		  LEFT JOIN vehicle_models md ON md.id = l.model_id
		 WHERE l.id = $1`, id).
		Scan(&l.ID, &l.ReferenceNo, &l.Status, &l.SellerID, &l.SellerKind,
			&sellerName, &sellerPhone, &l.DealerID, &dealer,
			&l.Make, &l.Model, &l.Year, &l.Mileage, &vin, &spec,
			&body, &fuel, &colour,
			&l.PriceMinor, &l.Currency, &emirate, &city,
			&l.RegistrationExpiry, &l.WarrantyUntil,
			&l.PublishedAt, &l.ExpiresAt, &l.FeaturedUntil, &l.BumpedAt,
			&l.SoldAt, &l.SoldPrice, &l.RemovedAt, &rejection,
			&l.CreatedAt, &l.Photos, &l.SellerListings, &l.SellerSold)
	if err != nil {
		return nil, notFoundOr("load listing", err)
	}

	l.SellerName = lens.contact(deref(sellerName))
	l.SellerPhone = lens.contact(deref(sellerPhone))
	l.Dealer = deref(dealer)
	// The VIN is not contact information but it is identifying, and it is the
	// field a scraped listing is rebuilt from. Behind the same lens as the
	// phone number for that reason.
	l.VIN = lens.contact(deref(vin))
	l.Spec = deref(spec)
	l.BodyType = deref(body)
	l.Fuel = deref(fuel)
	l.Colour = deref(colour)
	l.Emirate = deref(emirate)
	l.City = deref(city)
	l.RejectionNote = deref(rejection)

	rows, err := s.pool.Query(ctx, `
		SELECT kind, severity, coalesce(detail::text, ''), state, created_at
		  FROM fraud_signals
		 WHERE (subject_type = 'listing' AND subject_id = $1)
		    OR (subject_type = 'user' AND subject_id = $2)
		 ORDER BY created_at DESC
		 LIMIT 25`, id, l.SellerID)
	if err != nil {
		return nil, wrap("fraud signals", err)
	}
	for rows.Next() {
		var f FraudSignal
		if err := rows.Scan(&f.Kind, &f.Severity, &f.Detail, &f.State, &f.At); err != nil {
			rows.Close()
			return nil, err
		}
		l.Signals = append(l.Signals, f)
	}
	rows.Close()

	// The seller's most recent live purchase. Best-effort: a listing published
	// under a purchase that has since been consumed and archived should still
	// render, and a missing entitlement is itself informative — it means this
	// listing is up without a package behind it.
	var e Entitlement
	err = s.pool.QueryRow(ctx, `
		SELECT p.code, p.name, pp.created_at, pp.starts_at, pp.ends_at,
		       pp.listing_quota, pp.listings_used, pp.bump_quota, pp.bumps_used, pp.status
		  FROM package_purchases pp
		  JOIN listing_packages p ON p.id = pp.package_id
		 WHERE pp.buyer_id = $1
		 ORDER BY pp.created_at DESC LIMIT 1`, l.SellerID).
		Scan(&e.PackageCode, &e.PackageName, &e.PurchasedAt, &e.StartsAt, &e.EndsAt,
			&e.ListingQuota, &e.ListingsUsed, &e.BumpQuota, &e.BumpsUsed, &e.Status)
	if err == nil {
		l.Entitlement = &e
	}

	return &l, nil
}

// ListingTimeline merges status history with staff actions.
func (s *Service) ListingTimeline(ctx context.Context, id uuid.UUID) ([]TimelineEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT h.created_at, 'listing', h.to_status,
		       coalesce(h.reason, coalesce(h.from_status, 'created') || ' → ' || h.to_status),
		       coalesce(u.display_name, u.phone, h.actor_type, '')
		  FROM listing_status_history h
		  LEFT JOIN users u ON u.id = h.actor_id
		 WHERE h.listing_id = $1
		UNION ALL
		SELECT a.created_at, 'staff', a.action, coalesce(a.reason, ''),
		       coalesce(su.display_name, su.phone, '')
		  FROM admin_actions a
		  LEFT JOIN users su ON su.id = a.actor_id
		 WHERE a.subject_type = 'listing' AND a.subject_id = $1
		 ORDER BY 1 DESC LIMIT 100`, id)
	if err != nil {
		return nil, wrap("listing timeline", err)
	}
	defer rows.Close()

	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var actor string
		if err := rows.Scan(&e.At, &e.Source, &e.Kind, &e.Detail, &actor); err != nil {
			return nil, err
		}
		e.Actor = actor
		out = append(out, e)
	}
	return out, rows.Err()
}

// ── driver detail ───────────────────────────────────────────────────────────

// DriverDetail is one driver: where they are, what they are carrying, and
// whether they are allowed to be working.
type DriverDetail struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	DisplayName string
	Phone       string
	Status      string
	VehicleType string
	PlateNo     string
	VendorID    *uuid.UUID
	Vendor      string
	MaxStops    int
	CreatedAt   time.Time

	// LastPingAt without a position, or a position hours old, is the difference
	// between "the app is closed" and "the app is open and not reporting". Both
	// look like a missing driver on a map and need different phone calls.
	LastPingAt *time.Time
	LastLat    *float64
	LastLng    *float64

	OnShift     bool
	ShiftFrom   *time.Time
	ShiftUntil  *time.Time
	OpenStops   int
	TodayDone   int
	TodayFailed int

	CashHeldMinor  int64
	CashHeldStops  int
	CashCurrency   string
	OldestCashHeld *time.Time

	Documents []DriverDocument
}

// DriverDocument is one licence or permit.
type DriverDocument struct {
	Kind      string
	State     string
	ExpiresOn *time.Time
	// Expired is computed in SQL rather than in Go so that "expired" means the
	// database's idea of today, which is the same one every other date
	// comparison on this page uses.
	Expired bool
}

// LoadDriver composes position, workload, cash and documents.
func (s *Service) LoadDriver(ctx context.Context, id uuid.UUID, lens Lens) (*DriverDetail, error) {
	var d DriverDetail
	var plate, vendor *string

	err := s.pool.QueryRow(ctx, `
		SELECT dr.id, dr.user_id, dr.display_name, dr.phone, dr.status,
		       dr.vehicle_type, dr.plate_no, dr.vendor_id, v.trade_name,
		       dr.max_stops, dr.created_at, dr.last_ping_at,
		       ST_Y(dr.last_point::geometry), ST_X(dr.last_point::geometry)
		  FROM drivers dr
		  LEFT JOIN vendors v ON v.id = dr.vendor_id
		 WHERE dr.id = $1`, id).
		Scan(&d.ID, &d.UserID, &d.DisplayName, &d.Phone, &d.Status,
			&d.VehicleType, &plate, &d.VendorID, &vendor,
			&d.MaxStops, &d.CreatedAt, &d.LastPingAt, &d.LastLat, &d.LastLng)
	if err != nil {
		return nil, notFoundOr("load driver", err)
	}

	d.Phone = lens.contact(d.Phone)
	d.PlateNo = deref(plate)
	d.Vendor = deref(vendor)

	// Shift, workload and cash in one round trip each — they are independent
	// and none of them should be able to fail the page.
	_ = s.pool.QueryRow(ctx, `
		SELECT true, starts_at, ends_at
		  FROM driver_shifts
		 WHERE driver_id = $1 AND starts_at <= now() AND (ends_at IS NULL OR ends_at > now())
		 ORDER BY starts_at DESC LIMIT 1`, id).
		Scan(&d.OnShift, &d.ShiftFrom, &d.ShiftUntil)

	_ = s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status IN ('dispatched', 'en_route', 'arrived')),
		       count(*) FILTER (WHERE status = 'completed' AND updated_at >= current_date),
		       count(*) FILTER (WHERE status = 'failed' AND updated_at >= current_date)
		  FROM fulfillments WHERE driver_id = $1`, id).
		Scan(&d.OpenStops, &d.TodayDone, &d.TodayFailed)

	_ = s.pool.QueryRow(ctx, `
		SELECT coalesce(sum(collected_minor), 0), count(*),
		       coalesce(max(currency), 'AED'), min(collected_at)
		  FROM cod_collections WHERE driver_id = $1 AND state = 'held'`, id).
		Scan(&d.CashHeldMinor, &d.CashHeldStops, &d.CashCurrency, &d.OldestCashHeld)

	rows, err := s.pool.Query(ctx, `
		SELECT kind, state, expires_at,
		       expires_at IS NOT NULL AND expires_at < current_date
		  FROM driver_documents WHERE driver_id = $1
		 ORDER BY kind`, id)
	if err != nil {
		return nil, wrap("driver documents", err)
	}
	defer rows.Close()
	for rows.Next() {
		var doc DriverDocument
		if err := rows.Scan(&doc.Kind, &doc.State, &doc.ExpiresOn, &doc.Expired); err != nil {
			return nil, err
		}
		d.Documents = append(d.Documents, doc)
	}
	return &d, rows.Err()
}

// DriverTimeline is the driver's own history of stops.
//
// Read from `fulfillment_events` filtered by actor rather than by fulfillment,
// which is the difference between "what happened to this delivery" and "what did
// this person do today" — the second is what a dispatcher asks when a driver
// disputes a failed stop.
func (s *Service) DriverTimeline(ctx context.Context, id uuid.UUID) ([]TimelineEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.occurred_at, 'fulfillment', e.to_status::text,
		       coalesce(o.order_no, '') ||
		         coalesce(' · ' || (e.metadata->>'outcome'), '') ||
		         coalesce(' · ' || (e.metadata->>'reason'), ''),
		       ''
		  FROM fulfillment_events e
		  JOIN fulfillments f ON f.id = e.fulfillment_id
		  LEFT JOIN orders o ON o.id = f.order_id
		 WHERE e.actor_type = 'driver' AND e.actor_id = $1
		 ORDER BY e.occurred_at DESC LIMIT 100`, id)
	if err != nil {
		return nil, wrap("driver timeline", err)
	}
	defer rows.Close()

	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var actor string
		if err := rows.Scan(&e.At, &e.Source, &e.Kind, &e.Detail, &actor); err != nil {
			return nil, err
		}
		e.Actor = actor
		out = append(out, e)
	}
	return out, rows.Err()
}
