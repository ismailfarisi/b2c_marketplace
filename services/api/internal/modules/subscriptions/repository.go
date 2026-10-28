package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/marketplace/api/internal/httpx"
)

type Repository struct{}

// PlanTemplate is the shape a subscription is bought from.
//
// Named for what DATA-MODEL.md §3 calls it — "Template: duration, days/week,
// meals/day" — rather than `Plan`, which is taken by the scheduler verb. The
// collision is worth avoiding rather than resolving by import alias: one of
// them is a noun the customer buys and the other is what the nightly job does.
type PlanTemplate struct {
	ID               uuid.UUID
	BranchID         uuid.UUID
	VendorID         uuid.UUID
	ServiceType      string
	MenuCycleID      uuid.NullUUID
	Name             string
	CycleDays        int
	DaysPerWeek      int
	MealsPerDay      int
	AllowedPauseDays int
	CutoffHours      int
	IsPrepaid        bool
	PriceMinor       int64
	Currency         string
	TaxRateBP        int
	Timezone         string
	Country          string
	Weekdays         []int16
	MealSlots        []string
	RotationEnabled  bool
	// The kitchen's price for one week of this plan inside a rotation.
	RotationWeekPriceMinor *int64
}

// LoadPlan reads a plan and the branch context it belongs to.
//
// The tier's price wins when one is chosen. Absolute, not a delta on the plan
// price: two numbers that can drift apart mean the one that reaches the
// customer's card is whichever the code happened to add.
func (r *Repository) LoadPlan(ctx context.Context, tx pgx.Tx, planID uuid.UUID, tierID uuid.NullUUID) (PlanTemplate, error) {
	var p PlanTemplate
	var tierPrice *int64
	var tierDays, tierMeals *int16

	err := tx.QueryRow(ctx, `
		SELECT p.id, p.branch_id, b.vendor_id, p.service_type, p.menu_cycle_id, p.name,
		       p.cycle_days, p.days_per_week, p.meals_per_day,
		       p.allowed_pause_days, p.cutoff_hours, p.is_prepaid,
		       p.price_minor, p.currency, p.tax_rate_bp,
		       b.timezone, b.country,
		       t.price_minor, t.days_per_week, t.meals_per_day,
		       p.delivery_weekdays, p.meal_slots,
		       p.rotation_enabled, p.rotation_week_price_minor
		  FROM subscription_plans p
		  JOIN vendor_branches b ON b.id = p.branch_id
		  LEFT JOIN plan_tiers t ON t.id = $2 AND t.plan_id = p.id AND t.is_active
		 WHERE p.id = $1 AND p.is_active
		   AND b.is_active AND b.deleted_at IS NULL`,
		planID, tierID,
	).Scan(&p.ID, &p.BranchID, &p.VendorID, &p.ServiceType, &p.MenuCycleID, &p.Name,
		&p.CycleDays, &p.DaysPerWeek, &p.MealsPerDay,
		&p.AllowedPauseDays, &p.CutoffHours, &p.IsPrepaid,
		&p.PriceMinor, &p.Currency, &p.TaxRateBP,
		&p.Timezone, &p.Country,
		&tierPrice, &tierDays, &tierMeals,
		&p.Weekdays, &p.MealSlots,
		&p.RotationEnabled, &p.RotationWeekPriceMinor)

	if errors.Is(err, pgx.ErrNoRows) {
		return PlanTemplate{}, httpx.ErrNotFound
	}
	if err != nil {
		return PlanTemplate{}, fmt.Errorf("subscriptions: load plan: %w", err)
	}

	if tierPrice != nil {
		p.PriceMinor = *tierPrice
	}
	if tierDays != nil {
		p.DaysPerWeek = int(*tierDays)
	}
	if tierMeals != nil {
		p.MealsPerDay = int(*tierMeals)
	}
	return p, nil
}

type insertParams struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Plan        PlanTemplate
	TierID      uuid.NullUUID
	AddressID   uuid.UUID
	Start       time.Time
	End         time.Time
	RenewalMode string
	// Set for one week of a four-kitchen rotation.
	RotationID   uuid.NullUUID
	RotationWeek int
}

