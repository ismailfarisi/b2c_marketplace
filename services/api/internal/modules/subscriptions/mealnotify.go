package subscriptions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/platform/postgres"
)

// Notifier enqueues a templated message for a user inside the caller's
// transaction. DedupeKey makes repeated sweeps send each message once.
type Notifier interface {
	Notify(ctx context.Context, tx pgx.Tx, userID uuid.UUID, templateKey string, transactional bool,
		params map[string]string, dedupeKey string) error
}

// TomorrowMenuHour is the local hour from which tomorrow's menu is sent.
const TomorrowMenuHour = 18

// ChooseReminderLead is how long before a dish-choice cutoff the reminder goes.
const ChooseReminderLead = 6 * time.Hour

var (
	weekdaysEn = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	weekdaysAr = []string{"الأحد", "الاثنين", "الثلاثاء", "الأربعاء", "الخميس", "الجمعة", "السبت"}
	slotsAr    = map[string]string{
		"breakfast": "الفطور", "lunch": "الغداء", "dinner": "العشاء",
		"snack": "الوجبة الخفيفة", "suhoor": "السحور", "iftar": "الإفطار",
	}
)

func dayLabels(d time.Time) (en, ar string) {
	return fmt.Sprintf("%s %d %s", weekdaysEn[d.Weekday()], d.Day(), d.Month().String()[:3]),
		fmt.Sprintf("%s %d/%d", weekdaysAr[d.Weekday()], d.Day(), int(d.Month()))
}

func dishNames(ds []Dish) (en, ar string) {
	var e, a []string
	for _, d := range ds {
		e = append(e, d.Name)
		if d.NameAr != nil && *d.NameAr != "" {
			a = append(a, *d.NameAr)
		} else {
			a = append(a, d.Name)
		}
	}
	return strings.Join(e, " or "), strings.Join(a, " أو ")
}

// servedDish is what a meal will bring: the customer's pick or the default.
func servedDish(m UpcomingMeal) []Dish {
	for _, o := range m.Options {
		if m.ChosenID != nil && o.ID == *m.ChosenID {
			return []Dish{o}
		}
	}
	for _, o := range m.Options {
		if o.IsDefault {
			return []Dish{o}
		}
	}
	if len(m.Options) > 0 {
		return m.Options[:1]
	}
	return nil
}

func kitchenNames(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) (en, ar string, err error) {
	var nameAr *string
	if err = tx.QueryRow(ctx, `SELECT name, name_ar FROM vendor_branches WHERE id = $1`, branchID).
		Scan(&en, &nameAr); err != nil {
		return "", "", fmt.Errorf("subscriptions: kitchen name: %w", err)
	}
	ar = en
	if nameAr != nil && *nameAr != "" {
		ar = *nameAr
	}
	return en, ar, nil
}

func formatAmount(minor int64, currency string) string {
	return fmt.Sprintf("%s %d.%02d", currency, minor/100, minor%100)
}

func (s *Service) notifyRenewalDue(ctx context.Context, tx pgx.Tx, subID, userID uuid.UUID,
	plan PlanTemplate, cycle Cycle, amount int64) error {
	if s.notifier == nil {
		return nil
	}
	en, ar, err := kitchenNames(ctx, tx, plan.BranchID)
	if err != nil {
		return err
	}
	dEn, dAr := dayLabels(cycle.Starts)
	return s.notifier.Notify(ctx, tx, userID, "meals.renewal_due", true, map[string]string{
		"kitchen": en, "kitchen_ar": ar, "date": dEn, "date_ar": dAr,
		"amount": formatAmount(amount, plan.Currency),
	}, fmt.Sprintf("meals-renewal:%s:%d", subID, cycle.No))
}

