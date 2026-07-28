package comments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/moderation"
)

// ModerationTimeout bounds the synchronous classification call.
//
// Three seconds. Comments are moderated on the write path rather than in a
// worker because a comment that appears and then vanishes twenty seconds later
// is worse than one that takes three seconds to appear — the first reads as
// censorship, the second as a slow network.
//
// Past the timeout the comment lands pending and a human sees it. That is the
// deliberate cost of failing closed.
const ModerationTimeout = 3 * time.Second

// Counter is the engagement counter store, narrowed to what this package needs.
type Counter interface {
	Incr(ctx context.Context, reelID uuid.UUID, field string, by int64) error
}

// Staff authorises moderation actions.
//
// An interface rather than a *moderation.Service so this package does not
// depend on the whole moderation surface to ask one yes/no question.
type Staff interface {
	RequireStaff(ctx context.Context, userID uuid.UUID) error
}

type Service struct {
	pool      *pgxpool.Pool
	moderator moderation.TextModerator
	counters  Counter
	staff     Staff
}

func NewService(pool *pgxpool.Pool, moderator moderation.TextModerator, counters Counter, staff Staff) *Service {
	return &Service{pool: pool, moderator: moderator, counters: counters, staff: staff}
}

// Comment is one stored comment.
type Comment struct {
	ID         uuid.UUID
	ReelID     uuid.UUID
	AuthorID   uuid.UUID
	AuthorName string
	ParentID   *uuid.UUID
	Depth      int
	Body       string
	State      string
	Reason     string
	CreatedAt  time.Time
	Deleted    bool
	// ReplyCount is populated for root comments only.
	ReplyCount int
}

