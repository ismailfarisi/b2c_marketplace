package driver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/seam/fulfillment"
)

// Config carries the commission numbers cash collection needs.
//
// The same constants cmd/api hands to ordering and subscriptions. A cash order
// and a card order must not earn the platform different margins, and three
// copies of the number drift the first time one of them is tuned.
type Config struct {
	CommissionBP  int
	PlatformVATBP int
}

// Notifier is how the delivery code reaches the customer.
//
// Declared here rather than imported from the notify package, per the module
// rule: cross-module access goes through an interface the *consumer* declares.
// What this module knows about notifications is one sentence — "send this code
// to this number, once" — and that is deliberately all of it. It does not know
// what a template is, which channel is used, or that consent exists.
//
// Nil is a supported wiring. A deployment with no SMS provider still dispatches
// stops; it just cannot complete cash deliveries, and §8 of the Phase 8 doc
// says so rather than leaving it to be discovered at a door.
type Notifier interface {
	NotifyDeliveryCode(ctx context.Context, tx pgx.Tx, in DeliveryCode) (uuid.UUID, error)
}

// DeliveryCode is the whole of the notification contract this module needs.
type DeliveryCode struct {
	FulfillmentID uuid.UUID
	Phone         string
	RecipientName string
	Code          string
	// Attempt distinguishes a redelivery's code from the original's. A failed
	// first attempt issues a fresh code, and a notifier that deduplicated on
	// the fulfillment alone would suppress it.
	Attempt int
}

type Service struct {
	pool   *pgxpool.Pool
	cfg    Config
	notify Notifier
}

func NewService(pool *pgxpool.Pool, cfg Config, n Notifier) *Service {
	return &Service{pool: pool, cfg: cfg, notify: n}
}

// Identify resolves the signed-in user to a driver.
//
// Every method below takes a userID and resolves it here rather than accepting a
// driverID from the client. A driver app that can name which driver it is acting
// as is a driver app that can complete someone else's deliveries.
func (s *Service) Identify(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT id, status FROM drivers WHERE user_id = $1`, userID).Scan(&id, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httpx.ErrForbidden
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("identify driver: %w", err)
	}
	if status != "active" {
		return uuid.Nil, fmt.Errorf("%w: driver account is %s", httpx.ErrForbidden, status)
	}
	return id, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Shifts
// ─────────────────────────────────────────────────────────────────────────────

// StartShift opens the driver's availability.
//
// It refuses a second open shift rather than silently reusing the first: two
// overlapping shifts make "was anyone on shift at 19:40" ambiguous, and that is
// the first question asked after a late delivery.
func (s *Service) StartShift(ctx context.Context, driverID uuid.UUID, branchID uuid.NullUUID) (uuid.UUID, error) {
	var shiftID uuid.UUID
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var open int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM driver_shifts WHERE driver_id = $1 AND status = 'active'`,
			driverID).Scan(&open); err != nil {
			return fmt.Errorf("count open shifts: %w", err)
		}
		if open > 0 {
			return fmt.Errorf("%w: this driver already has an open shift", httpx.ErrConflict)
		}

		return tx.QueryRow(ctx, `
			INSERT INTO driver_shifts (driver_id, branch_id, starts_at, ends_at, started_at, status)
			VALUES ($1, $2, now(), now() + interval '12 hours', now(), 'active')
			RETURNING id`,
			driverID, branchID,
		).Scan(&shiftID)
	})
	return shiftID, err
}

