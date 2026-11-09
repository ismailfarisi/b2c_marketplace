package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

func fieldMessage(err error, field string) string {
	var v *httpx.ValidationError
	if errors.As(err, &v) {
		for _, fe := range v.Fields {
			if fe.Field == field {
				return fe.Message
			}
		}
	}
	return ""
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec: %v\n%s", err, sql)
	}
}

func addAddress(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, label string, lng, lat float64) uuid.UUID {
	t.Helper()
	id := uuid.New()
	mustExec(t, pool, `INSERT INTO addresses (id, user_id, label, city, street, point) VALUES ($1, $2, $3, 'Dubai', $4,
		ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography)`, id, userID, label, label+" street", lng, lat)
	return id
}

func shrinkArea(t *testing.T, pool *pgxpool.Pool, branchID uuid.UUID, metres int) {
	t.Helper()
	mustExec(t, pool, `UPDATE vendor_branches
		SET service_area = ST_Multi(ST_Buffer(point, $2)::geometry)::geography WHERE id = $1`, branchID, metres)
}

func countRows(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v\n%s", err, sql)
	}
	return n
}

func TestSubscribeNamesTheMealOutsideTheArea(t *testing.T) {
	f := newSubFixture(t, 12)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Dubai")
	start := EarliestStart(12, loc, time.Now()).AddDate(0, 0, 2)
	far := addAddress(t, f.pool, f.userID, "Office", 55.60, 25.14) // ~37 km east of a 15 km area

	in := f.input(start)
	in.Rules[3].AddressID = uuid.NullUUID{UUID: far, Valid: true}
	_, err := f.svc.Subscribe(ctx, in)
	if got := fieldCode(err, "schedule[3].address_id"); got != "outside_delivery_area" {
		t.Fatalf("code = %q (%v), want outside_delivery_area", got, err)
	}
	if msg := fieldMessage(err, "schedule[3].address_id"); !strings.Contains(msg, "lunch") {
		t.Fatalf("message %q should name the meal", msg)
	}

	in.Rules[3].AddressID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	_, err = f.svc.Subscribe(ctx, in)
	if got := fieldCode(err, "schedule[3].address_id"); got != "not_found" {
		t.Fatalf("code = %q (%v), want not_found", got, err)
	}

	near := addAddress(t, f.pool, f.userID, "Office", 55.28, 25.14)
	in.Rules[3].AddressID = uuid.NullUUID{UUID: near, Valid: true}
	if _, err := f.svc.Subscribe(ctx, in); err != nil {
		t.Fatalf("subscribe with a covered per-day address: %v", err)
	}
}

func TestRotationSlotAddressesMustBeCoveredByEveryKitchen(t *testing.T) {
	f := newSwapFixture(t, 7)
	ctx := context.Background()
	mustExec(t, f.pool, `UPDATE subscription_plans SET meal_slots = '{lunch,dinner}', meals_per_day = 2 WHERE id = ANY($1)`, f.plans)
	office := addAddress(t, f.pool, f.userID, "Office", 55.30, 25.14) // ~7 km east, inside 15 km areas
	end, _ := time.Parse("2006-01-02", f.rotation.EndDate)
	in := RotationInput{
		UserID: f.userID, AddressID: f.addressID, SlotAddresses: map[string]uuid.UUID{"dinner": office},
		PlanIDs: f.plans[:RotationKitchens], StartDate: end.AddDate(0, 0, 1),
		Weekdays: []int{0, 1, 2, 3, 4}, Slots: []string{"lunch", "dinner"},
	}

	shrinkArea(t, f.pool, f.branches[1], 2000)
	_, err := f.svc.PreviewRotation(ctx, in)
	if got := fieldCode(err, "slot_addresses"); got != "outside_delivery_area" {
		t.Fatalf("code = %q (%v), want outside_delivery_area", got, err)
	}
	if msg := fieldMessage(err, "slot_addresses"); !strings.Contains(msg, "dinner") || !strings.Contains(msg, "Swap kitchen") {
		t.Fatalf("message %q should name the kitchen and the meal", msg)
	}

	in.SlotAddresses = map[string]uuid.UUID{"supper": office}
	_, err = f.svc.PreviewRotation(ctx, in)
	if got := fieldCode(err, "slot_addresses"); got != "unknown_slot" {
		t.Fatalf("code = %q (%v), want unknown_slot", got, err)
	}
	in.SlotAddresses = map[string]uuid.UUID{"dinner": office}

	shrinkArea(t, f.pool, f.branches[1], 15000)
	r, err := f.svc.CreateRotation(ctx, in)
	if err != nil {
		t.Fatalf("create rotation: %v", err)
	}
	if n := countRows(t, f.pool, `SELECT count(*) FROM meal_rotation_addresses WHERE rotation_id = $1 AND meal_slot = 'dinner' AND address_id = $2`,
		r.ID, office); n != 1 {
		t.Fatalf("dinner rows = %d, want 1", n)
	}
	dinner := countRows(t, f.pool, `SELECT count(*) FROM subscription_schedule_rules sr JOIN subscriptions s ON s.id = sr.subscription_id
		 WHERE s.rotation_id = $1 AND sr.meal_slot = 'dinner' AND sr.address_id = $2`, r.ID, office)
	lunch := countRows(t, f.pool, `SELECT count(*) FROM subscription_schedule_rules sr JOIN subscriptions s ON s.id = sr.subscription_id
		 WHERE s.rotation_id = $1 AND sr.meal_slot = 'lunch' AND sr.address_id IS NULL`, r.ID)
	if dinner != 20 || lunch != 20 {
		t.Fatalf("dinner rules to office = %d, lunch rules default = %d, want 20 and 20", dinner, lunch)
	}
	got, err := f.svc.MyRotation(ctx, f.userID, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.DeliveryAddresses) != 2 || got.DeliveryAddresses[0].MealSlot != "lunch" ||
		got.DeliveryAddresses[0].AddressID != f.addressID || got.DeliveryAddresses[1].AddressID != office ||
		got.DeliveryAddresses[1].Label != "Office" {
		t.Fatalf("delivery_addresses = %+v", got.DeliveryAddresses)
	}
	if !got.CanChangeAddress {
		t.Fatal("a pending rotation can change its address")
	}
}