// Post writes a comment, moderating it first.
func (s *Service) Post(ctx context.Context, authorID, reelID uuid.UUID, parentID *uuid.UUID, body string) (*Comment, error) {
	if err := Validate(body); err != nil {
		return nil, err
	}
	body = Normalise(body)

	if err := s.checkReelAcceptsComments(ctx, reelID); err != nil {
		return nil, err
	}
	if err := s.checkRate(ctx, authorID); err != nil {
		return nil, err
	}

	state, reason, language := s.moderate(ctx, body)

	var c Comment
	var parent any
	if parentID != nil {
		parent = *parentID
	}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO reel_comments (reel_id, author_id, parent_id, body, language, state, reject_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, depth, created_at`,
		reelID, authorID, parent, body, nullableText(language), state, nullableText(reason),
	).Scan(&c.ID, &c.Depth, &c.CreatedAt)
	if err != nil {
		// The depth trigger raises for a reply-to-a-reply and for a reply on the
		// wrong reel. Both are client bugs, and both deserve a 422 that says
		// what happened rather than a 500 that says "database error".
		if isDepthViolation(err) {
			return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "parent_id", Code: "invalid",
				Message: "replies to replies are not supported",
			}}}
		}
		return nil, fmt.Errorf("comments: insert: %w", err)
	}

	c.ReelID, c.AuthorID, c.ParentID = reelID, authorID, parentID
	c.Body, c.State, c.Reason = body, state, reason

	if state == StatePublished && s.counters != nil {
		// Best-effort, and only for a comment that is actually visible. Counting
		// a pending comment would show a reel with 40 comments and a thread with
		// three, which reads as a broken app.
		_ = s.counters.Incr(ctx, reelID, "comments", 1)
	}
	return &c, nil
}

// moderate classifies the body, failing closed.
func (s *Service) moderate(ctx context.Context, body string) (state, reason, language string) {
	if s.moderator == nil {
		// No moderator wired. Pending, not published — the same rule the media
		// pipeline applies, and app.BuildMedia refuses to boot in production
		// without a real one.
		return StatePending, "", ""
	}

	// A budget of its own, so a slow classifier cannot consume the request's
	// entire deadline and take the insert with it.
	modCtx, cancel := context.WithTimeout(ctx, ModerationTimeout)
	defer cancel()

	res, err := s.moderator.ModerateText(modCtx, body)
	if err != nil {
		slog.WarnContext(ctx, "comment moderation unavailable, holding for review", "error", err)
	}
	state, reason = Decide(res, err)
	return state, reason, res.Language
}

func (s *Service) checkReelAcceptsComments(ctx context.Context, reelID uuid.UUID) error {
	var state string
	err := s.pool.QueryRow(ctx,
		`SELECT state::text FROM reels WHERE id = $1`, reelID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("comments: load reel: %w", err)
	}
	if state != "published" {
		// Includes a reel that has been taken down. Accepting comments on
		// removed content would mean a takedown leaves a live discussion of the
		// thing that was taken down.
		return httpx.ErrNotFound
	}
	return nil
}

func (s *Service) checkRate(ctx context.Context, authorID uuid.UUID) error {
	rows, err := s.pool.Query(ctx, `
		SELECT created_at
		  FROM reel_comments
		 WHERE author_id = $1
		   AND created_at > now() - make_interval(secs => $2)
		 ORDER BY created_at DESC
		 LIMIT $3`, authorID, DailyWindow.Seconds(), DailyLimit+1)
	if err != nil {
		return fmt.Errorf("comments: rate check: %w", err)
	}
	defer rows.Close()

	var recent []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return fmt.Errorf("comments: scan rate row: %w", err)
		}
		recent = append(recent, t)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("comments: rate check: %w", err)
	}

	// Rejected comments count. Someone whose comments are being refused is
	// exactly the account that should not be able to retry two hundred times.
	return CheckRate(recent, time.Now().UTC())
}

// List returns a reel's thread: published roots newest-first, with their
// replies inline.
//
// Two queries rather than one recursive CTE. The thread is at most two levels
// deep by construction, so recursion buys nothing, and the flat version is one
// a person can read six months from now.
func (s *Service) List(ctx context.Context, reelID uuid.UUID, before time.Time, limit int) ([]Comment, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var beforeArg any
	if !before.IsZero() {
		beforeArg = before
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.author_id, COALESCE(u.display_name, ''), c.body, c.created_at,
		       (SELECT count(*) FROM reel_comments rc
		         WHERE rc.parent_id = c.id
		           AND rc.state = 'published' AND rc.deleted_at IS NULL)
		  FROM reel_comments c
		  LEFT JOIN users u ON u.id = c.author_id
		 WHERE c.reel_id = $1
		   AND c.parent_id IS NULL
		   AND c.state = 'published'
		   AND c.deleted_at IS NULL
		   AND ($2::timestamptz IS NULL OR c.created_at < $2)
		 ORDER BY c.created_at DESC
		 LIMIT $3`, reelID, beforeArg, limit)
	if err != nil {
		return nil, fmt.Errorf("comments: list: %w", err)
	}
	defer rows.Close()

	var roots []Comment
	var rootIDs []uuid.UUID
	for rows.Next() {
		c := Comment{ReelID: reelID, State: StatePublished}
		if err := rows.Scan(&c.ID, &c.AuthorID, &c.AuthorName, &c.Body,
			&c.CreatedAt, &c.ReplyCount); err != nil {
			return nil, fmt.Errorf("comments: scan: %w", err)
		}
		roots = append(roots, c)
		rootIDs = append(rootIDs, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return roots, nil
	}

	replies, err := s.repliesFor(ctx, rootIDs)
	if err != nil {
		return nil, err
	}

	out := make([]Comment, 0, len(roots)*2)
	for _, r := range roots {
		out = append(out, r)
		out = append(out, replies[r.ID]...)
	}
	return out, nil
}

// repliesPerRoot caps how many replies come back inline. Past this the client
// asks for the thread; a root comment with 800 replies must not make one feed
// tap fetch 800 rows.
const repliesPerRoot = 3

func (s *Service) repliesFor(ctx context.Context, rootIDs []uuid.UUID) (map[uuid.UUID][]Comment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, parent_id, reel_id, author_id, COALESCE(display_name, ''), body, created_at
		  FROM (
		      SELECT c.id, c.parent_id, c.reel_id, c.author_id, u.display_name, c.body, c.created_at,
		             row_number() OVER (PARTITION BY c.parent_id ORDER BY c.created_at) AS rn
		        FROM reel_comments c
		        LEFT JOIN users u ON u.id = c.author_id
		       WHERE c.parent_id = ANY($1)
		         AND c.state = 'published'
		         AND c.deleted_at IS NULL
		  ) r
		 WHERE rn <= $2
		 ORDER BY parent_id, created_at`, rootIDs, repliesPerRoot)
	if err != nil {
		return nil, fmt.Errorf("comments: replies: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]Comment, len(rootIDs))
	for rows.Next() {
		c := Comment{Depth: 1, State: StatePublished}
		var parent uuid.UUID
		if err := rows.Scan(&c.ID, &parent, &c.ReelID, &c.AuthorID,
			&c.AuthorName, &c.Body, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("comments: scan reply: %w", err)
		}
		c.ParentID = &parent
		out[parent] = append(out[parent], c)
	}
	return out, rows.Err()
}

// Delete soft-deletes a comment.
//
// The author, the reel's author, and staff may all delete. The reel's author is
// on that list because a vendor cannot be told to accept whatever appears under
// their storefront while waiting for a moderator — and unlike a takedown, this
// one is reversible and recorded.
func (s *Service) Delete(ctx context.Context, userID, commentID uuid.UUID) error {
	var reelID uuid.UUID
	var wasPublished bool

	// The pre-update state is captured in a CTE rather than in RETURNING.
	// RETURNING reports the *new* row, so `state = 'published'` there would
	// always be false — and the comment counter would never come back down.
	err := s.pool.QueryRow(ctx, `
		WITH target AS (
		    SELECT c.id, c.reel_id, c.state
		      FROM reel_comments c
		     WHERE c.id = $1
		       AND c.deleted_at IS NULL
		       AND (
		           c.author_id = $2
		        OR EXISTS (
		               SELECT 1
		                 FROM reels r
		                 JOIN vendor_branches vb ON vb.id = r.author_id
		                 JOIN vendor_users vu ON vu.vendor_id = vb.vendor_id
		                WHERE r.id = c.reel_id
		                  AND r.author_type = 'vendor_branch'
		                  AND vu.user_id = $2
		                  -- NULL branch_id means every branch of the vendor,
		                  -- which is how a chain delegates. Matching only the
		                  -- named branch would lock a vendor's owner out of
		                  -- their own storefront.
		                  AND (vu.branch_id IS NULL OR vu.branch_id = vb.id))
		        OR EXISTS (SELECT 1 FROM platform_admins pa WHERE pa.user_id = $2)
		       )
		),
		removed AS (
		    UPDATE reel_comments
		       SET deleted_at = now(), state = 'removed'
		     WHERE id IN (SELECT id FROM target)
		    RETURNING id
		)
		SELECT t.reel_id, t.state = 'published'
		  FROM target t
		 WHERE EXISTS (SELECT 1 FROM removed)`,
		commentID, userID).Scan(&reelID, &wasPublished)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not found and not-yours are answered identically. Distinguishing them
		// would let anyone enumerate which comment ids exist.
		return httpx.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("comments: delete: %w", err)
	}

	if wasPublished && s.counters != nil {
		_ = s.counters.Incr(ctx, reelID, "comments", -1)
	}
	return nil
}

// Report files an abuse report against a comment.
func (s *Service) Report(ctx context.Context, reporterID, commentID uuid.UUID, reason, detail string) error {
	if reason == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
		}}}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO comment_reports (comment_id, reporter_id, reason, detail)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (comment_id, reporter_id) DO NOTHING`,
		commentID, reporterID, reason, nullableText(detail))
	if err != nil {
		return fmt.Errorf("comments: report: %w", err)
	}
	return nil
}