// EndShift closes availability and releases anything still assigned.
//
// The release is the part that matters. A driver who ends their shift with four
// undelivered stops has four customers waiting on someone who has gone home, and
// leaving the route assigned means the next planning run skips that work
// entirely — the fulfillments look handled and nobody is handling them.
func (s *Service) EndShift(ctx context.Context, driverID uuid.UUID) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE driver_shifts SET status = 'ended', ended_at = now()
			 WHERE driver_id = $1 AND status = 'active'`, driverID); err != nil {
			return fmt.Errorf("end shift: %w", err)
		}

		rows, err := tx.Query(ctx, `
			SELECT id FROM delivery_routes
			 WHERE driver_id = $1 AND status IN ('assigned', 'in_progress')`, driverID)
		if err != nil {
			return fmt.Errorf("find open routes: %w", err)
		}
		var routes []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			routes = append(routes, id)
		}
		rows.Close()

		for _, routeID := range routes {
			if err := releaseRoute(ctx, tx, routeID, "driver ended shift"); err != nil {
				return err
			}
		}
		return nil
	})
}

// releaseRoute mirrors dispatch.Release inside an existing transaction.
//
// Duplicated rather than shared, because sharing it would mean `driver`
// importing `dispatch` for one statement and the two packages would then be
// welded together for good. Nine lines of SQL is the cheaper of the two costs.
func releaseRoute(ctx context.Context, tx pgx.Tx, routeID uuid.UUID, reason string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE fulfillment_assignments SET released_at = now(), release_reason = $2
		 WHERE route_id = $1 AND released_at IS NULL`, routeID, reason); err != nil {
		return fmt.Errorf("release assignments: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE fulfillments f SET driver_id = NULL, route_id = NULL
		  FROM route_stops rs
		 WHERE rs.route_id = $1 AND f.id = rs.fulfillment_id
		   AND f.status IN ('scheduled', 'dispatched')`, routeID); err != nil {
		return fmt.Errorf("unroute fulfillments: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM route_stops rs USING fulfillments f
		 WHERE rs.route_id = $1 AND f.id = rs.fulfillment_id
		   AND f.status IN ('scheduled', 'dispatched')`, routeID); err != nil {
		return fmt.Errorf("clear route stops: %w", err)
	}
	_, err := tx.Exec(ctx,
		`UPDATE delivery_routes SET status = 'abandoned', driver_id = NULL WHERE id = $1`, routeID)
	return err
}

// ─────────────────────────────────────────────────────────────────────────────
// The manifest
// ─────────────────────────────────────────────────────────────────────────────

// Stop is one line on the driver's screen.
//
// Read the fields: an address, a window, a name, a phone number, an ETA and
// possibly an amount to collect. Everything except the amount was snapshotted
// onto the fulfillment when it was created. There is no item list, no product
// name and no vertical, because the driver does not need any of them to do the
// job — which is the Fulfillment primitive's whole argument, tested here rather
// than asserted.
type Stop struct {
	FulfillmentID uuid.UUID
	RouteID       uuid.UUID
	Sequence      int
	Kind          string
	Status        string

	AddressLine     string
	AddressLandmark string
	AddressArea     string
	AddressCity     string
	AddressNotes    string
	RecipientName   string
	RecipientPhone  string
	Lat, Lng        float64

	WindowLabel string
	ETA         *time.Time

	CODMinor int64
	Currency string

	AttemptCount int
	// RequiresOTP tells the client which capture UI to show *before* the driver
	// is at the door. Discovering the requirement at completion time means the
	// driver has already walked away.
	RequiresOTP bool
}

