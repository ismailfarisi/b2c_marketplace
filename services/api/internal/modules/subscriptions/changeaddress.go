package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Changing a delivery address after purchase moves one or more meals (lunch to
// the office, dinner home) for everything still to come: the schedule rules
// future materialisation reads, and the deliveries already materialised whose
// order cutoff has not passed. Deliveries the kitchen is already preparing
// keep the address they were planned with. Everything happens in one
// transaction: a kitchen that does not deliver to the new address anywhere in
// the subscription or rotation leaves nothing changed.

// effectiveHorizonDays bounds the search for the first delivery that goes to
// the new address.
const effectiveHorizonDays = 8 * 7

// SubscriptionAddressChange is the updated detail plus, per changed meal, the
// first date that goes to the new address.
type SubscriptionAddressChange struct {
	MyDetail
	Changed []DeliveryAddress `json:"changed"`
}

// RotationAddressChange is the updated rotation plus, per changed meal, the
// first date that goes to the new address.
type RotationAddressChange struct {
	Rotation
	Changed []DeliveryAddress `json:"changed"`
}

func addressesInvalid(code, msg string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: "addresses", Code: code, Message: msg}}}
}

// slotField names the meal a refusal is about: `addresses[lunch]`. The body
// is a list, but the meal, not its position, is what the reader has to fix.
func slotField(slot string) string { return "addresses[" + slot + "]" }

// outsideRefusals returns the fields of err when it is a 422 made only of
// outside_delivery_area refusals, so a rotation can keep checking its other
// weeks and name every kitchen and meal that refuses, not just the first.
func outsideRefusals(err error) []httpx.FieldError {
	var ve *httpx.ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) == 0 {
		return nil
	}
	for _, f := range ve.Fields {
		if f.Code != "outside_delivery_area" {
			return nil
		}
	}
	return ve.Fields
}

// ChangeSubscriptionAddresses moves the listed meals of one subscription to
// new addresses of the customer's. Someone else's subscription is 404; a
// company lunch is 409 company_lunch; a finished subscription is 409
// not_changeable; a kitchen that does not deliver there is 422
// outside_delivery_area.
func (s *Service) ChangeSubscriptionAddresses(ctx context.Context, userID, subID uuid.UUID,
	changes map[string]uuid.UUID, now time.Time) (SubscriptionAddressChange, error) {

	if len(changes) == 0 {
		return SubscriptionAddressChange{}, addressesInvalid("required", "choose at least one meal to move")
	}
	var effective map[string]*string
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		company, err := isCompanyLunch(ctx, tx, subID)
		if err != nil {
			return err
		}
		if company {
			return httpx.Coded(httpx.ErrConflict, "company_lunch",
				"Your company manages the office address for this lunch.")
		}
		if live.RotationID.Valid {
			return httpx.Coded(httpx.ErrConflict, "rotation_week",
				"Change the address on the rotation; it applies to every week.")
		}
		if !slices.Contains(addressChangeStatuses, live.Status) {
			return httpx.Coded(httpx.ErrConflict, "not_changeable", "This subscription has ended.")
		}
		if err := s.checkOwnAddresses(ctx, tx, userID, changes); err != nil {
			return err
		}
		effective, err = s.applyAddressChange(ctx, tx, live, changes, now, "")
		return err
	})
	if err != nil {
		return SubscriptionAddressChange{}, err
	}
	d, err := s.MyDetail(ctx, userID, subID, now)
	if err != nil {
		return SubscriptionAddressChange{}, err
	}
	out := SubscriptionAddressChange{MyDetail: d}
	out.Changed = markEffective(out.DeliveryAddresses, effective)
	return out, nil
}

