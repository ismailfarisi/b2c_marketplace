// Package actions owns the CTAs on a reel and the record of them being pressed.
//
// It is the storage half of the `action` seam. Everything vertical-specific
// stays behind a registered handler; this package only ever moves the triple
// (kind, target_type, target_id) around. If a query in here ever needs to know
// what a laundry offering is, the seam has leaked.
package actions

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/seam/action"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Input is one CTA as an author specifies it.
type Input struct {
	Kind       action.Kind
	TargetType action.TargetType
	TargetID   *uuid.UUID
	TargetURL  string
	Label      string
	LabelAr    string
}

// Replace sets a reel's full CTA list.
//
// Replace rather than append: position is a unique key, and an append API turns
// every reorder into a client-side diff that gets it wrong. Deleting and
// reinserting inside one transaction is simpler and cannot leave a gap.
//
// The clicks survive — action_clicks references reel_actions with ON DELETE
// CASCADE, so this WOULD destroy click history. It does not, because reordering
// is refused once a reel is published (see Service.Set).
func (r *Repository) Replace(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, inputs []Input) error {
	if _, err := tx.Exec(ctx, `DELETE FROM reel_actions WHERE reel_id = $1`, reelID); err != nil {
		return fmt.Errorf("clear reel actions: %w", err)
	}

	for i, in := range inputs {
		var targetID any
		if in.TargetID != nil {
			targetID = *in.TargetID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO reel_actions
			    (reel_id, kind, target_type, target_id, target_url, position, label, label_ar)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			reelID, string(in.Kind), string(in.TargetType), targetID,
			nullableText(in.TargetURL), i,
			nullableText(in.Label), nullableText(in.LabelAr),
		); err != nil {
			return fmt.Errorf("insert reel action %d: %w", i, err)
		}
	}
	return nil
}

// Stored is a persisted CTA. It is the seam's own type, not a wrapper around
// it: anything this package added would be a field handlers cannot see, and a
// field handlers cannot see is a field that does not exist as far as the seam
// is concerned.
type Stored = action.Action