// Manifest is the driver's day.
func (s *Service) Manifest(ctx context.Context, driverID uuid.UUID, date time.Time) ([]Stop, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, rs.route_id, rs.sequence, f.kind::text, f.status::text,
		       f.address_line, COALESCE(f.address_landmark, ''), COALESCE(f.address_area, ''),
		       COALESCE(f.address_city, ''), COALESCE(f.address_notes, ''),
		       COALESCE(f.recipient_name, ''), COALESCE(f.recipient_phone, ''),
		       ST_Y(f.point::geometry), ST_X(f.point::geometry),
		       w.label, rs.eta, f.attempt_count,
		       COALESCE(c.expected_minor, 0), COALESCE(c.currency, 'AED')
		  FROM route_stops rs
		  JOIN delivery_routes r ON r.id = rs.route_id
		  JOIN fulfillments f ON f.id = rs.fulfillment_id
		  JOIN time_windows w ON w.id = f.window_id
		  LEFT JOIN cod_collections c ON c.fulfillment_id = f.id
		 WHERE r.driver_id = $1
		   AND r.service_date = $2
		   AND r.status IN ('assigned', 'in_progress')
		 ORDER BY rs.sequence`,
		driverID, date)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	defer rows.Close()

	var out []Stop
	for rows.Next() {
		var st Stop
		if err := rows.Scan(&st.FulfillmentID, &st.RouteID, &st.Sequence, &st.Kind, &st.Status,
			&st.AddressLine, &st.AddressLandmark, &st.AddressArea, &st.AddressCity,
			&st.AddressNotes, &st.RecipientName, &st.RecipientPhone,
			&st.Lat, &st.Lng, &st.WindowLabel, &st.ETA, &st.AttemptCount,
			&st.CODMinor, &st.Currency); err != nil {
			return nil, fmt.Errorf("scan manifest stop: %w", err)
		}
		st.RequiresOTP = RequiredProof(fulfillment.Kind(st.Kind), st.CODMinor) == ProofOTP
		out = append(out, st)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Transitions
// ─────────────────────────────────────────────────────────────────────────────

// Advance moves one stop through the lifecycle.
//
// One method for depart/arrive/return-to-en-route rather than three, because
// they differ only in the target status and every one of them needs the same
// four things: ownership, a legal transition, an event row, and the driver's
// position at the time.
func (s *Service) Advance(ctx context.Context, driverID, fulfillmentID uuid.UUID,
	to fulfillment.Status, at *Point) error {

	if to == fulfillment.StatusCompleted || to == fulfillment.StatusFailed {
		return fmt.Errorf("%w: use Complete or Fail — those carry proof and a reason",
			httpx.ErrConflict)
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := lockStop(ctx, tx, driverID, fulfillmentID)
		if err != nil {
			return err
		}
		if !CanTransition(current.Status, to) {
			return fmt.Errorf("%w: a %s stop cannot become %s",
				httpx.ErrConflict, current.Status, to)
		}
		if to == fulfillment.StatusArrived && at != nil {
			if d := current.distanceTo(*at); d > ArrivalRadiusM {
				return fmt.Errorf("%w: you are %d m from the address — move closer to mark arrival",
					httpx.ErrConflict, int(d))
			}
		}
		return transition(ctx, tx, fulfillmentID, current.Status, to, driverID, at, nil)
	})
}

// Dispatch is the transition that issues the delivery OTP.
//
// Issued here rather than at completion because the customer needs it *before*
// the driver arrives, and issuing it at creation means it sits in a notification
// for three days before the delivery it belongs to.
//
// The code never leaves this function. Phase 7 returned it to the handler with
// a comment saying notifications were a Phase 8 concern; that is now closed, and
// the enqueue happens inside the same transaction as the hash. Either the
// customer has a code and the fulfillment has its hash, or neither exists —
// there is no state where a driver arrives at a door holding a code the
// customer was never sent.
//
// Returns whether a delivery code was actually sent. Most stops need none — a
// card-paid delivery is proved by a recipient name and a pickup by a photo —
// and QA found the handler telling every driver "a delivery code has been sent"
// regardless. A driver who reads that at a pickup stands at a door asking for a
// code the customer was never given, with no way to know the message was
// boilerplate.
func (s *Service) Dispatch(ctx context.Context, driverID, fulfillmentID uuid.UUID) (codeSent bool, err error) {
	code, hash, err := GenerateOTP()
	if err != nil {
		return false, err
	}

	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := lockStop(ctx, tx, driverID, fulfillmentID)
		if err != nil {
			return err
		}
		if !CanTransition(current.Status, fulfillment.StatusDispatched) {
			return fmt.Errorf("%w: a %s stop cannot be dispatched", httpx.ErrConflict, current.Status)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments SET otp_hash = $2, otp_issued_at = now() WHERE id = $1`,
			fulfillmentID, hash); err != nil {
			return fmt.Errorf("issue otp: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE delivery_routes SET status = 'in_progress', started_at = COALESCE(started_at, now())
			  WHERE id = $1 AND status = 'assigned'`, current.RouteID); err != nil {
			return fmt.Errorf("start route: %w", err)
		}

		sent, err := s.sendCode(ctx, tx, current, code)
		if err != nil {
			return err
		}
		codeSent = sent

		return transition(ctx, tx, fulfillmentID, current.Status,
			fulfillment.StatusDispatched, driverID, nil, nil)
	})
	if err != nil {
		return false, err
	}
	return codeSent, nil
}

// sendCode enqueues the delivery code and records which message carried it.
//
// Failing the whole dispatch when the notifier errors is the right severity and
// it is worth being explicit about why, because the instinct is to log and carry
// on. A dispatched cash stop whose code was never enqueued is unfinishable: the
// driver reaches the door, the customer has nothing to read out, and the only
// remedy is a support call while someone stands in a corridor. Better to leave
// the stop undispatched and let the driver retry.
//
// A stop that needs no OTP skips this entirely — a card-paid delivery is proved
// by a recipient name, and sending everyone a code they do not need trains
// people to ignore the ones that matter.
func (s *Service) sendCode(ctx context.Context, tx pgx.Tx, stop *stopRow, code string) (bool, error) {
	if RequiredProof(fulfillment.Kind(stop.Kind), stop.CODMinor) != ProofOTP {
		return false, nil
	}
	if s.notify == nil {
		return false, fmt.Errorf("%w: no notification provider is configured, so a cash "+
			"delivery cannot be completed — see docs/PHASE-8 §2", httpx.ErrConflict)
	}
	if stop.RecipientPhone == "" {
		return false, fmt.Errorf("%w: this stop has no recipient phone and needs a delivery code",
			httpx.ErrConflict)
	}

	notificationID, err := s.notify.NotifyDeliveryCode(ctx, tx, DeliveryCode{
		FulfillmentID: stop.FulfillmentID,
		Phone:         stop.RecipientPhone,
		RecipientName: stop.RecipientName,
		Code:          code,
		Attempt:       stop.AttemptCount,
	})
	if err != nil {
		return false, fmt.Errorf("send delivery code: %w", err)
	}

	// Recorded on the fulfillment so the dispatch board can distinguish "the
	// customer is reading out the wrong code" from "the message never left".
	// Those look identical to a driver and need opposite responses.
	if _, err := tx.Exec(ctx, `
		UPDATE fulfillments SET otp_notification_id = $2, otp_sent_at = now() WHERE id = $1`,
		stop.FulfillmentID, notificationID); err != nil {
		return false, fmt.Errorf("record otp notification: %w", err)
	}
	return true, nil
}

// CompleteInput is what the driver captured at the door.
type CompleteInput struct {
	FulfillmentID uuid.UUID
	Proof         ProofKind
	OTP           string
	SignatureKey  string
	PhotoKey      string
	RecipientName string
	Note          string
	At            *Point

	// CollectedMinor is cash actually taken. Allowed to differ from what was
	// expected — the customer had no change, or paid part by card — because a
	// schema that forbids the difference gets a driver entering a lie instead.
	CollectedMinor int64
}

// Complete closes a stop with proof, and with money if there was any.
//
// The order inside the transaction is deliberate: proof is validated first,
// then cash is posted, then the status moves. A completion that posted cash and
// then failed the status update would leave the ledger claiming money for a
// delivery that is still open.
func (s *Service) Complete(ctx context.Context, driverID uuid.UUID, in CompleteInput) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := lockStop(ctx, tx, driverID, in.FulfillmentID)
		if err != nil {
			return err
		}
		if !CanTransition(current.Status, fulfillment.StatusCompleted) {
			return fmt.Errorf("%w: a %s stop cannot be completed", httpx.ErrConflict, current.Status)
		}

		if !ProofAcceptable(in.Proof, fulfillment.Kind(current.Kind), current.CODMinor) {
			return fmt.Errorf("%w: this stop needs %s, not %s",
				httpx.ErrConflict, RequiredProof(fulfillment.Kind(current.Kind), current.CODMinor), in.Proof)
		}
		if in.Proof == ProofOTP && !VerifyOTP(in.OTP, current.OTPHash) {
			return fmt.Errorf("%w: that code does not match", httpx.ErrForbidden)
		}

		distance := 0
		if in.At != nil {
			distance = int(current.distanceTo(*in.At))
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO proof_of_delivery
				(fulfillment_id, kind, otp_verified, signature_key, photo_key,
				 recipient_name, note, captured_point, distance_m)
			VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''),
			        CASE WHEN $8::float8 IS NULL THEN NULL
			             ELSE ST_SetSRID(ST_MakePoint($9, $8), 4326)::geography END,
			        NULLIF($10, 0))
			ON CONFLICT (fulfillment_id) DO NOTHING`,
			in.FulfillmentID, string(in.Proof), in.Proof == ProofOTP,
			in.SignatureKey, in.PhotoKey, in.RecipientName, in.Note,
			latOrNil(in.At), lngOrNil(in.At), distance,
		); err != nil {
			return fmt.Errorf("record proof: %w", err)
		}

		if current.CODMinor > 0 {
			if err := s.collectCash(ctx, tx, driverID, current, in.CollectedMinor); err != nil {
				return err
			}
		}

		if err := recordAttempt(ctx, tx, in.FulfillmentID, driverID,
			current.AttemptCount+1, "delivered", "", in.At); err != nil {
			return err
		}
		if err := transition(ctx, tx, in.FulfillmentID, current.Status,
			fulfillment.StatusCompleted, driverID, in.At, nil); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE route_stops SET arrived_at = COALESCE(arrived_at, now()), departed_at = now()
			 WHERE fulfillment_id = $1 AND departed_at IS NULL`, in.FulfillmentID)
		return err
	})
}

// collectCash records the money and posts it to the ledger.
//
// Commission is computed from what was *collected*, not from what was expected.
// A customer who pays 240 on a 250 order has paid 240, and taking commission on
// 250 charges the vendor for money nobody received.
func (s *Service) collectCash(ctx context.Context, tx pgx.Tx, driverID uuid.UUID,
	current *stopRow, collectedMinor int64) error {

	if collectedMinor <= 0 {
		return fmt.Errorf("%w: this stop expects %d in cash", httpx.ErrConflict, current.CODMinor)
	}

	commission := collectedMinor * int64(s.cfg.CommissionBP) / 10_000
	vat := commission * int64(s.cfg.PlatformVATBP) / 10_000

	if _, err := tx.Exec(ctx, `
		UPDATE cod_collections SET collected_minor = $2, collected_at = now()
		 WHERE fulfillment_id = $1 AND state = 'held'`,
		current.FulfillmentID, collectedMinor); err != nil {
		return fmt.Errorf("record cash: %w", err)
	}

	_, err := ledger.PostCODCollected(ctx, tx, ledger.CODCollection{
		FulfillmentID:      current.FulfillmentID,
		OrderID:            current.OrderID,
		DriverID:           driverID,
		VendorID:           current.VendorID,
		Currency:           current.Currency,
		CollectedMinor:     collectedMinor,
		CommissionMinor:    commission,
		CommissionVATMinor: vat,
	})
	return err
}

// Fail closes a stop without delivering it.
//
// The reason is required and comes from a closed list. Free text would produce
// "customer not answering", "no answer", "nobody home" and "cust absent" as four
// distinct reasons, and then nobody can tell whether the time windows are wrong.
func (s *Service) Fail(ctx context.Context, driverID, fulfillmentID uuid.UUID,
	outcome, note string, at *Point) error {

	if !validOutcomes[outcome] {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "outcome", Code: "invalid",
			Message: "one of customer_absent, wrong_address, refused, unreachable, access_denied, vehicle_issue",
		}}}
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := lockStop(ctx, tx, driverID, fulfillmentID)
		if err != nil {
			return err
		}
		if !CanTransition(current.Status, fulfillment.StatusFailed) {
			return fmt.Errorf("%w: a %s stop cannot be failed", httpx.ErrConflict, current.Status)
		}
		if err := recordAttempt(ctx, tx, fulfillmentID, driverID,
			current.AttemptCount+1, outcome, note, at); err != nil {
			return err
		}
		if err := transition(ctx, tx, fulfillmentID, current.Status,
			fulfillment.StatusFailed, driverID, at, map[string]any{"outcome": outcome}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE fulfillments SET failure_reason = $2 WHERE id = $1`,
			fulfillmentID, outcome); err != nil {
			return fmt.Errorf("record failure reason: %w", err)
		}
		_, err = tx.Exec(ctx,
			`UPDATE route_stops SET departed_at = now() WHERE fulfillment_id = $1 AND departed_at IS NULL`,
			fulfillmentID)
		return err
	})
}

