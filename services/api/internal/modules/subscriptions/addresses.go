package subscriptions

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
)

// addressQuerier is the part of pgx.Tx and *pgxpool.Pool the address helpers
// need.
type addressQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// rotationAddresses is where a rotation delivers: the default address and the
// meals that go somewhere else (lunch at the office, dinner at home).
type rotationAddresses struct {
	Default uuid.UUID
	Slots   map[string]uuid.UUID
}

// All lists every distinct address the rotation delivers to, default first.
func (ra rotationAddresses) All() []uuid.UUID {
	return distinctAddresses(ra.Default, ra.Slots)
}

// distinctAddresses lists the default address and every per-slot address once,
// default first, so coverage queries see each kitchen must reach all of them.
func distinctAddresses(def uuid.UUID, slots map[string]uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{def}
	keys := make([]string, 0, len(slots))
	for k := range slots {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if id := slots[k]; !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// liveAddressIDs lists the distinct addresses a subscription delivers to: its
// default and every schedule rule's own address.
func liveAddressIDs(live *Live) []uuid.UUID {
	out := []uuid.UUID{live.AddressID}
	for _, r := range live.Rules {
		if r.AddressID.Valid && !slices.Contains(out, r.AddressID.UUID) {
			out = append(out, r.AddressID.UUID)
		}
	}
	return out
}

// rotationSlotAddresses loads the per-meal addresses stored for a rotation.
func rotationSlotAddresses(ctx context.Context, q addressQuerier, rotationID uuid.UUID) (map[string]uuid.UUID, error) {
	rows, err := q.Query(ctx,
		`SELECT meal_slot, address_id FROM meal_rotation_addresses WHERE rotation_id = $1`, rotationID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: rotation slot addresses: %w", err)
	}
	defer rows.Close()
	out := map[string]uuid.UUID{}
	for rows.Next() {
		var slot string
		var id uuid.UUID
		if err := rows.Scan(&slot, &id); err != nil {
			return nil, fmt.Errorf("subscriptions: scan rotation slot address: %w", err)
		}
		out[slot] = id
	}
	return out, rows.Err()
}

// loadRotationAddresses loads a rotation's default and per-meal addresses.
func loadRotationAddresses(ctx context.Context, q addressQuerier, rotationID uuid.UUID) (rotationAddresses, error) {
	var ra rotationAddresses
	if err := q.QueryRow(ctx, `SELECT address_id FROM meal_rotations WHERE id = $1`, rotationID).
		Scan(&ra.Default); err != nil {
		return ra, fmt.Errorf("subscriptions: rotation address: %w", err)
	}
	slots, err := rotationSlotAddresses(ctx, q, rotationID)
	if err != nil {
		return ra, err
	}
	ra.Slots = slots
	return ra, nil
}

// addressCoverage says, for each of the customer's own live addresses among
// ids, whether the kitchen's delivery area contains it. Addresses that are not
// the customer's or are deleted are absent from the result.
func addressCoverage(ctx context.Context, q addressQuerier, userID, branchID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := q.Query(ctx, `
		SELECT a.id,
		       COALESCE(ST_Intersects(b.service_area, a.point), false)
		  FROM addresses a
		  JOIN vendor_branches b ON b.id = $3
		 WHERE a.id = ANY($1) AND a.user_id = $2 AND a.deleted_at IS NULL`,
		ids, userID, branchID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: check addresses: %w", err)
	}
	defer rows.Close()
	covered := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		var ok bool
		if err := rows.Scan(&id, &ok); err != nil {
			return nil, fmt.Errorf("subscriptions: scan address: %w", err)
		}
		covered[id] = ok
	}
	return covered, rows.Err()
}

// addressProblem turns one address's coverage into the field error a client
// can show next to the right meal: kitchen names the kitchen when known, slot
// names the meal (empty for the default address).
func addressProblem(covered map[uuid.UUID]bool, id uuid.UUID, field, kitchen, slot string) error {
	ok, found := covered[id]
	if found && ok {
		return nil
	}
	who := "this kitchen"
	if kitchen != "" {
		who = kitchen
	}
	if !found {
		msg := "address not found"
		if slot != "" {
			msg = fmt.Sprintf("address for %s not found", slot)
		}
		return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: "not_found", Message: msg}}}
	}
	msg := fmt.Sprintf("%s does not deliver to that address", who)
	if slot != "" {
		msg = fmt.Sprintf("%s does not deliver %s to that address", who, slot)
	}
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: "outside_delivery_area", Message: msg}}}
}

