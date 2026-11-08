package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/modules/kitchen"
)

// Anti-boredom features over the ordinary weekly subscriptions: a "next week"
// preview (anticipation of what's coming) and an end-of-cycle recap for a
// four-kitchen rotation (people remember the peak and the end).

// NextWeekNoticeHour is when, on Thursday in the kitchen's time zone, the
// "next week" notification goes out: the GCC weekend starts Friday.
const NextWeekNoticeHour = 18

// PreviewDish is one dish on next week's menu for a meal.
type PreviewDish struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	NameAr    *string   `json:"name_ar"`
	ImageURL  *string   `json:"image_url"`
	IsDefault bool      `json:"is_default"`
	Chosen    bool      `json:"chosen"`
}

// PreviewMeal is one scheduled meal next week and the dishes on offer.
type PreviewMeal struct {
	Date    string        `json:"date"`
	Weekday int           `json:"weekday"`
	Slot    string        `json:"meal_slot"`
	Dishes  []PreviewDish `json:"dishes"`
}

// NextWeekPreview is next delivery week for one subscription: who cooks and
// what, from that kitchen's menu-cycle week for those dates.
type NextWeekPreview struct {
	SubscriptionID       uuid.UUID     `json:"subscription_id"`
	RotationID           *uuid.UUID    `json:"rotation_id"`
	RotationWeek         *int          `json:"rotation_week"`
	BranchID             uuid.UUID     `json:"branch_id"`
	KitchenName          string        `json:"kitchen_name"`
	KitchenNameAr        *string       `json:"kitchen_name_ar"`
	CoverImageURL        *string       `json:"cover_image_url"`
	Cuisines             []string      `json:"cuisines"`
	KitchenDescription   *string       `json:"kitchen_description"`
	KitchenDescriptionAr *string       `json:"kitchen_description_ar"`
	StartsOn             string        `json:"starts_on"`
	EndsOn               string        `json:"ends_on"`
	MenuWeek             int           `json:"menu_week"`
	Meals                []PreviewMeal `json:"meals"`

	loc *time.Location
}

// NextWeekStart is the Sunday that starts the week after the one containing t,
// as a calendar day in loc.
func NextWeekStart(t time.Time, loc *time.Location) time.Time {
	d := dayIn(t, loc)
	return d.AddDate(0, 0, 7-int(d.Weekday()))
}

// NextWeekNoticeDue reports whether the "next week" notice for the coming week
// may be sent at now in loc: from Thursday 18:00 until the week starts (Friday
// and Saturday catch up a missed Thursday run). It returns the week's Sunday.
func NextWeekNoticeDue(now time.Time, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	l := now.In(loc)
	switch l.Weekday() {
	case time.Thursday:
		if l.Hour() < NextWeekNoticeHour {
			return time.Time{}, false
		}
	case time.Friday, time.Saturday:
	default:
		return time.Time{}, false
	}
	return NextWeekStart(l, loc), true
}

type previewKey struct {
	weekday int
	slot    string
}

// previewMenu is a branch's active menu cycle, every week of it, with the
// dish photos the preview shows (cycleMenu in customer.go carries the pick
// options instead).
type previewMenu struct {
	anchor time.Time
	weeks  int
	byWeek map[int]map[previewKey][]PreviewDish
}

// week picks the menu-cycle week for day. A cycle whose later weeks have not
// been filled in yet falls back to week 1, which is what the kitchen serves
// today.
func (m previewMenu) week(day time.Time) int {
	w := kitchen.CycleWeek(m.anchor, m.weeks, day)
	if len(m.byWeek[w]) == 0 {
		return 1
	}
	return w
}

func (m previewMenu) dishes(day time.Time, slot string) []PreviewDish {
	return m.byWeek[m.week(day)][previewKey{weekday: int(day.Weekday()), slot: slot}]
}