// ChangeRotationAddresses moves the listed meals of a rotation for every week
// that has not ended and for a renewal that has not started yet. Any kitchen
// that does not deliver to the new address fails the whole change, naming the
// kitchen and its week.
func (s *Service) ChangeRotationAddresses(ctx context.Context, userID, rotationID uuid.UUID,
	changes map[string]uuid.UUID, now time.Time) (RotationAddressChange, error) {

	if len(changes) == 0 {
		return RotationAddressChange{}, addressesInvalid("required", "choose at least one meal to move")
	}
	var effective map[string]*string
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var slots []string
		err := tx.QueryRow(ctx, `SELECT status, meal_slots FROM meal_rotations
			 WHERE id = $1 AND user_id = $2 FOR UPDATE`, rotationID, userID).Scan(&status, &slots)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("subscriptions: change rotation address: %w", err)
		}
		if !slices.Contains(addressChangeStatuses, status) {
			return httpx.Coded(httpx.ErrConflict, "not_changeable", "This rotation has ended.")
		}
		if err := unknownSlot(slots, changes); err != nil {
			return err
		}
		if err := s.checkOwnAddresses(ctx, tx, userID, changes); err != nil {
			return err
		}
		effective, err = s.applyRotationAddressChange(ctx, tx, userID, rotationID, changes, now)
		return err
	})
	if err != nil {
		return RotationAddressChange{}, err
	}
	r, err := s.MyRotation(ctx, userID, rotationID)
	if err != nil {
		return RotationAddressChange{}, err
	}
	out := RotationAddressChange{Rotation: r}
	out.Changed = markEffective(out.DeliveryAddresses, effective)
	return out, nil
}

func unknownSlot(slots []string, changes map[string]uuid.UUID) error {
	var unknown []httpx.FieldError
	for _, slot := range slices.Sorted(maps.Keys(changes)) {
		if !slices.Contains(slots, slot) {
			unknown = append(unknown, httpx.FieldError{Field: slotField(slot), Code: "unknown_slot",
				Message: fmt.Sprintf("%s is not one of this plan's meals", slot)})
		}
	}
	if len(unknown) > 0 {
		return &httpx.ValidationError{Fields: unknown}
	}
	return nil
}

// markEffective stamps the changed meals with their first new-address date
// and returns just those entries.
func markEffective(all []DeliveryAddress, effective map[string]*string) []DeliveryAddress {
	changed := []DeliveryAddress{}
	for i := range all {
		if from, ok := effective[all[i].MealSlot]; ok {
			all[i].EffectiveFrom = from
			changed = append(changed, all[i])
		}
	}
	return changed
}

// applyRotationAddressChange changes every live week of the rotation, the
// rotation's own per-meal rows, and a renewal that has not started yet.
func (s *Service) applyRotationAddressChange(ctx context.Context, tx pgx.Tx, userID, rotationID uuid.UUID,
	changes map[string]uuid.UUID, now time.Time) (map[string]*string, error) {

	rows, err := tx.Query(ctx, `
		SELECT s.id, s.rotation_week FROM subscriptions s
		 WHERE s.rotation_id = $1 AND s.status = ANY($2)
		   AND s.end_date >= (($3::timestamptz AT TIME ZONE s.timezone))::date
		 ORDER BY s.rotation_week FOR UPDATE`, rotationID, addressChangeStatuses, now)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: rotation weeks to move: %w", err)
	}
	type week struct {
		id uuid.UUID
		no int
	}
	weeks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (week, error) {
		var w week
		err := r.Scan(&w.id, &w.no)
		return w, err
	})
	if err != nil {
		return nil, fmt.Errorf("subscriptions: scan rotation weeks: %w", err)
	}
	effective := map[string]*string{}
	for slot := range changes {
		effective[slot] = nil
	}
	loc := time.UTC
	// Every week is checked even after one refuses, so the answer names each
	// kitchen and meal that stands in the way; the transaction rolls back.
	var refused []httpx.FieldError
	for _, w := range weeks {
		live, err := s.repo.LoadLive(ctx, tx, w.id)
		if err != nil {
			return nil, err
		}
		if live.UserID != userID {
			return nil, httpx.ErrNotFound
		}
		loc = live.Location
		eff, err := s.applyAddressChange(ctx, tx, live, changes, now, fmt.Sprintf(" (week %d)", w.no))
		if fields := outsideRefusals(err); fields != nil {
			refused = append(refused, fields...)
			continue
		}
		if err != nil {
			return nil, err
		}
		for slot, from := range eff {
			if from != nil && (effective[slot] == nil || *from < *effective[slot]) {
				effective[slot] = from
			}
		}
	}
	if len(refused) == 0 {
		if err := insertRotationAddresses(ctx, tx, rotationID, changes); err != nil {
			return nil, err
		}
	}
	var successor uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM meal_rotations
		 WHERE renewed_from_id = $1 AND status <> 'cancelled' AND start_date > $2
		 ORDER BY created_at DESC LIMIT 1`, rotationID, dayIn(now, loc)).Scan(&successor)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("subscriptions: renewal rotation: %w", err)
	}
	if err == nil {
		_, err := s.applyRotationAddressChange(ctx, tx, userID, successor, changes, now)
		if fields := outsideRefusals(err); fields != nil {
			refused = append(refused, fields...)
		} else if err != nil {
			return nil, err
		}
	}
	if len(refused) > 0 {
		return nil, &httpx.ValidationError{Fields: refused}
	}
	return effective, nil
}

// changedAddressIDs lists the distinct addresses meals are moving to.
func changedAddressIDs(changes map[string]uuid.UUID) []uuid.UUID {
	var ids []uuid.UUID
	for _, slot := range slices.Sorted(maps.Keys(changes)) {
		if !slices.Contains(ids, changes[slot]) {
			ids = append(ids, changes[slot])
		}
	}
	return ids
}

// checkOwnAddresses rejects addresses that are not the customer's live ones.
// A company office saved for an office lunch is the company's, not a place
// the customer can send their own meals.
func (s *Service) checkOwnAddresses(ctx context.Context, tx pgx.Tx, userID uuid.UUID, changes map[string]uuid.UUID) error {
	ids := changedAddressIDs(changes)
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(DISTINCT id) FROM addresses
		 WHERE id = ANY($1) AND user_id = $2 AND deleted_at IS NULL AND company_office_id IS NULL`, ids, userID).Scan(&n); err != nil {
		return fmt.Errorf("subscriptions: own addresses: %w", err)
	}
	if n != len(ids) {
		return httpx.Coded(httpx.ErrNotFound, "address_not_found", "address not found")
	}
	return nil
}