var validOutcomes = map[string]bool{
	"customer_absent": true, "wrong_address": true, "refused": true,
	"unreachable": true, "access_denied": true, "vehicle_issue": true,
	"cancelled_by_customer": true,
}

// ─────────────────────────────────────────────────────────────────────────────
// Location
// ─────────────────────────────────────────────────────────────────────────────

// Ping records a position.
//
// Two writes: the append-only partitioned history, and the denormalised column
// on `drivers` that the dispatch board and the customer's tracking screen read.
// A single write to the history would mean every tracking poll running a
// max(recorded_at) over a partitioned table, which is the query that makes a
// tracking feature the heaviest reader in the system.
func (s *Service) Ping(ctx context.Context, driverID uuid.UUID, at Point,
	accuracyM, speedKMH float32, routeID uuid.NullUUID) error {

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO driver_location_pings (driver_id, point, accuracy_m, speed_kmh, route_id)
			VALUES ($1, ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography, $4, $5, $6)`,
			driverID, at.Lat, at.Lng, accuracyM, speedKMH, routeID); err != nil {
			return fmt.Errorf("record ping: %w", err)
		}
		_, err := tx.Exec(ctx, `
			UPDATE drivers
			   SET last_point = ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography,
			       last_ping_at = now()
			 WHERE id = $1`, driverID, at.Lat, at.Lng)
		return err
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Cash handling
// ─────────────────────────────────────────────────────────────────────────────

// Held is what a driver is currently carrying.
type Held struct {
	Collections int
	TotalMinor  int64
	Currency    string
}

func (s *Service) HeldCash(ctx context.Context, driverID uuid.UUID) (Held, error) {
	var h Held
	err := s.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(collected_minor), 0), COALESCE(min(currency), 'AED')
		  FROM cod_collections
		 WHERE driver_id = $1 AND state = 'held'`,
		driverID,
	).Scan(&h.Collections, &h.TotalMinor, &h.Currency)
	if err != nil {
		return h, fmt.Errorf("load held cash: %w", err)
	}
	return h, nil
}

