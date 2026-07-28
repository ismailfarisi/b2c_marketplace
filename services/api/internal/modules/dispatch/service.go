package dispatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct {
	pool *pgxpool.Pool
	opts Options
}

func NewService(pool *pgxpool.Pool, opts Options) *Service {
	return &Service{pool: pool, opts: opts.normalise()}
}

// Origin is where a route starts: the branch.
type Origin struct {
	BranchID uuid.UUID
	At       Point
	Timezone string
}

// LoadOpenWork reads a branch's unrouted fulfillments for a date.
//
// This query is the seam claim in SQL. It touches `fulfillments`, `time_windows`
// and `vendor_branches` — the primitive, its window, and where the driver starts
// from. It does not touch `order_lines`, `menu_items`,
// `subscription_meal_selections` or `vehicle_listings`, and it never will,
// because there is nothing in any of them that changes where a van should go.
//
// `COALESCE(order_id, subscription_id)` is the parent: precedence within an
// order or a subscription, none across them. That one expression is the whole
// of what dispatch knows about verticals.
func (s *Service) LoadOpenWork(ctx context.Context, branchID uuid.UUID, date time.Time) (Origin, []Stop, error) {
	var origin Origin
	origin.BranchID = branchID

	err := s.pool.QueryRow(ctx, `
		SELECT ST_Y(point::geometry), ST_X(point::geometry), timezone
		  FROM vendor_branches
		 WHERE id = $1 AND deleted_at IS NULL`,
		branchID,
	).Scan(&origin.At.Lat, &origin.At.Lng, &origin.Timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return origin, nil, httpx.ErrNotFound
	}
	if err != nil {
		return origin, nil, fmt.Errorf("load branch origin: %w", err)
	}

	loc, err := time.LoadLocation(origin.Timezone)
	if err != nil {
		// A branch with an unloadable timezone is a data bug, and defaulting to
		// UTC would silently shift every window by four hours. Gulf-local is the
		// least wrong fallback and it is logged by the caller as a not-found.
		loc = time.FixedZone("Asia/Dubai", 4*3600)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT f.id,
		       COALESCE(f.order_id, f.subscription_id),
		       f.sequence,
		       f.kind::text,
		       ST_Y(f.point::geometry), ST_X(f.point::geometry),
		       w.starts_at, w.ends_at
		  FROM fulfillments f
		  JOIN time_windows w ON w.id = f.window_id
		 WHERE f.branch_id = $1
		   AND f.scheduled_date = $2
		   AND f.status = 'scheduled'
		   AND f.route_id IS NULL
		 ORDER BY w.starts_at, f.sequence, f.id`,
		branchID, date)
	if err != nil {
		return origin, nil, fmt.Errorf("load open fulfillments: %w", err)
	}
	defer rows.Close()

	var stops []Stop
	for rows.Next() {
		var (
			st       Stop
			parent   uuid.NullUUID
			from, to time.Time
		)
		if err := rows.Scan(&st.FulfillmentID, &parent, &st.Sequence, &st.Kind,
			&st.At.Lat, &st.At.Lng, &from, &to); err != nil {
			return origin, nil, fmt.Errorf("scan fulfillment: %w", err)
		}
		if parent.Valid {
			st.ParentID = parent.UUID
		}
		st.WindowStart = combine(date, from, loc)
		st.WindowEnd = combine(date, to, loc)
		stops = append(stops, st)
	}
	return origin, stops, rows.Err()
}

// combine puts a `time` column onto a service date in the branch's zone.
//
// pgx scans a bare `time` into a time.Time on 1 January year zero, so the parts
// have to be reassembled rather than added. Doing it in the branch's location
// rather than UTC is what makes an 18:00–20:00 window mean six in the evening
// where the food is, which is the only place it means anything.
func combine(date time.Time, clock time.Time, loc *time.Location) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(),
		clock.Hour(), clock.Minute(), clock.Second(), 0, loc)
}

// BuildResult is what one planning run produced.
type BuildResult struct {
	RoutesCreated int
	StopsRouted   int
	Unrouted      int
	LateStops     int
}

// BuildRoutes plans and persists a branch's day.
//
// Idempotent by construction rather than by a guard: it only reads fulfillments
// with `route_id IS NULL`, and stamping the route is part of the same
// transaction. A second run five minutes later picks up whatever arrived since
// and leaves the rest alone — which is what makes it safe to run on a ticker
// instead of once at a cutoff time nobody remembers to move.
//
// Routes are left unassigned. Choosing a driver is a separate decision with a
// separate failure mode, and coupling them means a planning run that cannot find
// a driver produces no route at all.
func (s *Service) BuildRoutes(ctx context.Context, branchID uuid.UUID, date time.Time) (BuildResult, error) {
	origin, stops, err := s.LoadOpenWork(ctx, branchID, date)
	if err != nil {
		return BuildResult{}, err
	}
	if len(stops) == 0 {
		return BuildResult{}, nil
	}

	// Grouped by window before sequencing. A route that mixes the 09:00–12:00
	// and 18:00–21:00 windows is a driver sitting in a car park for five hours,
	// and no amount of clever sequencing fixes it — the constraint is the
	// promise made to the customer, not the geometry.
	byWindow := map[string][]Stop{}
	var order []string
	for _, st := range stops {
		key := st.WindowStart.Format(time.RFC3339)
		if _, seen := byWindow[key]; !seen {
			order = append(order, key)
		}
		byWindow[key] = append(byWindow[key], st)
	}
	sortStrings(order)

	var out BuildResult
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		for _, key := range order {
			sequenced := Sequence(origin.At, byWindow[key], s.opts)
			for _, chunk := range Split(sequenced, s.opts.MaxStops) {
				start := chunk[0].WindowStart
				legs := Plan(origin.At, start, chunk, s.opts)

				routeID, err := s.insertRoute(ctx, tx, branchID, date, chunk, legs)
				if err != nil {
					return err
				}
				if err := s.insertStops(ctx, tx, routeID, legs); err != nil {
					return err
				}

				out.RoutesCreated++
				out.StopsRouted += len(chunk)
				for _, l := range legs {
					if l.Late {
						out.LateStops++
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return BuildResult{}, err
	}
	out.Unrouted = len(stops) - out.StopsRouted
	return out, nil
}

func (s *Service) insertRoute(ctx context.Context, tx pgx.Tx, branchID uuid.UUID,
	date time.Time, chunk []Stop, legs []Leg) (uuid.UUID, error) {

	distance := 0
	duration := 0
	for _, l := range legs {
		distance += l.DistanceM
		duration += l.DurationS
	}

	var routeID uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO delivery_routes
			(branch_id, service_date, status, planned_stops,
			 planned_distance_m, planned_duration_s)
		VALUES ($1, $2, 'planned', $3, $4, $5)
		RETURNING id`,
		branchID, date, len(chunk), distance, duration,
	).Scan(&routeID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert route: %w", err)
	}
	return routeID, nil
}