func (s *Service) previewMenu(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) (previewMenu, error) {
	m := previewMenu{weeks: 1, byWeek: map[int]map[previewKey][]PreviewDish{}}
	rows, err := tx.Query(ctx, `
		SELECT c.anchor_date, c.weeks, cd.week_no, cd.weekday, di.meal_slot, di.is_default,
		       mi.id, mi.name, mi.name_ar, mi.image_url
		  FROM menu_cycles c
		  JOIN menu_cycle_days cd ON cd.cycle_id = c.id
		  JOIN menu_day_items di ON di.cycle_day_id = cd.id
		  JOIN menu_items mi ON mi.id = di.menu_item_id AND mi.is_active
		 WHERE c.id = (SELECT id FROM menu_cycles
		                WHERE branch_id = $1 AND is_active ORDER BY created_at LIMIT 1)
		 ORDER BY cd.week_no, cd.weekday, di.meal_slot, di.is_default DESC, di.sort_order`, branchID)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var weekNo, weekday int
		var slot string
		var d PreviewDish
		if err := rows.Scan(&m.anchor, &m.weeks, &weekNo, &weekday, &slot, &d.IsDefault,
			&d.ID, &d.Name, &d.NameAr, &d.ImageURL); err != nil {
			return m, err
		}
		if m.byWeek[weekNo] == nil {
			m.byWeek[weekNo] = map[previewKey][]PreviewDish{}
		}
		k := previewKey{weekday: weekday, slot: slot}
		m.byWeek[weekNo][k] = append(m.byWeek[weekNo][k], d)
	}
	return m, rows.Err()
}

// NextWeek returns, for each of the customer's paid subscriptions with meals
// next week, who cooks and the dishes per day.
func (s *Service) NextWeek(ctx context.Context, userID uuid.UUID, now time.Time) ([]NextWeekPreview, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return s.nextWeek(ctx, tx, userID, now)
}