// DeclareRemittance is the driver saying what they are about to hand over.
//
// Declared and counted are separate fields and separate moments. The driver
// declares; someone at the branch counts. Collapsing them into one number means
// the system can never represent a shortfall, and a system that cannot represent
// a shortfall gets one anyway — silently.
func (s *Service) DeclareRemittance(ctx context.Context, driverID uuid.UUID) (uuid.UUID, int64, error) {
	var (
		remittanceID uuid.UUID
		total        int64
	)
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var currency string
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(sum(collected_minor), 0), COALESCE(min(currency), 'AED')
			  FROM cod_collections
			 WHERE driver_id = $1 AND state = 'held'
			   FOR UPDATE`,
			driverID).Scan(&total, &currency); err != nil {
			return fmt.Errorf("total held cash: %w", err)
		}
		if total <= 0 {
			return fmt.Errorf("%w: nothing to remit", httpx.ErrConflict)
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO cod_remittances (driver_id, reference_no, declared_minor, currency)
			VALUES ($1, generate_reference('CASH'), $2, $3)
			RETURNING id`,
			driverID, total, currency,
		).Scan(&remittanceID); err != nil {
			return fmt.Errorf("create remittance: %w", err)
		}

		_, err := tx.Exec(ctx, `
			UPDATE cod_collections SET remittance_id = $2
			 WHERE driver_id = $1 AND state = 'held'`, driverID, remittanceID)
		return err
	})
	return remittanceID, total, err
}