// SendMealReminders sends tomorrow's menu (from TomorrowMenuHour local time)
// and a reminder before each dish-choice cutoff the customer has not used.
// Only paid, scheduled meals are considered. Returns how many subscriptions
// were looked at.
func (s *Service) SendMealReminders(ctx context.Context, now time.Time, batch int) (int, error) {
	if s.notifier == nil {
		return 0, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT f.subscription_id
		  FROM fulfillments f
		  JOIN subscriptions s ON s.id = f.subscription_id
		 WHERE f.status = 'scheduled' AND s.status = 'active'
		   AND f.scheduled_date BETWEEN CURRENT_DATE AND CURRENT_DATE + 3
		 LIMIT $1`, batch)
	if err != nil {
		return 0, fmt.Errorf("subscriptions: reminder candidates: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, fmt.Errorf("subscriptions: reminder candidates: %w", err)
	}
	for _, id := range ids {
		if err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			return s.remindOne(ctx, tx, id, now)
		}); err != nil {
			slog.ErrorContext(ctx, "meal reminder failed", "subscription_id", id, "error", err)
		}
	}
	return len(ids), nil
}

func (s *Service) remindOne(ctx context.Context, tx pgx.Tx, subID uuid.UUID, now time.Time) error {
	live, err := s.repo.LoadLive(ctx, tx, subID)
	if err != nil {
		return err
	}
	if live.Status != "active" {
		return nil
	}
	today := dayIn(now, live.Location)
	meals, err := s.upcoming(ctx, tx, live, today, today.AddDate(0, 0, 2), now)
	if err != nil || len(meals) == 0 {
		return err
	}
	kEn, kAr, err := kitchenNames(ctx, tx, live.BranchID)
	if err != nil {
		return err
	}
	tomorrow := today.AddDate(0, 0, 1).Format("2006-01-02")
	var menuEn, menuAr []string
	for _, m := range meals {
		d, _ := time.ParseInLocation("2006-01-02", m.Date, live.Location)
		dEn, dAr := dayLabels(d)
		if m.Date == tomorrow {
			en, ar := dishNames(servedDish(m))
			slotAr := slotsAr[m.Slot]
			if slotAr == "" {
				slotAr = m.Slot
			}
			menuEn = append(menuEn, m.Slot+": "+en)
			menuAr = append(menuAr, slotAr+": "+ar)
		}
		if len(m.Options) > 1 && m.ChosenBy != "customer" && !m.Locked &&
			m.CutoffAt.After(now) && m.CutoffAt.Sub(now) <= ChooseReminderLead {
			en, ar := dishNames(m.Options)
			slotAr := slotsAr[m.Slot]
			if slotAr == "" {
				slotAr = m.Slot
			}
			if err := s.notifier.Notify(ctx, tx, live.UserID, "meals.choose_reminder", false, map[string]string{
				"kitchen": kEn, "kitchen_ar": kAr, "day": dEn, "day_ar": dAr,
				"slot": m.Slot, "slot_ar": slotAr, "options": en, "options_ar": ar,
				"cutoff": m.CutoffAt.In(live.Location).Format("15:04"),
			}, fmt.Sprintf("meals-choose:%s:%s:%s", subID, m.Date, m.Slot)); err != nil {
				return err
			}
		}
	}
	if len(menuEn) > 0 && now.In(live.Location).Hour() >= TomorrowMenuHour {
		d := today.AddDate(0, 0, 1)
		dEn, dAr := dayLabels(d)
		return s.notifier.Notify(ctx, tx, live.UserID, "meals.tomorrow", false, map[string]string{
			"kitchen": kEn, "kitchen_ar": kAr, "day": dEn, "day_ar": dAr,
			"menu": strings.Join(menuEn, "; "), "menu_ar": strings.Join(menuAr, "؛ "),
		}, fmt.Sprintf("meals-tomorrow:%s:%s", subID, tomorrow))
	}
	return nil
}

// NotifyDeliveries tells subscribers when a meal is on the way, delivered or
// could not be delivered, once per delivery and state.
func (s *Service) NotifyDeliveries(ctx context.Context, batch int) (int, error) {
	if s.notifier == nil {
		return 0, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.status::text, s.user_id, s.branch_id
		  FROM fulfillments f
		  JOIN subscriptions s ON s.id = f.subscription_id
		 WHERE f.status IN ('dispatched', 'en_route', 'completed', 'failed')
		   AND f.updated_at >= now() - interval '6 hours'
		 ORDER BY f.updated_at
		 LIMIT $1`, batch)
	if err != nil {
		return 0, fmt.Errorf("subscriptions: delivery updates: %w", err)
	}
	type update struct {
		id, user, branch uuid.UUID
		status           string
	}
	var list []update
	for rows.Next() {
		var u update
		if err := rows.Scan(&u.id, &u.status, &u.user, &u.branch); err != nil {
			rows.Close()
			return 0, fmt.Errorf("subscriptions: delivery updates: %w", err)
		}
		list = append(list, u)
	}
	rows.Close()
	for _, u := range list {
		key := map[string]string{
			"dispatched": "meals.on_the_way", "en_route": "meals.on_the_way",
			"completed": "meals.delivered", "failed": "meals.delivery_failed",
		}[u.status]
		if err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			en, ar, err := kitchenNames(ctx, tx, u.branch)
			if err != nil {
				return err
			}
			return s.notifier.Notify(ctx, tx, u.user, key, false,
				map[string]string{"kitchen": en, "kitchen_ar": ar},
				fmt.Sprintf("meals-delivery:%s:%s", u.id, key))
		}); err != nil {
			slog.ErrorContext(ctx, "delivery update failed", "fulfillment_id", u.id, "error", err)
		}
	}
	return len(list), nil
}