func (s *Service) nextWeek(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) ([]NextWeekPreview, error) {
	rows, err := tx.Query(ctx, `
		SELECT id FROM subscriptions
		 WHERE user_id = $1 AND status = 'active'
		   AND (end_date IS NULL OR end_date >= CURRENT_DATE - 1)
		 ORDER BY start_date, rotation_week NULLS LAST, id`, userID)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []NextWeekPreview{}
	for _, id := range ids {
		p, err := s.previewFor(ctx, tx, id, now)
		if err != nil {
			return nil, err
		}
		if p != nil {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (s *Service) previewFor(ctx context.Context, tx pgx.Tx, subID uuid.UUID, now time.Time) (*NextWeekPreview, error) {
	live, err := s.repo.LoadLive(ctx, tx, subID)
	if err != nil {
		return nil, err
	}
	loc := live.Location
	start := NextWeekStart(now, loc)
	end := start.AddDate(0, 0, 6)
	meals, _, err := s.upcoming(ctx, tx, live, start, end, now)
	if err != nil {
		return nil, err
	}
	if len(meals) == 0 {
		return nil, nil
	}
	menu, err := s.previewMenu(ctx, tx, live.BranchID)
	if err != nil {
		return nil, err
	}

	p := &NextWeekPreview{
		SubscriptionID: subID, BranchID: live.BranchID,
		StartsOn: start.Format("2006-01-02"), EndsOn: end.Format("2006-01-02"),
		MenuWeek: menu.week(start), Meals: []PreviewMeal{}, Cuisines: []string{}, loc: loc,
	}
	var rotID uuid.NullUUID
	var rotWeek *int
	if err := tx.QueryRow(ctx, `
		SELECT b.name, b.name_ar, b.cover_image_url, COALESCE(b.cuisines, '{}'),
		       b.kitchen_description, b.kitchen_description_ar, s.rotation_id, s.rotation_week
		  FROM subscriptions s JOIN vendor_branches b ON b.id = s.branch_id
		 WHERE s.id = $1`, subID).Scan(&p.KitchenName, &p.KitchenNameAr, &p.CoverImageURL, &p.Cuisines,
		&p.KitchenDescription, &p.KitchenDescriptionAr, &rotID, &rotWeek); err != nil {
		return nil, err
	}
	if rotID.Valid {
		id := rotID.UUID
		p.RotationID = &id
		p.RotationWeek = rotWeek
	}

	for _, m := range meals {
		day, err := time.ParseInLocation("2006-01-02", m.Date, loc)
		if err != nil {
			return nil, err
		}
		pm := PreviewMeal{Date: m.Date, Weekday: m.Weekday, Slot: m.Slot, Dishes: []PreviewDish{}}
		for _, d := range menu.dishes(day, m.Slot) {
			d.Chosen = m.ChosenID != nil && *m.ChosenID == d.ID
			pm.Dishes = append(pm.Dishes, d)
		}
		p.Meals = append(p.Meals, pm)
	}
	return p, nil
}

// noticeDishes is the short "what's cooking" line for a notification: the
// dish each meal will get (chosen, else default), deduplicated, at most four.
func noticeDishes(p NextWeekPreview) (en, ar string) {
	var e, a []string
	seen := map[uuid.UUID]bool{}
	for _, m := range p.Meals {
		if len(m.Dishes) == 0 {
			continue
		}
		d := m.Dishes[0]
		for _, c := range m.Dishes {
			if c.Chosen {
				d = c
			}
		}
		if seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		e = append(e, d.Name)
		if d.NameAr != nil && *d.NameAr != "" {
			a = append(a, *d.NameAr)
		} else {
			a = append(a, d.Name)
		}
	}
	more := len(e) > 4
	if more {
		e, a = e[:4], a[:4]
	}
	en, ar = strings.Join(e, ", "), strings.Join(a, "، ")
	if more {
		en += "…"
		ar += "…"
	}
	return en, ar
}

func previewDates(p NextWeekPreview) (en, ar string) {
	if len(p.Meals) == 0 {
		return "", ""
	}
	first, _ := time.Parse("2006-01-02", p.Meals[0].Date)
	last, _ := time.Parse("2006-01-02", p.Meals[len(p.Meals)-1].Date)
	fe, fa := dayLabels(first)
	if first.Equal(last) {
		return fe, fa
	}
	le, la := dayLabels(last)
	return fe + " – " + le, fa + " – " + la
}

// ChannelNotifier sends one template on one named channel. The meal notifier
// implements it; a notice that must reach push and WhatsApp both uses it.
type ChannelNotifier interface {
	NotifyOn(ctx context.Context, tx pgx.Tx, userID uuid.UUID, templateKey, channel string,
		transactional bool, params map[string]string, dedupeKey string) error
}

func (s *Service) notifyOn(ctx context.Context, tx pgx.Tx, userID uuid.UUID, key string, channels []string,
	params map[string]string, dedupe string) error {
	if s.notifier == nil {
		return nil
	}
	cn, ok := s.notifier.(ChannelNotifier)
	if !ok {
		return s.notifier.Notify(ctx, tx, userID, key, false, params, dedupe)
	}
	for _, ch := range channels {
		if err := cn.NotifyOn(ctx, tx, userID, key, ch, false, params, dedupe+":"+ch); err != nil {
			return err
		}
	}
	return nil
}

// markNotice records a sent marker; false means this notice was already sent.
func markNotice(ctx context.Context, tx pgx.Tx, kind string, userID uuid.UUID, ref string) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO meal_variety_notices (kind, user_id, ref) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, kind, userID, ref)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// anyZoneNoticeDue is a cheap guard so the sweep only looks for candidates
// while it is Thursday evening to Saturday somewhere.
func anyZoneNoticeDue(now time.Time) bool {
	for off := -12; off <= 14; off++ {
		if _, ok := NextWeekNoticeDue(now, time.FixedZone("", off*3600)); ok {
			return true
		}
	}
	return false
}

// SendNextWeekNotices sends each customer with meals next week one "next week"
// notice (push, which also lands in the in-app inbox, plus WhatsApp), from
// Thursday 18:00 in the kitchen's time zone. Customers with several
// subscriptions get the first kitchen's week; the marker keeps it to one per
// customer per week.
func (s *Service) SendNextWeekNotices(ctx context.Context, now time.Time, batch int) (int, error) {
	if !anyZoneNoticeDue(now) {
		return 0, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT s.user_id
		  FROM subscriptions s
		 WHERE s.status = 'active'
		   AND (s.end_date IS NULL OR s.end_date >= CURRENT_DATE)
		   AND s.start_date <= CURRENT_DATE + 10
		   AND NOT EXISTS (SELECT 1 FROM meal_variety_notices n
		                    WHERE n.kind = 'next_week' AND n.user_id = s.user_id
		                      AND n.sent_at > now() - interval '4 days')
		 LIMIT $1`, batch)
	if err != nil {
		return 0, err
	}
	var users []uuid.UUID
	for rows.Next() {
		var u uuid.UUID
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return 0, err
		}
		users = append(users, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	sent := 0
	for _, u := range users {
		ok, err := s.sendNextWeekNotice(ctx, u, now)
		if err != nil {
			slog.ErrorContext(ctx, "next week notice failed", "user_id", u, "error", err)
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

func (s *Service) sendNextWeekNotice(ctx context.Context, userID uuid.UUID, now time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	previews, err := s.nextWeek(ctx, tx, userID, now)
	if err != nil {
		return false, err
	}
	var pick *NextWeekPreview
	var week time.Time
	for i := range previews {
		w, due := NextWeekNoticeDue(now, previews[i].loc)
		if due && w.Format("2006-01-02") == previews[i].StartsOn {
			pick, week = &previews[i], w
			break
		}
	}
	if pick == nil {
		return false, nil
	}
	ref := week.Format("2006-01-02")
	fresh, err := markNotice(ctx, tx, "next_week", userID, ref)
	if err != nil || !fresh {
		return false, err
	}
	dishes, dishesAr := noticeDishes(*pick)
	dates, datesAr := previewDates(*pick)
	kitchenAr := pick.KitchenName
	if pick.KitchenNameAr != nil && *pick.KitchenNameAr != "" {
		kitchenAr = *pick.KitchenNameAr
	}
	params := map[string]string{
		"kitchen": pick.KitchenName, "kitchen_ar": kitchenAr,
		"dates": dates, "dates_ar": datesAr,
		"dishes": dishes, "dishes_ar": dishesAr,
		"link": "/en/subscriptions",
	}
	if err := s.notifyOn(ctx, tx, userID, "meals.next_week", []string{"push", "whatsapp"}, params,
		"meals-next-week:"+userID.String()+":"+ref); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// RecapKitchen is one of the cycle's kitchens.
type RecapKitchen struct {
	Week          int       `json:"week"`
	BranchID      uuid.UUID `json:"branch_id"`
	KitchenName   string    `json:"kitchen_name"`
	KitchenNameAr *string   `json:"kitchen_name_ar"`
	CoverImageURL *string   `json:"cover_image_url"`
	Cuisines      []string  `json:"cuisines"`
}

// RecapFavourite is the kitchen the customer rated highest this cycle.
type RecapFavourite struct {
	BranchID      uuid.UUID `json:"branch_id"`
	KitchenName   string    `json:"kitchen_name"`
	KitchenNameAr *string   `json:"kitchen_name_ar"`
	Rating        int       `json:"rating"`
}

// RotationRecap sums up a four-kitchen month, shown from week 4.
type RotationRecap struct {
	RotationID      uuid.UUID       `json:"rotation_id"`
	RotationNo      string          `json:"rotation_no"`
	StartsOn        string          `json:"starts_on"`
	EndsOn          string          `json:"ends_on"`
	Week4StartsOn   string          `json:"week4_starts_on"`
	Ended           bool            `json:"ended"`
	Kitchens        []RecapKitchen  `json:"kitchens"`
	KitchenCount    int             `json:"kitchen_count"`
	CuisineCount    int             `json:"cuisine_count"`
	DishesDelivered int             `json:"dishes_delivered"`
	DaysSkipped     int             `json:"days_skipped"`
	Favourite       *RecapFavourite `json:"favourite"`
	RenewPath       string          `json:"renew_path"`
}

// Week4Start is the first day of a rotation's last week.
func Week4Start(start time.Time) time.Time {
	return start.AddDate(0, 0, 7*(RotationKitchens-1))
}

// RecapReady reports whether a rotation starting on start has reached week 4
// by today (both calendar days).
func RecapReady(start, today time.Time) bool {
	return !civil(today).Before(Week4Start(civil(start)))
}

// civil is t's calendar day as a UTC midnight, so dates from different zones
// (a Postgres date, a local "today") compare as days.
func civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func renewPath(id uuid.UUID) string { return "/rotations/" + id.String() + "/renew" }

// recapShownFor is how long after a cycle ends its recap stays on My meals.
const recapShownFor = 28

type recapRow struct {
	id         uuid.UUID
	no         string
	start, end time.Time
	loc        *time.Location
}

func scanRecapRows(rows pgx.Rows) ([]recapRow, error) {
	defer rows.Close()
	var out []recapRow
	for rows.Next() {
		var r recapRow
		var tz string
		if err := rows.Scan(&r.id, &r.no, &r.start, &r.end, &tz); err != nil {
			return nil, err
		}
		r.loc = time.UTC
		if l, err := time.LoadLocation(tz); err == nil {
			r.loc = l
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const recapRowCols = `r.id, r.rotation_no, r.start_date, r.end_date,
	COALESCE((SELECT s.timezone FROM subscriptions s WHERE s.rotation_id = r.id
	           ORDER BY s.rotation_week LIMIT 1), 'Asia/Dubai')`

// MyRecaps returns the recaps of the customer's rotations that have reached
// week 4, newest first, until a few weeks after they end.
func (s *Service) MyRecaps(ctx context.Context, userID uuid.UUID, now time.Time) ([]RotationRecap, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT `+recapRowCols+`
		  FROM meal_rotations r
		 WHERE r.user_id = $1 AND r.status IN ('active', 'completed')
		   AND r.start_date <= CURRENT_DATE + 1 - 21
		   AND r.end_date >= CURRENT_DATE - 1 - $2::int
		 ORDER BY r.start_date DESC`, userID, recapShownFor)
	if err != nil {
		return nil, err
	}
	rr, err := scanRecapRows(rows)
	if err != nil {
		return nil, err
	}
	out := []RotationRecap{}
	for _, r := range rr {
		today := dayIn(now, r.loc)
		if !RecapReady(r.start, today) || civil(today).After(civil(r.end).AddDate(0, 0, recapShownFor)) {
			continue
		}
		rc, err := s.recap(ctx, tx, userID, r, today)
		if err != nil {
			return nil, err
		}
		out = append(out, rc)
	}
	return out, nil
}

func (s *Service) recap(ctx context.Context, tx pgx.Tx, userID uuid.UUID, r recapRow, today time.Time) (RotationRecap, error) {
	rc := RotationRecap{
		RotationID: r.id, RotationNo: r.no,
		StartsOn: r.start.Format("2006-01-02"), EndsOn: r.end.Format("2006-01-02"),
		Week4StartsOn: Week4Start(r.start).Format("2006-01-02"),
		Ended:         civil(today).After(civil(r.end)),
		Kitchens:      []RecapKitchen{}, RenewPath: renewPath(r.id),
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (s.rotation_week)
		       s.rotation_week, b.id, b.name, b.name_ar, b.cover_image_url, COALESCE(b.cuisines, '{}')
		  FROM subscriptions s JOIN vendor_branches b ON b.id = s.branch_id
		 WHERE s.rotation_id = $1 AND s.rotation_week IS NOT NULL
		 -- A swapped week keeps its cancelled subscription; the live one cooked.
		 ORDER BY s.rotation_week, s.status = 'cancelled', s.created_at DESC`, r.id)
	if err != nil {
		return rc, err
	}
	cuisines := map[string]bool{}
	for rows.Next() {
		var k RecapKitchen
		if err := rows.Scan(&k.Week, &k.BranchID, &k.KitchenName, &k.KitchenNameAr, &k.CoverImageURL, &k.Cuisines); err != nil {
			rows.Close()
			return rc, err
		}
		for _, c := range k.Cuisines {
			cuisines[c] = true
		}
		rc.Kitchens = append(rc.Kitchens, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rc, err
	}
	rc.KitchenCount = len(rc.Kitchens)
	rc.CuisineCount = len(cuisines)

	if err := tx.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM fulfillments f JOIN subscriptions s ON s.id = f.subscription_id
		    WHERE s.rotation_id = $1 AND f.status = 'completed'),
		  (SELECT count(DISTINCT k.skip_date) FROM subscription_skips k
		     JOIN subscriptions s ON s.id = k.subscription_id
		    WHERE s.rotation_id = $1)`, r.id).Scan(&rc.DishesDelivered, &rc.DaysSkipped); err != nil {
		return rc, err
	}

	var fav RecapFavourite
	err = tx.QueryRow(ctx, `
		SELECT b.id, b.name, b.name_ar, kr.rating
		  FROM kitchen_reviews kr
		  JOIN subscriptions s ON s.id = kr.subscription_id
		  JOIN vendor_branches b ON b.id = kr.branch_id
		 WHERE s.rotation_id = $1 AND kr.user_id = $2 AND kr.rating >= 4
		 ORDER BY kr.rating DESC, kr.created_at DESC
		 LIMIT 1`, r.id, userID).Scan(&fav.BranchID, &fav.KitchenName, &fav.KitchenNameAr, &fav.Rating)
	switch {
	case err == nil:
		rc.Favourite = &fav
	case !errors.Is(err, pgx.ErrNoRows):
		return rc, err
	}
	return rc, nil
}

// SendRecapNotices sends one recap notice (push, which also lands in the
// in-app inbox) per rotation when its week 4 starts in the kitchen's zone.
func (s *Service) SendRecapNotices(ctx context.Context, now time.Time, batch int) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+recapRowCols+`, r.user_id
		  FROM meal_rotations r
		 WHERE r.status = 'active'
		   AND r.start_date <= CURRENT_DATE + 1 - 21
		   AND r.end_date >= CURRENT_DATE - 1
		   AND NOT EXISTS (SELECT 1 FROM meal_variety_notices n
		                    WHERE n.kind = 'cycle_recap' AND n.user_id = r.user_id AND n.ref = r.id::text)
		 LIMIT $1`, batch)
	if err != nil {
		return 0, err
	}
	type cand struct {
		recapRow
		user uuid.UUID
	}
	var cands []cand
	for rows.Next() {
		var c cand
		var tz string
		if err := rows.Scan(&c.id, &c.no, &c.start, &c.end, &tz, &c.user); err != nil {
			rows.Close()
			return 0, err
		}
		c.loc = time.UTC
		if l, err := time.LoadLocation(tz); err == nil {
			c.loc = l
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	sent := 0
	for _, c := range cands {
		today := dayIn(now, c.loc)
		if !RecapReady(c.start, today) {
			continue
		}
		ok, err := s.sendRecapNotice(ctx, c.user, c.recapRow, today)
		if err != nil {
			slog.ErrorContext(ctx, "recap notice failed", "rotation_id", c.id, "error", err)
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

func (s *Service) sendRecapNotice(ctx context.Context, userID uuid.UUID, r recapRow, today time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	fresh, err := markNotice(ctx, tx, "cycle_recap", userID, r.id.String())
	if err != nil || !fresh {
		return false, err
	}
	rc, err := s.recap(ctx, tx, userID, r, today)
	if err != nil {
		return false, err
	}
	params := map[string]string{
		"kitchens": strconv.Itoa(rc.KitchenCount),
		"dishes":   strconv.Itoa(rc.DishesDelivered),
		"link":     "/en" + rc.RenewPath,
	}
	if err := s.notifyOn(ctx, tx, userID, "meals.cycle_recap", []string{"push"}, params,
		fmt.Sprintf("meals-cycle-recap:%s", r.id)); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
