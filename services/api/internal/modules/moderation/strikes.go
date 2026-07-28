package moderation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

// Strikes and appeals — the Phase 8 half of PLAN-VIDEO-FIRST §6.
//
// That section lists four requirements. Auto-moderation, a review queue with
// local context and in-feed reporting all shipped in Phase 2. "Author strikes,
// and appeals" did not, and they are the two that turn moderation from a
// sequence of per-item decisions into something an author experiences as a
// policy.
//
// The distinction is not cosmetic. Removing ten reels from one author without
// a strike record means the eleventh is judged exactly like the first — the
// system has no memory, so a repeat offender is indistinguishable from someone
// having a bad week. And a platform that removes content with no route to
// challenge it is one the app stores treat as a policy violation rather than a
// product choice.

// Severity is how bad one violation was.
type Severity string

const (
	Minor  Severity = "minor"
	Major  Severity = "major"
	Severe Severity = "severe"
)

// Points is the weight a severity carries on the ladder.
//
// Gapped, and the gaps had to be widened once: the first attempt used 1/3/8,
// which made three minor caption violations arithmetically identical to one
// major one. TestSeveritiesAreDistinguishable caught it. That equality is not a
// rounding detail — it decides whether an author who is careless three times is
// treated like one who did something seriously wrong once, and those need
// different answers.
func (s Severity) Points() int {
	switch s {
	case Severe:
		return 10
	case Major:
		return 4
	case Minor:
		return 1
	default:
		// An unrecognised severity takes the lightest weight. Failing toward
		// leniency here is right: the alternative is a typo in a policy code
		// banning someone.
		return 1
	}
}

// Lifetime is how long a strike counts for.
//
// Strikes expire, and this is the design decision most worth defending. A
// permanent record makes the ladder a one-way ratchet: an author who posted
// something wrong once, two years ago, is treated for the rest of their life
// like one who did it last week — and on a marketplace, "for the rest of their
// life" means a vendor's business.
//
// Severity buys duration rather than only weight, because those are different
// statements. A minor caption violation should be forgotten in a month; a
// severe one should shape how the next year is judged.
func (s Severity) Lifetime() time.Duration {
	switch s {
	case Severe:
		return 365 * 24 * time.Hour
	case Major:
		return 180 * 24 * time.Hour
	default:
		return 60 * 24 * time.Hour
	}
}

// Enforcement is what an author's accumulated points mean today.
type Enforcement string

const (
	// None — nothing has happened, or everything has expired.
	None Enforcement = "none"
	// Warned — the author is told; nothing is restricted.
	Warned Enforcement = "warned"
	// ReviewAll — the author keeps posting, and every upload goes to a human
	// before publish rather than through the auto-approve path.
	ReviewAll Enforcement = "review_all"
	// Suspended — no new content for a period. Existing content stays up.
	Suspended Enforcement = "suspended"
	// Banned — the account is closed to authoring.
	Banned Enforcement = "banned"
)

// Thresholds, in points.
//
// One severe violation (8) reaches Suspended on its own and does not reach
// Banned. That is deliberate: a single decision by a single moderator must not
// be able to end a business. Banning requires either a second severe violation
// or a sustained pattern, which means at least two people have looked.
const (
	WarnAt      = 1
	ReviewAllAt = 4
	SuspendAt   = 8
	BanAt       = 14
)

// EnforcementFor maps points to a consequence.
func EnforcementFor(points int) Enforcement {
	switch {
	case points >= BanAt:
		return Banned
	case points >= SuspendAt:
		return Suspended
	case points >= ReviewAllAt:
		return ReviewAll
	case points >= WarnAt:
		return Warned
	default:
		return None
	}
}