func (r *Repository) InsertSubscription(ctx context.Context, tx pgx.Tx, p insertParams) error {
	var end any
	if !p.End.IsZero() {
		end = p.End
	}
	var tierID any
	if p.TierID.Valid {
		tierID = p.TierID.UUID
	}
	var rotationID, rotationWeek any
	if p.RotationID.Valid {
		rotationID, rotationWeek = p.RotationID.UUID, p.RotationWeek
	}

	// Retried like order numbers, and for the same reason: the generator is
	// random, collisions are rare and inevitable, and a customer failing to
	// subscribe because two people signed up in the same second is not an
	// acceptable failure mode.
	for attempt := 0; attempt < 3; attempt++ {
		_, err := tx.Exec(ctx, `
			INSERT INTO subscriptions (
				id, subscription_no, user_id, branch_id, vendor_id,
				plan_id, tier_id, service_type, status,
				start_date, end_date, renewal_mode, timezone, address_id,
				rotation_id, rotation_week
			) VALUES (
				$1, generate_subscription_no(), $2, $3, $4,
				$5, $6, $7, 'pending_payment',
				$8, $9, $10, $11, $12, $13, $14
			)`,
			p.ID, p.UserID, p.Plan.BranchID, p.Plan.VendorID,
			p.Plan.ID, tierID, p.Plan.ServiceType,
			p.Start, end, p.RenewalMode, p.Plan.Timezone, p.AddressID,
			rotationID, rotationWeek)
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err, "subscriptions_no_key") {
			return fmt.Errorf("subscriptions: insert: %w", err)
		}
	}
	return fmt.Errorf("subscriptions: could not allocate a unique number in 3 attempts")
}