// checkAddresses verifies the default address and every schedule rule's own
// address belong to the customer and sit inside the kitchen's delivery area.
// Errors name the meal: field schedule[<i>].address_id for rule i, address_id
// for the default.
func (s *Service) checkAddresses(ctx context.Context, tx pgx.Tx, in SubscribeInput, branchID uuid.UUID) error {
	ids := []uuid.UUID{in.AddressID}
	for _, r := range in.Rules {
		if r.AddressID.Valid {
			ids = append(ids, r.AddressID.UUID)
		}
	}
	covered, err := addressCoverage(ctx, tx, in.UserID, branchID, ids)
	if err != nil {
		return err
	}
	if err := addressProblem(covered, in.AddressID, "address_id", "", ""); err != nil {
		return err
	}
	for i, r := range in.Rules {
		if !r.AddressID.Valid {
			continue
		}
		if err := addressProblem(covered, r.AddressID.UUID,
			fmt.Sprintf("schedule[%d].address_id", i), "", r.Slot); err != nil {
			return err
		}
	}
	return nil
}

// checkRotationAddresses verifies one rotation kitchen delivers to the default
// address and to every per-meal address. Errors name the kitchen and the meal
// under field slot_addresses (address_id for the default).
func checkRotationAddresses(ctx context.Context, q addressQuerier, in RotationInput, kitchen string, branchID uuid.UUID) error {
	covered, err := addressCoverage(ctx, q, in.UserID, branchID, distinctAddresses(in.AddressID, in.SlotAddresses))
	if err != nil {
		return err
	}
	if err := addressProblem(covered, in.AddressID, "address_id", kitchen, ""); err != nil {
		return err
	}
	for _, slot := range in.Slots {
		id, ok := in.SlotAddresses[slot]
		if !ok {
			continue
		}
		if err := addressProblem(covered, id, "slot_addresses", kitchen, slot); err != nil {
			return err
		}
	}
	return nil
}

// validateSlotAddresses rejects per-meal addresses for meals the rotation
// does not have.
func validateSlotAddresses(slots []string, slotAddresses map[string]uuid.UUID) error {
	for slot := range slotAddresses {
		if !slices.Contains(slots, slot) {
			return rotationInvalid("slot_addresses", "unknown_slot",
				fmt.Sprintf("%s is not one of the rotation's meals", slot))
		}
	}
	return nil
}

// slotAddress is the address a rule for slot should carry: the per-meal choice
// when there is one, otherwise none (the subscription default applies).
func slotAddress(slotAddresses map[string]uuid.UUID, slot string) uuid.NullUUID {
	if id, ok := slotAddresses[slot]; ok {
		return uuid.NullUUID{UUID: id, Valid: true}
	}
	return uuid.NullUUID{}
}

// insertRotationAddresses stores the per-meal addresses of a rotation.
func insertRotationAddresses(ctx context.Context, tx pgx.Tx, rotationID uuid.UUID, slotAddresses map[string]uuid.UUID) error {
	for slot, id := range slotAddresses {
		if _, err := tx.Exec(ctx, `
			INSERT INTO meal_rotation_addresses (rotation_id, meal_slot, address_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (rotation_id, meal_slot) DO UPDATE SET address_id = EXCLUDED.address_id`,
			rotationID, slot, id); err != nil {
			return fmt.Errorf("subscriptions: insert rotation address: %w", err)
		}
	}
	return nil
}