// --- moderation queue --------------------------------------------------------

// QueueItem is one comment awaiting a human.
type QueueItem struct {
	Comment
	Reports    int
	WaitingFor time.Duration
}

// Queue returns pending and reported comments, oldest first.
//
// Oldest first, always. A queue worked newest-first leaves its worst backlog
// permanently unworked, and the comments at the bottom are the ones that have
// been invisible to their authors the longest.
func (s *Service) Queue(ctx context.Context, moderatorID uuid.UUID, limit int) ([]QueueItem, error) {
	if s.staff == nil {
		return nil, httpx.ErrForbidden
	}
	if err := s.staff.RequireStaff(ctx, moderatorID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.reel_id, c.author_id, COALESCE(u.display_name, ''),
		       c.body, c.state, c.created_at,
		       COALESCE(rep.n, 0) AS reports,
		       EXTRACT(EPOCH FROM (now() - c.created_at))::float8 AS waiting_seconds
		  FROM reel_comments c
		  LEFT JOIN users u ON u.id = c.author_id
		  LEFT JOIN LATERAL (
		      SELECT count(*) AS n FROM comment_reports r
		       WHERE r.comment_id = c.id AND r.state = 'open'
		  ) rep ON true
		 WHERE c.deleted_at IS NULL
		   AND (c.state = 'pending' OR COALESCE(rep.n, 0) > 0)
		 ORDER BY c.created_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("comments: queue: %w", err)
	}
	defer rows.Close()

	var out []QueueItem
	for rows.Next() {
		var it QueueItem
		var waiting float64
		if err := rows.Scan(&it.ID, &it.ReelID, &it.AuthorID, &it.AuthorName,
			&it.Body, &it.State, &it.CreatedAt, &it.Reports, &waiting); err != nil {
			return nil, fmt.Errorf("comments: scan queue: %w", err)
		}
		it.WaitingFor = time.Duration(waiting * float64(time.Second))
		out = append(out, it)
	}
	return out, rows.Err()
}