// SuspensionLength is how long a suspension runs at a given point total.
//
// Escalating, and capped. An unbounded formula produces a nine-month suspension
// that is a ban nobody decided to impose — if the answer is a ban, that should
// be the decision rather than the arithmetic.
func SuspensionLength(points int) time.Duration {
	switch {
	case points >= 12:
		return 30 * 24 * time.Hour
	case points >= 10:
		return 14 * 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

// StrikeState is an author's standing.
type StrikeState struct {
	Points      int
	Active      int
	Enforcement Enforcement
	// ExpiresAt is when the *next* strike falls off, which is when the
	// enforcement level may drop. Shown to the author, because "you are
	// restricted" with no end date is indistinguishable from a ban.
	NextExpiry time.Time
}

type StrikeService struct{ pool *pgxpool.Pool }

func NewStrikeService(pool *pgxpool.Pool) *StrikeService { return &StrikeService{pool: pool} }

// Standing computes an author's current state from unexpired, unvoided strikes.
//
// Derived on read rather than stored on the author, for the same reason
// `kyc.Eligible` is: what changes an author's standing is *a date passing*, and
// no write happens when a strike expires. A stored counter would keep an author
// restricted for as long as nobody ran the job that decrements it.
func (s *StrikeService) Standing(ctx context.Context, authorType string, authorID uuid.UUID) (StrikeState, error) {
	return standing(ctx, s.pool, authorType, authorID)
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func standing(ctx context.Context, q querier, authorType string, authorID uuid.UUID) (StrikeState, error) {
	rows, err := q.Query(ctx, `
		SELECT severity, expires_at FROM author_strikes
		 WHERE author_type = $1 AND author_id = $2
		   AND voided_at IS NULL AND expires_at > now()`, authorType, authorID)
	if err != nil {
		return StrikeState{}, fmt.Errorf("moderation: load strikes: %w", err)
	}
	defer rows.Close()

	var st StrikeState
	for rows.Next() {
		var sev Severity
		var expires time.Time
		if err := rows.Scan(&sev, &expires); err != nil {
			return st, err
		}
		st.Active++
		st.Points += sev.Points()
		if st.NextExpiry.IsZero() || expires.Before(st.NextExpiry) {
			st.NextExpiry = expires
		}
	}
	if err := rows.Err(); err != nil {
		return st, err
	}

	st.Enforcement = EnforcementFor(st.Points)
	return st, nil
}

// Strike records a violation and returns the author's new standing.
type Strike struct {
	AuthorType  string
	AuthorID    uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	Severity    Severity
	Reason      string
	PolicyCode  string
	IssuedBy    uuid.UUID
}

// Issue records a strike.
//
// One strike per subject, enforced by the caller rather than by a constraint:
// re-reviewing a reel after an appeal must be able to leave the original strike
// in place, and a unique index would make that an error rather than a decision.
func (s *StrikeService) Issue(ctx context.Context, in Strike) (StrikeState, error) {
	if in.Reason == "" || in.PolicyCode == "" {
		return StrikeState{}, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "a strike with no policy reference cannot be appealed against or audited",
		}}}
	}

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO author_strikes
		    (author_type, author_id, subject_type, subject_id, severity,
		     reason, policy_code, issued_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now() + $9::interval)`,
		in.AuthorType, in.AuthorID, in.SubjectType, in.SubjectID, string(in.Severity),
		in.Reason, in.PolicyCode, in.IssuedBy,
		fmt.Sprintf("%d hours", int(in.Severity.Lifetime().Hours()))); err != nil {
		return StrikeState{}, fmt.Errorf("moderation: issue strike: %w", err)
	}

	return s.Standing(ctx, in.AuthorType, in.AuthorID)
}

// Void reverses a strike, normally because an appeal succeeded.
//
// The row stays. A moderation function that deletes its own reversed decisions
// cannot be reviewed — the rate at which appeals succeed is the single most
// useful measure of whether the policy is being applied correctly, and it is
// unmeasurable if the successes disappear.
func (s *StrikeService) Void(ctx context.Context, strikeID uuid.UUID, reason string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE author_strikes SET voided_at = now(), voided_reason = $2
		 WHERE id = $1 AND voided_at IS NULL`, strikeID, reason)
	if err != nil {
		return fmt.Errorf("moderation: void strike: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

// --- appeals ----------------------------------------------------------------

// AppealWindow is how long an author has to challenge a decision, and
// AppealSLA is how long the platform then has to answer.
//
// Both are needed. A window with no SLA is a way of saying no slowly; an SLA
// with no window means a decision is never final.
const (
	AppealWindow = 30 * 24 * time.Hour
	AppealSLA    = 7 * 24 * time.Hour
)

// OpenAppeal records a challenge.
func (s *StrikeService) OpenAppeal(ctx context.Context, in Appeal) (uuid.UUID, error) {
	if len(in.Statement) < 10 {
		return uuid.Nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "statement", Code: "too_short",
			Message: "tell us what you think we got wrong",
		}}}
	}

	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO content_appeals
		    (subject_type, subject_id, appellant_id, statement, original_reviewer_id, due_at)
		VALUES ($1, $2, $3, $4, $5, now() + $6::interval)
		RETURNING id`,
		in.SubjectType, in.SubjectID, in.AppellantID, in.Statement,
		nullUUID(in.OriginalReviewerID), fmt.Sprintf("%d hours", int(AppealSLA.Hours()))).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("moderation: open appeal: %w", err)
	}
	return id, nil
}

type Appeal struct {
	SubjectType        string
	SubjectID          uuid.UUID
	AppellantID        uuid.UUID
	Statement          string
	OriginalReviewerID uuid.UUID
}

// DecideAppeal upholds or overturns.
//
// The self-review check is in the database as a CHECK constraint and is
// repeated here for the error message. In a moderation team of four the person
// who made the original decision is the obvious next reviewer most of the time,
// and an appeal reviewed by them is not an appeal — it is the same decision
// taken twice with extra steps.
func (s *StrikeService) DecideAppeal(ctx context.Context, appealID, reviewerID uuid.UUID, overturn bool, note string) error {
	var original uuid.NullUUID
	if err := s.pool.QueryRow(ctx,
		`SELECT original_reviewer_id FROM content_appeals WHERE id = $1 AND status IN ('open', 'in_review')`,
		appealID).Scan(&original); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		return fmt.Errorf("moderation: load appeal: %w", err)
	}
	if original.Valid && original.UUID == reviewerID {
		return fmt.Errorf("%w: an appeal cannot be reviewed by the person whose "+
			"decision is being appealed", httpx.ErrForbidden)
	}

	status := "upheld"
	if overturn {
		status = "overturned"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE content_appeals
		   SET status = $2, reviewer_id = $3, reviewed_at = now(), outcome_note = nullif($4, '')
		 WHERE id = $1 AND status IN ('open', 'in_review')`,
		appealID, status, reviewerID, note)
	if err != nil {
		return fmt.Errorf("moderation: decide appeal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: this appeal is already decided", httpx.ErrConflict)
	}
	return nil
}