// SettleRemittance is the branch counting the cash.
//
// The variance is stored and the *counted* figure is what reaches the ledger. A
// shortfall needs no posting of its own: the difference simply stays in the
// driver's float, which is exactly what that account is supposed to mean — cash
// the driver is holding that the platform has not received.
func (s *Service) SettleRemittance(ctx context.Context, remittanceID uuid.UUID,
	countedMinor int64, receivedBy uuid.UUID) error {

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			driverID uuid.UUID
			declared int64
			currency string
			state    string
		)
		if err := tx.QueryRow(ctx, `
			SELECT driver_id, declared_minor, currency, state
			  FROM cod_remittances WHERE id = $1 FOR UPDATE`,
			remittanceID).Scan(&driverID, &declared, &currency, &state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load remittance: %w", err)
		}
		if state == "settled" {
			return nil // idempotent
		}
		if countedMinor < 0 {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "counted_minor", Code: "invalid",
			}}}
		}

		if _, err := tx.Exec(ctx, `
			UPDATE cod_remittances
			   SET counted_minor = $2, variance_minor = $2 - declared_minor,
			       state = 'settled', received_by = $3, settled_at = now()
			 WHERE id = $1`, remittanceID, countedMinor, receivedBy); err != nil {
			return fmt.Errorf("settle remittance: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cod_collections SET state = 'remitted' WHERE remittance_id = $1`,
			remittanceID); err != nil {
			return fmt.Errorf("mark collections remitted: %w", err)
		}

		if countedMinor == 0 {
			return nil
		}
		_, err := ledger.PostCODRemitted(ctx, tx, ledger.CODRemittance{
			RemittanceID: remittanceID,
			DriverID:     driverID,
			Currency:     currency,
			CountedMinor: countedMinor,
		})
		return err
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// internals
// ─────────────────────────────────────────────────────────────────────────────

type stopRow struct {
	FulfillmentID uuid.UUID
	RouteID       uuid.UUID
	OrderID       uuid.UUID
	VendorID      uuid.UUID
	Kind          string
	Status        fulfillment.Status
	Lat, Lng      float64
	AttemptCount  int
	OTPHash       string
	CODMinor      int64
	Currency      string
	// Carried on the fulfillment itself, which is the Fulfillment primitive
	// paying off once more: the delivery code goes to whoever is at the door,
	// and this module resolves that without a join to `orders` or a customer
	// record.
	RecipientPhone string
	RecipientName  string
}

// lockStop loads the fulfillment and proves the driver owns it.
//
// `FOR UPDATE OF f` rather than a plain read: two taps on "complete" from a
// phone with a flaky connection arrive as two requests, and without the lock
// both read `arrived` and both post cash.
func lockStop(ctx context.Context, tx pgx.Tx, driverID, fulfillmentID uuid.UUID) (*stopRow, error) {
	var (
		r       stopRow
		status  string
		otpHash *string
		orderID uuid.NullUUID
	)
	err := tx.QueryRow(ctx, `
		SELECT f.id, COALESCE(f.route_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       f.order_id, b.vendor_id, f.kind::text, f.status::text,
		       ST_Y(f.point::geometry), ST_X(f.point::geometry),
		       f.attempt_count, f.otp_hash,
		       COALESCE(c.expected_minor, 0), COALESCE(c.currency, 'AED'),
		       COALESCE(f.recipient_phone, ''), COALESCE(f.recipient_name, '')
		  FROM fulfillments f
		  JOIN vendor_branches b ON b.id = f.branch_id
		  LEFT JOIN cod_collections c ON c.fulfillment_id = f.id AND c.state = 'held'
		 WHERE f.id = $1 AND f.driver_id = $2
		   FOR UPDATE OF f`,
		fulfillmentID, driverID,
	).Scan(&r.FulfillmentID, &r.RouteID, &orderID, &r.VendorID, &r.Kind, &status,
		&r.Lat, &r.Lng, &r.AttemptCount, &otpHash, &r.CODMinor, &r.Currency,
		&r.RecipientPhone, &r.RecipientName)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not found rather than forbidden: a 403 confirms the fulfillment exists,
		// which lets a driver enumerate other drivers' work.
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load stop: %w", err)
	}
	r.Status = fulfillment.Status(status)
	if otpHash != nil {
		r.OTPHash = *otpHash
	}
	if orderID.Valid {
		r.OrderID = orderID.UUID
	}
	return &r, nil
}