// Decide records a moderator's ruling on a comment.
func (s *Service) Decide(ctx context.Context, moderatorID, commentID uuid.UUID, verdict moderation.Verdict, notes string) error {
	if s.staff == nil {
		return httpx.ErrForbidden
	}
	if err := s.staff.RequireStaff(ctx, moderatorID); err != nil {
		return err
	}

	var state string
	switch verdict {
	case moderation.VerdictApprove:
		state = StatePublished
	case moderation.VerdictReject:
		state = StateRejected
	default:
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "decision", Code: "invalid",
			Message: "a moderator decides approve or reject; review is where it already is",
		}}}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("comments: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Read-then-write inside the transaction, with the row locked. The previous
	// state decides whether the comment counter moves, and reading it from
	// RETURNING would report the state this statement just set.
	var reelID uuid.UUID
	var previous string
	err = tx.QueryRow(ctx, `
		SELECT reel_id, state
		  FROM reel_comments
		 WHERE id = $1 AND deleted_at IS NULL
		   FOR UPDATE`, commentID).Scan(&reelID, &previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("comments: load comment: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE reel_comments
		   SET state = $2,
		       reject_reason = CASE WHEN $2 = 'rejected' THEN $3 ELSE NULL END
		 WHERE id = $1`,
		commentID, state, nullableText(notes)); err != nil {
		return fmt.Errorf("comments: decide: %w", err)
	}

	// Every open report on this comment is resolved by the same ruling. Leaving
	// them open would mean the next moderator sees a comment that has already
	// been judged and judges it again.
	if _, err := tx.Exec(ctx, `
		UPDATE comment_reports
		   SET state = CASE WHEN $2 = 'rejected' THEN 'upheld' ELSE 'dismissed' END,
		       resolved_by = $3, resolved_at = now()
		 WHERE comment_id = $1 AND state = 'open'`,
		commentID, state, moderatorID); err != nil {
		return fmt.Errorf("comments: resolve reports: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("comments: commit: %w", err)
	}

	// The counter follows visibility in both directions. A moderator rejecting a
	// comment that was already live has to take it off the count, or the badge
	// on the card keeps promising a comment nobody can find.
	if s.counters != nil {
		switch {
		case state == StatePublished && previous != StatePublished:
			_ = s.counters.Incr(ctx, reelID, "comments", 1)
		case state != StatePublished && previous == StatePublished:
			_ = s.counters.Incr(ctx, reelID, "comments", -1)
		}
	}
	return nil
}

func isDepthViolation(err error) bool {
	// The trigger raises a plain exception, which pgx surfaces as a
	// *pgconn.PgError with SQLSTATE P0001 (raise_exception).
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "P0001"
}

func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