// DeliveryAddress is where one meal of a subscription or rotation goes.
// EffectiveFrom is set after an address change: the first delivery date that
// goes to the new address.
type DeliveryAddress struct {
	MealSlot      string    `json:"meal_slot"`
	AddressID     uuid.UUID `json:"address_id"`
	Label         string    `json:"label"`
	Line          string    `json:"line"`
	EffectiveFrom *string   `json:"effective_from"`
}

// sortSlots orders meal slots as they fall in the day.
func sortSlots(slots []string) []string {
	out := slices.Clone(slots)
	slices.SortStableFunc(out, func(a, b string) int { return slotOrder(a) - slotOrder(b) })
	return out
}

// ruleSlotAddresses reduces a subscription's schedule rules to one address per
// meal slot: the address most of the slot's weekdays use (the default when a
// rule has none). It also returns the slots in display order.
func ruleSlotAddresses(rules []Rule, def uuid.UUID) ([]string, map[string]uuid.UUID) {
	counts := map[string]map[uuid.UUID]int{}
	var slots []string
	for _, r := range rules {
		if _, ok := counts[r.Slot]; !ok {
			counts[r.Slot] = map[uuid.UUID]int{}
			slots = append(slots, r.Slot)
		}
		id := def
		if r.AddressID.Valid {
			id = r.AddressID.UUID
		}
		counts[r.Slot][id]++
	}
	out := make(map[string]uuid.UUID, len(slots))
	for slot, c := range counts {
		best, bestN := def, -1
		for id, n := range c {
			if n > bestN || (n == bestN && id == def) {
				best, bestN = id, n
			}
		}
		out[slot] = best
	}
	return sortSlots(slots), out
}

// deliveryAddressesFor describes each meal slot's address with its label and
// a one-line summary. Slots missing from perSlot use def.
func deliveryAddressesFor(ctx context.Context, q addressQuerier, slots []string, def uuid.UUID, perSlot map[string]uuid.UUID) ([]DeliveryAddress, error) {
	ids := distinctAddresses(def, perSlot)
	rows, err := q.Query(ctx, `
		SELECT id, label,
		       concat_ws(', ', NULLIF(flat,''), NULLIF(building,''), NULLIF(street,''),
		                 NULLIF(landmark,''), NULLIF(area,''), NULLIF(city,''))
		  FROM addresses WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: delivery addresses: %w", err)
	}
	defer rows.Close()
	type labelled struct{ label, line string }
	known := map[uuid.UUID]labelled{}
	for rows.Next() {
		var id uuid.UUID
		var l labelled
		if err := rows.Scan(&id, &l.label, &l.line); err != nil {
			return nil, fmt.Errorf("subscriptions: scan delivery address: %w", err)
		}
		known[id] = l
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DeliveryAddress, 0, len(slots))
	for _, slot := range sortSlots(slots) {
		id := def
		if a, ok := perSlot[slot]; ok {
			id = a
		}
		l := known[id]
		out = append(out, DeliveryAddress{MealSlot: slot, AddressID: id, Label: l.label, Line: l.line})
	}
	return out, nil
}

// addressChangeStatuses are the subscription and rotation statuses whose
// delivery addresses may still be changed.
var addressChangeStatuses = []string{"active", "paused", "pending_payment"}

func canChangeAddress(status string, companyLunch bool) bool {
	return !companyLunch && slices.Contains(addressChangeStatuses, status)
}

// isCompanyLunch says whether a company pays for and addresses the subscription.
func isCompanyLunch(ctx context.Context, q addressQuerier, subID uuid.UUID) (bool, error) {
	var company bool
	err := q.QueryRow(ctx, `SELECT company_id IS NOT NULL FROM subscriptions WHERE id = $1`, subID).Scan(&company)
	if err != nil {
		return false, fmt.Errorf("subscriptions: company lunch: %w", err)
	}
	return company, nil
}