// Point is a coordinate reported by the driver's device.
type Point struct{ Lat, Lng float64 }

// distanceTo is a local haversine rather than a round trip to PostGIS.
//
// This runs inside an open transaction on the completion path, which is the
// hottest write in the driver app. `SELECT ST_Distance(...)` there would add a
// query to every arrival for arithmetic that is fifteen lines.
func (r *stopRow) distanceTo(p Point) float64 {
	const earthRadiusM = 6_371_000.0
	lat1 := r.Lat * math.Pi / 180
	lat2 := p.Lat * math.Pi / 180
	dLat := (p.Lat - r.Lat) * math.Pi / 180
	dLng := (p.Lng - r.Lng) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

func transition(ctx context.Context, tx pgx.Tx, fulfillmentID uuid.UUID,
	from, to fulfillment.Status, driverID uuid.UUID, at *Point, meta map[string]any) error {

	tag, err := tx.Exec(ctx, `
		UPDATE fulfillments
		   SET status = $3::fulfillment_status,
		       dispatched_at = CASE WHEN $3 = 'dispatched' THEN now() ELSE dispatched_at END,
		       arrived_at    = CASE WHEN $3 = 'arrived'    THEN now() ELSE arrived_at END,
		       completed_at  = CASE WHEN $3 = 'completed'  THEN now() ELSE completed_at END,
		       failed_at     = CASE WHEN $3 = 'failed'     THEN now() ELSE failed_at END
		 WHERE id = $1 AND status = $2::fulfillment_status`,
		fulfillmentID, string(from), string(to))
	if err != nil {
		return fmt.Errorf("transition fulfillment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: this stop changed while the request was in flight", httpx.ErrConflict)
	}

	payload := meta
	if payload == nil {
		payload = map[string]any{}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO fulfillment_events
			(fulfillment_id, from_status, to_status, actor_type, actor_id, point, metadata)
		VALUES ($1, $2::fulfillment_status, $3::fulfillment_status, 'driver', $4,
		        CASE WHEN $5::float8 IS NULL THEN NULL
		             ELSE ST_SetSRID(ST_MakePoint($6, $5), 4326)::geography END,
		        $7)`,
		fulfillmentID, string(from), string(to), driverID,
		latOrNil(at), lngOrNil(at), payload)
	if err != nil {
		return fmt.Errorf("record fulfillment event: %w", err)
	}
	return nil
}

func recordAttempt(ctx context.Context, tx pgx.Tx, fulfillmentID, driverID uuid.UUID,
	attemptNo int, outcome, note string, at *Point) error {

	if _, err := tx.Exec(ctx, `
		INSERT INTO delivery_attempts
			(fulfillment_id, driver_id, attempt_no, outcome, note, point)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''),
		        CASE WHEN $6::float8 IS NULL THEN NULL
		             ELSE ST_SetSRID(ST_MakePoint($7, $6), 4326)::geography END)
		ON CONFLICT (fulfillment_id, attempt_no) DO NOTHING`,
		fulfillmentID, driverID, attemptNo, outcome, note,
		latOrNil(at), lngOrNil(at)); err != nil {
		return fmt.Errorf("record attempt: %w", err)
	}
	_, err := tx.Exec(ctx,
		`UPDATE fulfillments SET attempt_count = $2 WHERE id = $1`, fulfillmentID, attemptNo)
	return err
}

func latOrNil(p *Point) any {
	if p == nil {
		return nil
	}
	return p.Lat
}

func lngOrNil(p *Point) any {
	if p == nil {
		return nil
	}
	return p.Lng
}