// applyAddressChange moves the listed meals of one subscription: it checks
// the kitchen delivers to each new address, rewrites the schedule rules, and
// re-addresses the materialised deliveries whose cutoff has not passed.
// where names the week in error messages for rotations. It returns, per
// slot, the first date that will go to the new address (nil if none in the
// next eight weeks).
func (s *Service) applyAddressChange(ctx context.Context, tx pgx.Tx, live *Live,
	changes map[string]uuid.UUID, now time.Time, where string) (map[string]*string, error) {

	var slots []string
	weekdays := map[string]map[time.Weekday]bool{}
	for _, r := range live.Rules {
		if !slices.Contains(slots, r.Slot) {
			slots = append(slots, r.Slot)
			weekdays[r.Slot] = map[time.Weekday]bool{}
		}
		weekdays[r.Slot][time.Weekday(r.Weekday)] = true
	}
	if err := unknownSlot(slots, changes); err != nil {
		return nil, err
	}
	var kitchen string
	if err := tx.QueryRow(ctx, `SELECT name FROM vendor_branches WHERE id = $1`, live.BranchID).Scan(&kitchen); err != nil {
		return nil, fmt.Errorf("subscriptions: kitchen name: %w", err)
	}
	ids := changedAddressIDs(changes)
	covered, err := addressCoverage(ctx, tx, live.UserID, live.BranchID, ids)
	if err != nil {
		return nil, err
	}
	ordered := slices.Sorted(maps.Keys(changes))
	slices.SortStableFunc(ordered, func(a, b string) int { return slotOrder(a) - slotOrder(b) })
	var refused []httpx.FieldError
	for _, slot := range ordered {
		ok, found := covered[changes[slot]]
		if !found {
			return nil, httpx.Coded(httpx.ErrNotFound, "address_not_found", "address not found")
		}
		if !ok {
			refused = append(refused, httpx.FieldError{Field: slotField(slot), Code: "outside_delivery_area",
				Message: fmt.Sprintf("%s does not deliver %s to that address%s", kitchen, slot, where)})
		}
	}
	if len(refused) > 0 {
		return nil, &httpx.ValidationError{Fields: refused}
	}
	addrs, err := s.repo.LoadAddresses(ctx, tx, ids)
	if err != nil {
		return nil, err
	}

	today := dayIn(now, live.Location)
	effective := make(map[string]*string, len(changes))
	for _, slot := range ordered {
		addr := addrs[changes[slot]]
		if _, err := tx.Exec(ctx, `UPDATE subscription_schedule_rules SET address_id = $3
			 WHERE subscription_id = $1 AND meal_slot = $2`, live.ID, slot, addr.ID); err != nil {
			return nil, fmt.Errorf("subscriptions: move rule: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT id, scheduled_date, status FROM fulfillments
			 WHERE subscription_id = $1 AND payload_ref = $2 AND scheduled_date >= $3
			 ORDER BY scheduled_date FOR UPDATE`, live.ID, slot, today)
		if err != nil {
			return nil, fmt.Errorf("subscriptions: deliveries to move: %w", err)
		}
		type delivery struct {
			id     uuid.UUID
			date   time.Time
			status string
		}
		deliveries, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (delivery, error) {
			var d delivery
			err := r.Scan(&d.id, &d.date, &d.status)
			return d, err
		})
		if err != nil {
			return nil, fmt.Errorf("subscriptions: scan deliveries: %w", err)
		}
		planned := map[string]bool{} // date -> rewritten
		for _, d := range deliveries {
			key := d.date.Format("2006-01-02")
			moved := d.status == "scheduled" && CanModify(d.date, live.CutoffHours, live.Location, now)
			if moved {
				if _, err := tx.Exec(ctx, `
					UPDATE fulfillments
					   SET address_id = $2, address_line = $3, address_landmark = NULLIF($4,''),
					       address_area = NULLIF($5,''), address_city = NULLIF($6,''),
					       address_notes = NULLIF($7,''), recipient_name = NULLIF($8,''),
					       recipient_phone = NULLIF($9,''),
					       point = ST_SetSRID(ST_MakePoint($10,$11),4326)::geography
					 WHERE id = $1`,
					d.id, addr.ID, addr.Line, addr.Landmark, addr.Area, addr.City, addr.Notes,
					addr.Recipient, addr.Phone, addr.Lng, addr.Lat); err != nil {
					return nil, fmt.Errorf("subscriptions: move delivery: %w", err)
				}
			}
			if _, seen := planned[key]; !seen || moved {
				planned[key] = moved
			}
		}
		effective[slot] = firstNewAddressDate(live, slot, weekdays[slot], planned, today, now)
	}

	meta := map[string]any{}
	for slot, id := range changes {
		meta[slot] = id
	}
	if err := s.repo.RecordEvent(ctx, tx, live.ID, "addresses_changed", live.Status, live.Status,
		"customer", uuid.NullUUID{UUID: live.UserID, Valid: true},
		mustJSON(map[string]any{"addresses": meta})); err != nil {
		return nil, err
	}
	if err := s.insertOutbox(ctx, tx, live.ID, "subscription.addresses_changed", map[string]any{
		"subscription_id": live.ID, "user_id": live.UserID, "addresses": meta,
	}); err != nil {
		return nil, err
	}
	return effective, nil
}

// firstNewAddressDate finds the first delivery date of the slot that goes to
// the new address: a materialised delivery that was re-addressed, or a
// delivery not materialised yet whose cutoff has not passed. Pauses and skips
// are not consulted: they do not change where a meal goes, only whether.
func firstNewAddressDate(live *Live, slot string, weekdays map[time.Weekday]bool,
	planned map[string]bool, today, now time.Time) *string {

	from := today
	if start := dayIn(live.Start, live.Location); start.After(from) {
		from = start
	}
	for i := 0; i < effectiveHorizonDays; i++ {
		d := from.AddDate(0, 0, i)
		if !live.End.IsZero() && d.After(dayIn(live.End, live.Location)) {
			return nil
		}
		if !weekdays[d.Weekday()] {
			continue
		}
		key := d.Format("2006-01-02")
		if moved, materialised := planned[key]; materialised {
			if moved {
				return &key
			}
			continue
		}
		if CanModify(d, live.CutoffHours, live.Location, now) {
			return &key
		}
	}
	return nil
}
