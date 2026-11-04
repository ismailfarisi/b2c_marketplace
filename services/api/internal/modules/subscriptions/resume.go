package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// FirstChangeableDay is the earliest day, from today, whose cutoff has not
// passed. Days before it are already bought for and stay as they are.
func FirstChangeableDay(cutoffHours int, loc *time.Location, now time.Time) time.Time {
	d := dayIn(now, loc)
	for !CanModify(d, cutoffHours, loc, now) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

// ResumeWindow is how a pause ends early: deliveries come back from resumeOn,
// and the pause keeps the days before it. cancel means the pause had not
// started by then and goes away entirely. ok is false when every day left in
// the pause is already past its cutoff.
func ResumeWindow(from, to, firstChangeable time.Time) (resumeOn time.Time, newTo time.Time, cancel, ok bool) {
	if firstChangeable.After(to) {
		return time.Time{}, time.Time{}, false, false
	}
	if !firstChangeable.After(from) {
		return from, time.Time{}, true, true
	}
	return firstChangeable, firstChangeable.AddDate(0, 0, -1), false, true
}

// Resumed reports what a resume did.
type Resumed struct {
	ResumesOn    string `json:"resumes_on"`
	DaysReturned int    `json:"days_returned"`
}

// Resume ends the customer's current or next pause early.
//
// Deliveries come back from the first day still before its cutoff; days
// before that stay paused, because the kitchen has already planned for them.
// The pause days given back return to the allowance, and an "extend" pause
// pulls the end date back in by the same number, so the customer is never both
// fed and credited for one day.
func (s *Service) Resume(ctx context.Context, userID, subID uuid.UUID, now time.Time) (Resumed, error) {
	var out Resumed
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		if live.RotationID.Valid {
			return httpx.Coded(httpx.ErrConflict, "rotation_no_pause",
				"A week in a kitchen rotation cannot be paused; skip meals instead.")
		}
		if live.Status == "cancelled" || live.Status == "completed" {
			return httpx.Coded(httpx.ErrConflict, "not_paused", "There is no pause to end.")
		}

		today := dayIn(now, live.Location)
		var pauseID uuid.UUID
		var from, to time.Time
		var mode string
		var suppressed int
		err = tx.QueryRow(ctx, `
			SELECT id, from_date, to_date, credit_mode, suppressed_days
			  FROM subscription_pauses
			 WHERE subscription_id = $1 AND state IN ('scheduled', 'active')
			   AND to_date >= $2
			 ORDER BY from_date LIMIT 1
			 FOR UPDATE`, subID, today).Scan(&pauseID, &from, &to, &mode, &suppressed)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.Coded(httpx.ErrConflict, "not_paused", "There is no pause to end.")
		}
		if err != nil {
			return fmt.Errorf("subscriptions: load pause: %w", err)
		}
		// A wallet pause has already paid the customer for the days; giving
		// them back would need a wallet debit, which is staff work.
		if mode == "wallet" {
			return httpx.Coded(httpx.ErrConflict, "resume_wallet_pause",
				"This pause was credited to your wallet and cannot be ended early.")
		}
		from, to = dayIn(from, live.Location), dayIn(to, live.Location)

		resumeOn, newTo, cancel, ok := ResumeWindow(from, to,
			FirstChangeableDay(live.CutoffHours, live.Location, now))
		if !ok {
			return httpx.Coded(httpx.ErrConflict, "resume_past_cutoff",
				"The rest of this pause is past the cutoff and can no longer change.")
		}

		remaining := 0
		if cancel {
			if _, err := tx.Exec(ctx,
				`UPDATE subscription_pauses SET state = 'cancelled' WHERE id = $1`, pauseID); err != nil {
				return fmt.Errorf("subscriptions: cancel pause: %w", err)
			}
		} else {
			// Counted from the pause's own start, as it was when created.
			remaining = PausedDays(live.Subscription, live.Rules, Pause{From: from, To: newTo}, from)
			if remaining > suppressed {
				remaining = suppressed
			}
			if _, err := tx.Exec(ctx,
				`UPDATE subscription_pauses SET to_date = $2, suppressed_days = $3 WHERE id = $1`,
				pauseID, newTo, remaining); err != nil {
				return fmt.Errorf("subscriptions: shorten pause: %w", err)
			}
		}
		returned := suppressed - remaining

		if mode == "extend" && returned > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE subscriptions
				   SET pause_days_used = GREATEST(0, pause_days_used - $2),
				       end_date = CASE WHEN end_date IS NULL THEN NULL ELSE end_date - $2::int END
				 WHERE id = $1`, subID, returned); err != nil {
				return fmt.Errorf("subscriptions: return allowance: %w", err)
			}
			cycle, err := s.currentCycle(ctx, tx, subID)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE subscription_cycles SET ends_on = GREATEST(starts_on, ends_on - $2::int)
				 WHERE id = $1`, cycle.ID, returned); err != nil {
				return fmt.Errorf("subscriptions: shorten cycle: %w", err)
			}
			// A renewal already opened after the extended end moves back with it.
			if _, err := tx.Exec(ctx, `
				UPDATE subscription_cycles
				   SET starts_on = starts_on - $3::int, ends_on = ends_on - $3::int
				 WHERE subscription_id = $1 AND cycle_no > $2`, subID, cycle.No, returned); err != nil {
				return fmt.Errorf("subscriptions: move renewal: %w", err)
			}
		}

		// Bring back deliveries the pause cancelled, except days the customer
		// skipped on their own.
		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments f
			   SET status = 'scheduled'
			 WHERE f.subscription_id = $1
			   AND f.scheduled_date BETWEEN $2 AND $3
			   AND f.scheduled_date >= CURRENT_DATE
			   AND f.status = 'cancelled'
			   AND NOT EXISTS (
			       SELECT 1 FROM subscription_skips k
			        WHERE k.subscription_id = f.subscription_id
			          AND k.skip_date = f.scheduled_date
			          AND (k.meal_slot IS NULL OR k.meal_slot = f.payload_ref))`,
			subID, resumeOn, to); err != nil {
			return fmt.Errorf("subscriptions: restore deliveries: %w", err)
		}

		out = Resumed{ResumesOn: resumeOn.Format("2006-01-02"), DaysReturned: returned}
		return s.repo.RecordEvent(ctx, tx, subID, "resumed", "", "",
			"customer", uuid.NullUUID{UUID: userID, Valid: true},
			mustJSON(map[string]any{"pause_id": pauseID, "resumes_on": out.ResumesOn, "days": returned}))
	})
	return out, err
}

// Unskip puts a skipped meal back, until the same cutoff a skip has.
func (s *Service) Unskip(ctx context.Context, userID, subID uuid.UUID, date time.Time, slot string, now time.Time) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		day := dayIn(date, live.Location)
		if !CanModify(day, live.CutoffHours, live.Location, now) {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "date", Code: "past_cutoff",
				Message: fmt.Sprintf(
					"changes must be made at least %d hours before the delivery day",
					live.CutoffHours),
			}}}
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM subscription_skips
			 WHERE subscription_id = $1 AND skip_date = $2
			   AND meal_slot IS NOT DISTINCT FROM NULLIF($3, '')`, subID, day, slot)
		if err != nil {
			return fmt.Errorf("subscriptions: delete skip: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.Coded(httpx.ErrConflict, "not_skipped", "That meal is not skipped.")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments
			   SET status = 'scheduled'
			 WHERE subscription_id = $1
			   AND scheduled_date = $2
			   AND scheduled_date >= CURRENT_DATE
			   AND status = 'cancelled'
			   AND ($3 = '' OR payload_ref = $3)
			   AND NOT EXISTS (
			       SELECT 1 FROM subscription_pauses p
			        WHERE p.subscription_id = $1 AND p.state IN ('scheduled', 'active')
			          AND $2 BETWEEN p.from_date AND p.to_date)`,
			subID, day, slot); err != nil {
			return fmt.Errorf("subscriptions: restore skipped delivery: %w", err)
		}
		return s.repo.RecordEvent(ctx, tx, subID, "unskipped", "", "",
			"customer", uuid.NullUUID{UUID: userID, Valid: true},
			mustJSON(map[string]any{"date": day.Format("2006-01-02"), "slot": slot}))
	})
}