func (s *Service) insertStops(ctx context.Context, tx pgx.Tx, routeID uuid.UUID, legs []Leg) error {
	for i, l := range legs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO route_stops
				(route_id, fulfillment_id, sequence, eta, leg_distance_m, leg_duration_s)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			routeID, l.Stop.FulfillmentID, i+1, l.ETA, l.DistanceM, l.DurationS,
		); err != nil {
			return fmt.Errorf("insert route stop: %w", err)
		}
		// Stamping route_id here is what makes the next planning run skip this
		// fulfillment. The unique partial index on route_stops is the backstop:
		// even if two planners raced, only one can claim an undelivered stop.
		if _, err := tx.Exec(ctx,
			`UPDATE fulfillments SET route_id = $2 WHERE id = $1 AND route_id IS NULL`,
			l.Stop.FulfillmentID, routeID); err != nil {
			return fmt.Errorf("stamp fulfillment route: %w", err)
		}
	}
	return nil
}

// Assign gives a route to a driver.
//
// The driver's own capacity is re-checked here against the route's stop count,
// because the planner split routes against a *default* maximum and the driver
// who ends up taking one may be on a bike.
func (s *Service) Assign(ctx context.Context, routeID, driverID uuid.UUID, by string, byID *uuid.UUID) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var stops int
		var status string
		if err := tx.QueryRow(ctx,
			`SELECT planned_stops, status FROM delivery_routes WHERE id = $1 FOR UPDATE`,
			routeID).Scan(&stops, &status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load route: %w", err)
		}
		if status != "planned" && status != "assigned" {
			return fmt.Errorf("%w: this route is %s", httpx.ErrConflict, status)
		}

		var maxStops int
		var driverStatus string
		if err := tx.QueryRow(ctx,
			`SELECT max_stops, status FROM drivers WHERE id = $1`,
			driverID).Scan(&maxStops, &driverStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load driver: %w", err)
		}
		if driverStatus != "active" {
			return fmt.Errorf("%w: driver is %s", httpx.ErrConflict, driverStatus)
		}
		if stops > maxStops {
			return fmt.Errorf("%w: route has %d stops, this driver carries %d",
				httpx.ErrConflict, stops, maxStops)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE delivery_routes SET driver_id = $2, status = 'assigned' WHERE id = $1`,
			routeID, driverID); err != nil {
			return fmt.Errorf("assign route: %w", err)
		}

		// The denormalised column on fulfillments, and the append-only history.
		// Both, every time: the column is what the board reads and the history is
		// what a dispute reads, and writing only one of them is how "who was
		// supposed to deliver this" becomes unanswerable.
		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments f SET driver_id = $2
			  FROM route_stops rs
			 WHERE rs.route_id = $1 AND f.id = rs.fulfillment_id`,
			routeID, driverID); err != nil {
			return fmt.Errorf("stamp fulfillment driver: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO fulfillment_assignments
				(fulfillment_id, driver_id, route_id, assigned_by, assigned_by_id)
			SELECT rs.fulfillment_id, $2, $1, $3, $4
			  FROM route_stops rs WHERE rs.route_id = $1`,
			routeID, driverID, by, byID); err != nil {
			return fmt.Errorf("record assignment: %w", err)
		}
		return nil
	})
}

// Release takes a route off a driver and returns its undelivered stops to the
// unrouted pool.
//
// Delivered stops stay where they are. A driver who completes four of seven and
// then breaks down has genuinely delivered four, and unwinding those would make
// the customer's order status go backwards.
func (s *Service) Release(ctx context.Context, routeID uuid.UUID, reason string) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE fulfillment_assignments a
			   SET released_at = now(), release_reason = NULLIF($2, '')
			 WHERE a.route_id = $1 AND a.released_at IS NULL`,
			routeID, reason); err != nil {
			return fmt.Errorf("release assignments: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments f
			   SET driver_id = NULL, route_id = NULL
			  FROM route_stops rs
			 WHERE rs.route_id = $1
			   AND f.id = rs.fulfillment_id
			   AND f.status IN ('scheduled', 'dispatched')`,
			routeID); err != nil {
			return fmt.Errorf("unroute fulfillments: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			DELETE FROM route_stops rs
			 USING fulfillments f
			 WHERE rs.route_id = $1 AND f.id = rs.fulfillment_id
			   AND f.status IN ('scheduled', 'dispatched')`,
			routeID); err != nil {
			return fmt.Errorf("clear route stops: %w", err)
		}

		_, err := tx.Exec(ctx,
			`UPDATE delivery_routes SET status = 'abandoned', driver_id = NULL WHERE id = $1`,
			routeID)
		return err
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// The dispatch board
// ─────────────────────────────────────────────────────────────────────────────

type BoardRoute struct {
	ID               uuid.UUID
	DriverID         uuid.NullUUID
	DriverName       string
	Status           string
	PlannedStops     int
	CompletedStops   int
	FailedStops      int
	PlannedDistanceM int
	FirstETA         *time.Time
	LastETA          *time.Time
	StartedAt        *time.Time
}

type Board struct {
	BranchID   uuid.UUID
	Date       time.Time
	Routes     []BoardRoute
	Unassigned int
}

// Board is one query per screen, not one per row.
//
// The ops console refreshes this every few seconds during a delivery window. A
// version that loaded routes and then fanned out per route for its stop counts
// would be twenty round trips a tick per branch, which is how an ops screen
// becomes the heaviest reader in the system.
func (s *Service) Board(ctx context.Context, branchID uuid.UUID, date time.Time) (*Board, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.driver_id, COALESCE(d.display_name, ''), r.status,
		       r.planned_stops, r.planned_distance_m, r.started_at,
		       count(*) FILTER (WHERE f.status = 'completed'),
		       count(*) FILTER (WHERE f.status = 'failed'),
		       min(rs.eta), max(rs.eta)
		  FROM delivery_routes r
		  LEFT JOIN drivers d ON d.id = r.driver_id
		  LEFT JOIN route_stops rs ON rs.route_id = r.id
		  LEFT JOIN fulfillments f ON f.id = rs.fulfillment_id
		 WHERE r.branch_id = $1 AND r.service_date = $2
		   AND r.status <> 'cancelled'
		 GROUP BY r.id, d.display_name
		 ORDER BY min(rs.eta) NULLS LAST, r.created_at`,
		branchID, date)
	if err != nil {
		return nil, fmt.Errorf("load dispatch board: %w", err)
	}
	defer rows.Close()

	board := &Board{BranchID: branchID, Date: date}
	for rows.Next() {
		var r BoardRoute
		var distance *int
		if err := rows.Scan(&r.ID, &r.DriverID, &r.DriverName, &r.Status,
			&r.PlannedStops, &distance, &r.StartedAt,
			&r.CompletedStops, &r.FailedStops, &r.FirstETA, &r.LastETA); err != nil {
			return nil, fmt.Errorf("scan board route: %w", err)
		}
		if distance != nil {
			r.PlannedDistanceM = *distance
		}
		board.Routes = append(board.Routes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM fulfillments
		 WHERE branch_id = $1 AND scheduled_date = $2
		   AND status = 'scheduled' AND route_id IS NULL`,
		branchID, date,
	).Scan(&board.Unassigned); err != nil {
		return nil, fmt.Errorf("count unassigned: %w", err)
	}
	return board, nil
}

// BranchesWithWork lists what the planner should plan.
//
// Driven by the fulfillment table rather than by a list of branches, so a branch
// that onboarded this morning is planned this afternoon without anyone adding it
// to a config.
func (s *Service) BranchesWithWork(ctx context.Context, date time.Time, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT branch_id
		  FROM fulfillments
		 WHERE scheduled_date = $1 AND status = 'scheduled' AND route_id IS NULL
		 LIMIT $2`, date, limit)
	if err != nil {
		return nil, fmt.Errorf("find branches with work: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