func (r *Repository) InsertRules(ctx context.Context, tx pgx.Tx, subID uuid.UUID, rules []Rule) error {
	for _, rule := range rules {
		var addr any
		if rule.AddressID.Valid {
			addr = rule.AddressID.UUID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO subscription_schedule_rules
			    (subscription_id, weekday, meal_slot, window_id, address_id)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (subscription_id, weekday, meal_slot) DO UPDATE
			   SET window_id = EXCLUDED.window_id, address_id = EXCLUDED.address_id`,
			subID, int(rule.Weekday), rule.Slot, rule.WindowID, addr,
		); err != nil {
			return fmt.Errorf("subscriptions: insert rule: %w", err)
		}
	}
	return nil
}

func (r *Repository) LoadRules(ctx context.Context, tx pgx.Tx, subID uuid.UUID) ([]Rule, error) {
	rows, err := tx.Query(ctx, `
		SELECT weekday, meal_slot, window_id, address_id
		  FROM subscription_schedule_rules
		 WHERE subscription_id = $1
		 ORDER BY weekday, meal_slot`, subID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: load rules: %w", err)
	}
	defer rows.Close()

	var out []Rule
	for rows.Next() {
		var rule Rule
		var weekday int16
		if err := rows.Scan(&weekday, &rule.Slot, &rule.WindowID, &rule.AddressID); err != nil {
			return nil, fmt.Errorf("subscriptions: scan rule: %w", err)
		}
		rule.Weekday = Weekday(weekday)
		out = append(out, rule)
	}
	return out, rows.Err()
}

// Live is the working set of one subscription: enough to plan its next window.
type Live struct {
	Subscription
	SubscriptionNo string
	UserID         uuid.UUID
	BranchID       uuid.UUID
	VendorID       uuid.UUID
	PlanID         uuid.UUID
	Status         string
	CutoffHours    int
	Currency       string
	Materialised   *time.Time
	RotationID     uuid.NullUUID
	Rules          []Rule
	Pauses         []Pause
	Skips          []Skip
}

// LoadLive reads everything the scheduler needs, in three queries.
//
// Three and not one: rules, pauses and skips are independent one-to-many sets,
// and joining them together multiplies the rows by each other. A subscription
// with 10 rules, 3 pauses and 20 skips would return 600 rows to carry 33 facts.
func (r *Repository) LoadLive(ctx context.Context, tx pgx.Tx, subID uuid.UUID) (*Live, error) {
	var l Live
	var tz string
	var start, end *time.Time

	err := tx.QueryRow(ctx, `
		SELECT s.id, s.subscription_no, s.user_id, s.branch_id, s.vendor_id,
		       s.plan_id, s.status, s.start_date, s.end_date, s.timezone,
		       s.address_id, s.materialised_through,
		       p.cutoff_hours, p.currency, s.rotation_id
		  FROM subscriptions s
		  JOIN subscription_plans p ON p.id = s.plan_id
		 WHERE s.id = $1`, subID,
	).Scan(&l.ID, &l.SubscriptionNo, &l.UserID, &l.BranchID, &l.VendorID,
		&l.PlanID, &l.Status, &start, &end, &tz,
		&l.AddressID, &l.Materialised, &l.CutoffHours, &l.Currency, &l.RotationID)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("subscriptions: load: %w", err)
	}

	loc, lerr := time.LoadLocation(tz)
	if lerr != nil {
		// A branch with a bad timezone string must not stop its customers'
		// meals. UTC is wrong by four hours here and that is survivable; a
		// crashed scheduler is not.
		loc = time.UTC
	}
	l.Location = loc
	if start != nil {
		l.Start = *start
	}
	if end != nil {
		l.End = *end
	}
	// suspended and cancelled both stop new work; the difference is whether the
	// customer can resume without re-subscribing.
	l.Suspended = l.Status == "suspended" || l.Status == "cancelled" || l.Status == "pending_payment"

	if l.Rules, err = r.LoadRules(ctx, tx, subID); err != nil {
		return nil, err
	}
	if l.Pauses, err = r.loadPauses(ctx, tx, subID); err != nil {
		return nil, err
	}
	if l.Skips, err = r.loadSkips(ctx, tx, subID); err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *Repository) loadPauses(ctx context.Context, tx pgx.Tx, subID uuid.UUID) ([]Pause, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, from_date, to_date, credit_mode, state
		  FROM subscription_pauses
		 WHERE subscription_id = $1
		   AND state IN ('scheduled', 'active')
		 ORDER BY from_date`, subID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: load pauses: %w", err)
	}
	defer rows.Close()

	var out []Pause
	for rows.Next() {
		var p Pause
		var state string
		if err := rows.Scan(&p.ID, &p.From, &p.To, &p.Mode, &state); err != nil {
			return nil, fmt.Errorf("subscriptions: scan pause: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) loadSkips(ctx context.Context, tx pgx.Tx, subID uuid.UUID) ([]Skip, error) {
	// Bounded to the future. A subscription with two years of skip history
	// would otherwise load every one of them on every nightly run to plan the
	// next fortnight.
	rows, err := tx.Query(ctx, `
		SELECT skip_date, COALESCE(meal_slot, '')
		  FROM subscription_skips
		 WHERE subscription_id = $1
		   AND skip_date >= CURRENT_DATE - 1`, subID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: load skips: %w", err)
	}
	defer rows.Close()

	var out []Skip
	for rows.Next() {
		var s Skip
		if err := rows.Scan(&s.Date, &s.Slot); err != nil {
			return nil, fmt.Errorf("subscriptions: scan skip: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Address is the snapshot written onto each fulfillment.
type Address struct {
	ID        uuid.UUID
	Line      string
	Landmark  string
	Area      string
	City      string
	Notes     string
	Recipient string
	Phone     string
	Lat, Lng  float64
}

func (r *Repository) LoadAddresses(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]Address, error) {
	// The same projection ordering.LoadAddress uses. GCC addressing is
	// landmark-based — "villa behind ADNOC, Al Wasl Road" is a complete address
	// — so the line is composed from the parts that exist rather than read from
	// a single column.
	rows, err := tx.Query(ctx, `
		SELECT id,
		       concat_ws(', ', NULLIF(flat,''), NULLIF(building,''), NULLIF(street,'')) AS line,
		       COALESCE(landmark,''), COALESCE(area,''), COALESCE(city,''), COALESCE(notes,''),
		       COALESCE(recipient_name,''), COALESCE(recipient_phone,''),
		       ST_Y(point::geometry), ST_X(point::geometry)
		  FROM addresses
		 WHERE id = ANY($1) AND deleted_at IS NULL`, ids)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: load addresses: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]Address, len(ids))
	for rows.Next() {
		var a Address
		if err := rows.Scan(&a.ID, &a.Line, &a.Landmark, &a.Area, &a.City,
			&a.Notes, &a.Recipient, &a.Phone, &a.Lat, &a.Lng); err != nil {
			return nil, fmt.Errorf("subscriptions: scan address: %w", err)
		}
		out[a.ID] = a
	}
	return out, rows.Err()
}

// InsertFulfillment materialises one delivery.
//
// ON CONFLICT DO NOTHING against the (subscription, date, sequence) unique
// index. The scheduler is idempotent by construction, but the index is what
// makes a concurrent second run — a nightly job racing a manual trigger —
// impossible to get wrong rather than merely unlikely.
func (r *Repository) InsertFulfillment(ctx context.Context, tx pgx.Tx,
	subID, branchID uuid.UUID, occ Occurrence, reservationID uuid.NullUUID, addr Address) (uuid.UUID, error) {

	var reservation any
	if reservationID.Valid {
		reservation = reservationID.UUID
	}

	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO fulfillments (
			subscription_id, branch_id, kind, status, sequence,
			scheduled_date, window_id, reservation_id,
			address_id, address_line, address_landmark, address_area,
			address_city, address_notes, recipient_name, recipient_phone,
			point, payload_ref
		) VALUES (
			$1,$2,'delivery','scheduled',$3,
			$4,$5,$6,
			$7,$8,NULLIF($9,''),NULLIF($10,''),
			NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),
			ST_SetSRID(ST_MakePoint($15,$16),4326)::geography, $17
		)
		ON CONFLICT (subscription_id, scheduled_date, sequence)
		    WHERE subscription_id IS NOT NULL DO NOTHING
		RETURNING id`,
		subID, branchID, occ.Sequence,
		occ.Date, occ.WindowID, reservation,
		addr.ID, addr.Line, addr.Landmark, addr.Area,
		addr.City, addr.Notes, addr.Recipient, addr.Phone,
		addr.Lng, addr.Lat,
		// The slot rides in payload_ref, which the seam defines as opaque to
		// logistics. Dispatch passes it through; the kitchen's prep sheet reads
		// it. This is the one channel by which a vertical tells itself something
		// about a fulfillment without the engine learning what it means.
		occ.Slot,
	).Scan(&id)

	if errors.Is(err, pgx.ErrNoRows) {
		// Already materialised. Not an error — it is the idempotent path, hit
		// every time the nightly job re-covers ground it already covered.
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("subscriptions: insert fulfillment: %w", err)
	}
	return id, nil
}

func (r *Repository) SetMaterialisedThrough(ctx context.Context, tx pgx.Tx, subID uuid.UUID, through time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE subscriptions
		   SET materialised_through = GREATEST(COALESCE(materialised_through, $2), $2)
		 WHERE id = $1`, subID, through)
	if err != nil {
		return fmt.Errorf("subscriptions: set horizon: %w", err)
	}
	return nil
}

// RecordEvent appends to the audit trail.
//
// Support answers "why did my meals stop" from this table, and a mutable status
// column cannot answer it.
func (r *Repository) RecordEvent(ctx context.Context, tx pgx.Tx, subID uuid.UUID,
	eventType, from, to, actorType string, actorID uuid.NullUUID, metadata []byte) error {

	var actor any
	if actorID.Valid {
		actor = actorID.UUID
	}
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO subscription_events
		    (subscription_id, event_type, from_status, to_status, actor_type, actor_id, metadata)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), $5, $6, $7)`,
		subID, eventType, from, to, actorType, actor, metadata)
	if err != nil {
		return fmt.Errorf("subscriptions: record event: %w", err)
	}
	return nil
}

// isUniqueViolation reports whether err is a 23505 on the named constraint.
//
// Matching the constraint name matters: retrying on any unique violation would
// loop forever on a genuine duplicate that a new subscription number cannot fix.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