// OverdueAppeals lists appeals past their SLA.
//
// A queue endpoint rather than an alert, because the response to a breached
// appeal SLA is to review it, not to page someone. What makes it matter is that
// it is visible at all: the failure mode of an appeals process is not that
// appeals are decided wrongly, it is that they are never decided.
func (s *StrikeService) OverdueAppeals(ctx context.Context, limit int) ([]OverdueAppeal, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, subject_type, subject_id, appellant_id, created_at, due_at
		  FROM content_appeals
		 WHERE status IN ('open', 'in_review') AND due_at < now()
		 ORDER BY due_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("moderation: overdue appeals: %w", err)
	}
	defer rows.Close()

	var out []OverdueAppeal
	for rows.Next() {
		var a OverdueAppeal
		if err := rows.Scan(&a.ID, &a.SubjectType, &a.SubjectID, &a.AppellantID,
			&a.CreatedAt, &a.DueAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type OverdueAppeal struct {
	ID          uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	AppellantID uuid.UUID
	CreatedAt   time.Time
	DueAt       time.Time
}

func nullUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// --- queue claims -----------------------------------------------------------

// ClaimTTL is how long a moderator holds an item before it returns to the
// queue.
//
// Fifteen minutes. Long enough to watch a reel, read a caption and decide;
// short enough that a closed laptop does not remove an item from the queue for
// the rest of the shift.
const ClaimTTL = 15 * time.Minute

// Claim takes an item out of a queue for one reviewer.
//
// Two moderators judging the same reel is wasted capacity. Two moderators
// reaching *different* verdicts on the same reel is an inconsistency the author
// sees, and it is the one that costs trust.
func (s *StrikeService) Claim(ctx context.Context, queue string, subjectID, reviewerID uuid.UUID) error {
	// Expire stale claims first, so an abandoned item is reclaimable without a
	// sweeper job having run. Doing it lazily here means the queue is correct
	// on read rather than correct on schedule.
	if _, err := s.pool.Exec(ctx, `
		UPDATE moderation_assignments
		   SET released_at = now(), resolution = 'expired'
		 WHERE queue = $1 AND subject_id = $2 AND released_at IS NULL AND expires_at < now()`,
		queue, subjectID); err != nil {
		return fmt.Errorf("moderation: expire claims: %w", err)
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO moderation_assignments (queue, subject_id, reviewer_id, expires_at)
		VALUES ($1, $2, $3, now() + $4::interval)`,
		queue, subjectID, reviewerID, fmt.Sprintf("%d minutes", int(ClaimTTL.Minutes())))
	if err != nil {
		return fmt.Errorf("%w: another reviewer is looking at this", httpx.ErrConflict)
	}
	return nil
}

// Release hands an item back, deciding or abandoning it.
func (s *StrikeService) Release(ctx context.Context, queue string, subjectID uuid.UUID, decided bool) error {
	resolution := "released"
	if decided {
		resolution = "decided"
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE moderation_assignments SET released_at = now(), resolution = $3
		 WHERE queue = $1 AND subject_id = $2 AND released_at IS NULL`,
		queue, subjectID, resolution)
	if err != nil {
		return fmt.Errorf("moderation: release claim: %w", err)
	}
	return nil
}