func TestSwapOptionsAndProposalHonourEveryAddress(t *testing.T) {
	f := newSwapFixture(t, 7)
	ctx := context.Background()
	office := addAddress(t, f.pool, f.userID, "Office", 55.30, 25.14)
	mustExec(t, f.pool, `INSERT INTO meal_rotation_addresses (rotation_id, meal_slot, address_id) VALUES ($1, 'lunch', $2)`,
		f.rotation.ID, office)
	shrinkArea(t, f.pool, f.branches[swapDearer], 2000)

	opts, err := f.svc.SwapOptions(ctx, f.userID, f.rotation.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uuid.UUID]bool{}
	for _, o := range opts.Items {
		seen[o.BranchID] = true
	}
	if seen[f.branches[swapDearer]] || !seen[f.branches[swapCheaper]] {
		t.Fatalf("swap options %v: the kitchen not covering the office must be out, the other in", seen)
	}

	end, _ := time.Parse("2006-01-02", f.rotation.EndDate)
	p, err := f.svc.propose(ctx, f.rotation, f.userID,
		rotationAddresses{Default: f.addressID, Slots: map[string]uuid.UUID{"lunch": office}}, end.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range p.Eligible {
		if k.BranchID == f.branches[swapDearer] {
			t.Fatalf("proposal offers a kitchen that does not deliver to the office")
		}
	}
}

func TestSuggestionHonoursSlotAddresses(t *testing.T) {
	f := newSuggestFixture(t)
	ctx := context.Background()
	var branches []uuid.UUID
	for _, c := range []string{"emirati", "levantine", "indian", "persian", "turkish"} {
		b, _ := f.addKitchen(t, suggestKitchen{cuisine: c, rotation: true, weekPrice: 10000})
		branches = append(branches, b)
	}
	office := addAddress(t, f.pool, f.userID, "Office", f.lng+0.03, f.lat) // ~3 km east, inside 5 km areas
	shrinkArea(t, f.pool, branches[0], 1000)

	out, err := f.svc.SuggestRotation(ctx, SuggestInput{
		UserID: f.userID, AddressID: f.addressID, SlotAddresses: map[string]uuid.UUID{"lunch": office},
	})
	if err != nil {
		var v *httpx.ValidationError
		if errors.As(err, &v) {
			t.Fatalf("%+v", v.Fields)
		}
		t.Fatal(err)
	}
	if len(out.BranchIDs) != RotationKitchens {
		t.Fatalf("branches = %v", out.BranchIDs)
	}
	for _, b := range out.BranchIDs {
		if b == branches[0] {
			t.Fatal("suggested a kitchen that does not deliver to the office")
		}
	}

	_, err = f.svc.SuggestRotation(ctx, SuggestInput{
		UserID: f.userID, AddressID: f.addressID, SlotAddresses: map[string]uuid.UUID{"lunch": uuid.New()},
	})
	if got := fieldCode(err, "slot_addresses"); got != "not_found" {
		t.Fatalf("code = %q (%v), want not_found", got, err)
	}
}

func TestChangeSubscriptionAddresses(t *testing.T) {
	f := newSubFixture(t, 12)
	ctx := context.Background()
	now := time.Now()
	loc, _ := time.LoadLocation("Asia/Dubai")
	res, err := f.svc.Subscribe(ctx, f.input(EarliestStart(12, loc, now)))
	if err != nil {
		t.Fatal(err)
	}
	today := dayIn(now, loc)
	mustExec(t, f.pool, `UPDATE subscriptions SET status = 'active', start_date = $2 WHERE id = $1`, res.ID, today.AddDate(0, 0, -7))
	var branchID uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT branch_id FROM subscriptions WHERE id = $1`, res.ID).Scan(&branchID); err != nil {
		t.Fatal(err)
	}
	insertDelivery := func(date time.Time, status string, seq int) uuid.UUID {
		id := uuid.New()
		mustExec(t, f.pool, `INSERT INTO fulfillments (id, subscription_id, branch_id, kind, status, sequence, scheduled_date,
			window_id, address_id, address_line, point, payload_ref)
			VALUES ($1, $2, $3, 'delivery', $4, $5, $6, $7, $8, 'Home', ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography, 'lunch')`,
			id, res.ID, branchID, status, seq, date, f.windowID, f.addressID)
		return id
	}
	future := today.AddDate(0, 0, 5)
	locked := insertDelivery(today, "scheduled", 1)                       // cutoff passed
	dispatched := insertDelivery(today.AddDate(0, 0, 3), "dispatched", 2) // already on its way
	movable := insertDelivery(future, "scheduled", 3)

	office := addAddress(t, f.pool, f.userID, "Office", 55.28, 25.14)
	far := addAddress(t, f.pool, f.userID, "Far office", 55.60, 25.14)
	stranger := uuid.New()
	mustExec(t, f.pool, `INSERT INTO users (id, phone) VALUES ($1, $2)`, stranger, fmt.Sprintf("+97158%07d", rand.IntN(10_000_000)))
	theirs := addAddress(t, f.pool, stranger, "Theirs", 55.23, 25.14)

	defaultRules := func() int {
		return countRows(t, f.pool, `SELECT count(*) FROM subscription_schedule_rules WHERE subscription_id = $1 AND address_id IS NULL`, res.ID)
	}

	_, err = f.svc.ChangeSubscriptionAddresses(ctx, f.userID, res.ID, map[string]uuid.UUID{"lunch": far}, now)
	if got := fieldCode(err, "addresses"); got != "outside_delivery_area" {
		t.Fatalf("far office: code = %q (%v)", got, err)
	}
	if defaultRules() != 7 {
		t.Fatal("a refused change must leave the schedule alone")
	}
	_, err = f.svc.ChangeSubscriptionAddresses(ctx, f.userID, res.ID, map[string]uuid.UUID{"lunch": theirs}, now)
	if !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("someone else's address: %v, want not found", err)
	}
	_, err = f.svc.ChangeSubscriptionAddresses(ctx, f.userID, res.ID, map[string]uuid.UUID{"dinner": office}, now)
	if got := fieldCode(err, "addresses"); got != "unknown_slot" {
		t.Fatalf("unknown slot: code = %q (%v)", got, err)
	}
	_, err = f.svc.ChangeSubscriptionAddresses(ctx, stranger, res.ID, map[string]uuid.UUID{"lunch": office}, now)
	if !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("someone else's subscription: %v, want not found", err)
	}

	out, err := f.svc.ChangeSubscriptionAddresses(ctx, f.userID, res.ID, map[string]uuid.UUID{"lunch": office}, now)
	if err != nil {
		t.Fatal(err)
	}
	if defaultRules() != 0 || countRows(t, f.pool,
		`SELECT count(*) FROM subscription_schedule_rules WHERE subscription_id = $1 AND address_id = $2`, res.ID, office) != 7 {
		t.Fatal("every weekday of the slot must move")
	}
	addressOf := func(id uuid.UUID) (uuid.UUID, string) {
		var a uuid.UUID
		var line string
		if err := f.pool.QueryRow(ctx, `SELECT address_id, address_line FROM fulfillments WHERE id = $1`, id).Scan(&a, &line); err != nil {
			t.Fatal(err)
		}
		return a, line
	}
	if a, line := addressOf(movable); a != office || line != "Office street" {
		t.Fatalf("movable delivery now at %s %q, want the office", a, line)
	}
	if a, _ := addressOf(locked); a != f.addressID {
		t.Fatal("a delivery past its cutoff must keep its address")
	}
	if a, _ := addressOf(dispatched); a != f.addressID {
		t.Fatal("a dispatched delivery must keep its address")
	}
	if len(out.Changed) != 1 || out.Changed[0].MealSlot != "lunch" || out.Changed[0].EffectiveFrom == nil {
		t.Fatalf("changed = %+v", out.Changed)
	}
	from := *out.Changed[0].EffectiveFrom
	if from <= today.Format("2006-01-02") || from > future.Format("2006-01-02") {
		t.Fatalf("effective_from = %s, want after today and no later than %s", from, future.Format("2006-01-02"))
	}
	if len(out.DeliveryAddresses) != 1 || out.DeliveryAddresses[0].AddressID != office || !out.CanChangeAddress {
		t.Fatalf("detail = %+v can_change=%v", out.DeliveryAddresses, out.CanChangeAddress)
	}
	if n := countRows(t, f.pool, `SELECT count(*) FROM subscription_events WHERE subscription_id = $1 AND event_type = 'addresses_changed'`, res.ID); n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}

	companyID := uuid.New()
	mustExec(t, f.pool, `INSERT INTO companies (id, name, legal_name, billing_email, status, created_by) VALUES ($1, 'Acme', 'Acme LLC', 'a@acme.test', 'active', $2)`, companyID, f.userID)
	mustExec(t, f.pool, `UPDATE subscriptions SET company_id = $2 WHERE id = $1`, res.ID, companyID)
	_, err = f.svc.ChangeSubscriptionAddresses(ctx, f.userID, res.ID, map[string]uuid.UUID{"lunch": f.addressID}, now)
	if !errors.Is(err, httpx.ErrConflict) || codeOf(err) != "company_lunch" {
		t.Fatalf("company lunch: %v, want 409 company_lunch", err)
	}
	d, err := f.svc.MyDetail(ctx, f.userID, res.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.CanChangeAddress {
		t.Fatal("a company lunch cannot change its address")
	}
}

func TestChangeRotationAddressesAppliesToEveryRemainingWeek(t *testing.T) {
	f := newSwapFixture(t, 7)
	ctx := context.Background()
	now := time.Now()
	office := addAddress(t, f.pool, f.userID, "Office", 55.30, 25.14)
	officeRules := func() int {
		return countRows(t, f.pool, `SELECT count(*) FROM subscription_schedule_rules sr JOIN subscriptions s ON s.id = sr.subscription_id
			 WHERE s.rotation_id = $1 AND sr.address_id = $2`, f.rotation.ID, office)
	}

	week3 := f.week(t, 3)
	shrinkArea(t, f.pool, week3.BranchID, 2000)
	_, err := f.svc.ChangeRotationAddresses(ctx, f.userID, f.rotation.ID, map[string]uuid.UUID{"lunch": office}, now)
	if got := fieldCode(err, "addresses"); got != "outside_delivery_area" {
		t.Fatalf("code = %q (%v), want outside_delivery_area", got, err)
	}
	if msg := fieldMessage(err, "addresses"); !strings.Contains(msg, "week 3") || !strings.Contains(msg, week3.KitchenName) {
		t.Fatalf("message %q should name the kitchen and the week", msg)
	}
	if officeRules() != 0 || countRows(t, f.pool, `SELECT count(*) FROM meal_rotation_addresses WHERE rotation_id = $1`, f.rotation.ID) != 0 {
		t.Fatal("a refused change must change nothing")
	}

	shrinkArea(t, f.pool, week3.BranchID, 15000)
	out, err := f.svc.ChangeRotationAddresses(ctx, f.userID, f.rotation.ID, map[string]uuid.UUID{"lunch": office}, now)
	if err != nil {
		t.Fatal(err)
	}
	if officeRules() != 20 {
		t.Fatalf("office rules = %d, want 20 (four weeks of five days)", officeRules())
	}
	if n := countRows(t, f.pool, `SELECT count(*) FROM meal_rotation_addresses WHERE rotation_id = $1 AND meal_slot = 'lunch' AND address_id = $2`,
		f.rotation.ID, office); n != 1 {
		t.Fatalf("rotation rows = %d, want 1", n)
	}
	if len(out.Changed) != 1 || out.Changed[0].EffectiveFrom == nil || *out.Changed[0].EffectiveFrom < f.rotation.StartDate {
		t.Fatalf("changed = %+v", out.Changed)
	}
	if len(out.DeliveryAddresses) != 1 || out.DeliveryAddresses[0].AddressID != office {
		t.Fatalf("delivery_addresses = %+v", out.DeliveryAddresses)
	}

	_, err = f.svc.ChangeRotationAddresses(ctx, uuid.New(), f.rotation.ID, map[string]uuid.UUID{"lunch": office}, now)
	if !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("someone else's rotation: %v", err)
	}
}