// ListForReels fetches every CTA for a page of reels in one query.
//
// One query for the page, not one per reel. A feed page is 20 reels and the
// per-reel version is a 20-round-trip N+1 on the hottest read path in the
// product — the exact thing action.Registry.ResolveBatch exists to avoid, and
// it would be undone here before it ever got the chance.
func (r *Repository) ListForReels(ctx context.Context, reelIDs []uuid.UUID) (map[uuid.UUID][]Stored, error) {
	out := make(map[uuid.UUID][]Stored, len(reelIDs))
	if len(reelIDs) == 0 {
		return out, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, reel_id, kind, target_type,
		       COALESCE(target_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       COALESCE(target_url, ''), position,
		       COALESCE(label, ''), COALESCE(label_ar, '')
		  FROM reel_actions
		 WHERE reel_id = ANY($1)
		 ORDER BY reel_id, position`, reelIDs)
	if err != nil {
		return nil, fmt.Errorf("list reel actions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var s Stored
		var kind, targetType string
		if err := rows.Scan(&s.ID, &s.ReelID, &kind, &targetType, &s.TargetID,
			&s.TargetURL, &s.Position, &s.Label, &s.LabelAr); err != nil {
			return nil, fmt.Errorf("scan reel action: %w", err)
		}
		s.Kind = action.Kind(kind)
		s.TargetType = action.TargetType(targetType)
		out[s.ReelID] = append(out[s.ReelID], s)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, actionID uuid.UUID) (Stored, error) {
	var s Stored
	var kind, targetType string
	err := r.pool.QueryRow(ctx, `
		SELECT id, reel_id, kind, target_type,
		       COALESCE(target_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       COALESCE(target_url, ''), position,
		       COALESCE(label, ''), COALESCE(label_ar, '')
		  FROM reel_actions WHERE id = $1`, actionID,
	).Scan(&s.ID, &s.ReelID, &kind, &targetType, &s.TargetID, &s.TargetURL,
		&s.Position, &s.Label, &s.LabelAr)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stored{}, httpx.ErrNotFound
	}
	if err != nil {
		return Stored{}, fmt.Errorf("get reel action: %w", err)
	}
	s.Kind = action.Kind(kind)
	s.TargetType = action.TargetType(targetType)
	return s, nil
}

// ClickInput is one recorded CTA press.
type ClickInput struct {
	ActionID  uuid.UUID
	ReelID    uuid.UUID
	UserID    uuid.NullUUID
	ViewerKey string
	SessionID string
	Surface   string
	WatchedMS int
}

// RecordClick appends to the evidence log.
//
// Append-only and never deduplicated. A viewer who presses the CTA three times
// pressed it three times; collapsing that would lose the fact that they
// hesitated, which is a genuine signal for Phase 4 and costs one row to keep.
// Attribution reads the most recent one, so duplicates change no number.
func (r *Repository) RecordClick(ctx context.Context, in ClickInput) (int64, error) {
	var userID any
	if in.UserID.Valid {
		userID = in.UserID.UUID
	}
	if in.Surface == "" {
		in.Surface = "feed"
	}

	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO action_clicks
		    (action_id, reel_id, user_id, viewer_key, session_id, surface, watched_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		in.ActionID, in.ReelID, userID, in.ViewerKey,
		nullableText(in.SessionID), in.Surface, nullableInt(in.WatchedMS),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("record action click: %w", err)
	}
	return id, nil
}

// RecordImpressions writes the feed's denominator.
//
// Phase 2 kept the seen-set in Redis and out of the serving path, which was
// right for serving and wrong for measurement: without a durable impression
// count there is no denominator, and "conversion rate" quietly becomes
// orders-per-click — the flattering one.
//
// Best-effort by design. The caller logs and continues on error: losing an
// impression row degrades a metric, and failing the feed request to protect a
// metric would be the wrong trade on the product's front door.
func (r *Repository) RecordImpressions(ctx context.Context, viewerKey string, userID uuid.NullUUID, surface string, reelIDs []uuid.UUID) error {
	if viewerKey == "" || len(reelIDs) == 0 {
		return nil
	}
	var uid any
	if userID.Valid {
		uid = userID.UUID
	}

	rows := make([][]any, 0, len(reelIDs))
	for i, id := range reelIDs {
		// Position is 1-based and is the rank within the page. A conversion
		// rate that ignores position compares a reel shown first against one
		// that was barely shown at all.
		rows = append(rows, []any{id, viewerKey, uid, surface, i + 1})
	}

	_, err := r.pool.CopyFrom(ctx,
		pgx.Identifier{"feed_impressions"},
		[]string{"reel_id", "viewer_key", "user_id", "surface", "position"},
		pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("record impressions: %w", err)
	}
	return nil
}

// --- social ------------------------------------------------------------------

func (r *Repository) Follow(ctx context.Context, followerID uuid.UUID, followeeType string, followeeID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO follows (follower_id, followee_type, followee_id)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		followerID, followeeType, followeeID)
	if err != nil {
		return fmt.Errorf("follow: %w", err)
	}
	return nil
}

func (r *Repository) Unfollow(ctx context.Context, followerID uuid.UUID, followeeType string, followeeID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM follows
		 WHERE follower_id = $1 AND followee_type = $2 AND followee_id = $3`,
		followerID, followeeType, followeeID)
	if err != nil {
		return fmt.Errorf("unfollow: %w", err)
	}
	return nil
}

// IsFollowing answers the question the feed asks for every author on a page, so
// it takes the whole page at once.
func (r *Repository) FollowingSet(ctx context.Context, followerID uuid.UUID, followeeIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := make(map[uuid.UUID]bool, len(followeeIDs))
	if len(followeeIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT followee_id FROM follows
		 WHERE follower_id = $1 AND followee_id = ANY($2)`, followerID, followeeIDs)
	if err != nil {
		return nil, fmt.Errorf("following set: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (r *Repository) Save(ctx context.Context, userID, reelID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO reel_saves (user_id, reel_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, userID, reelID)
	if err != nil {
		return fmt.Errorf("save reel: %w", err)
	}
	return nil
}

func (r *Repository) Unsave(ctx context.Context, userID, reelID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM reel_saves WHERE user_id = $1 AND reel_id = $2`, userID, reelID)
	if err != nil {
		return fmt.Errorf("unsave reel: %w", err)
	}
	return nil
}

// SavedReelIDs is the user's saved list, newest first.
func (r *Repository) SavedReelIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT reel_id FROM reel_saves
		 WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("saved reels: %w", err)
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

// ReelState is the subset of a reel this package needs to authorise a change.
type ReelState struct {
	AuthorType string
	AuthorID   uuid.UUID
	State      string
}

func (r *Repository) ReelState(ctx context.Context, reelID uuid.UUID) (ReelState, error) {
	var s ReelState
	err := r.pool.QueryRow(ctx,
		`SELECT author_type, author_id, state::text FROM reels WHERE id = $1`, reelID,
	).Scan(&s.AuthorType, &s.AuthorID, &s.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReelState{}, httpx.ErrNotFound
	}
	if err != nil {
		return ReelState{}, fmt.Errorf("reel state: %w", err)
	}
	return s, nil
}

func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(i int) any {
	if i == 0 {
		return nil
	}
	return i
}
