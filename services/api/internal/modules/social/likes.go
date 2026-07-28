package social

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Counter is the engagement counter store, as much of it as likes need.
//
// Narrow on purpose. This package should not be able to increment views.
type Counter interface {
	Incr(ctx context.Context, reelID uuid.UUID, field string, by int64) error
}

// Likes owns the like relation.
//
// Two stores, two questions, and keeping them straight is the whole design:
//
//   - "how many likes does this reel have" is a badge on a card, answered from
//     the Redis counter, and being 3% low for ten seconds costs nothing.
//   - "have *I* liked this" is a filled heart in this viewer's UI, answered
//     from reel_likes, and being wrong is immediately visible to the one person
//     who knows the answer.
//
// Serving the second question from a counter is the bug that produces a heart
// that un-fills when you scroll away and back.
type Likes struct {
	pool     *pgxpool.Pool
	counters Counter
}

func NewLikes(pool *pgxpool.Pool, counters Counter) *Likes {
	return &Likes{pool: pool, counters: counters}
}

// Like records a like and returns whether this call was the one that created it.
//
// Idempotent. A double-tap, a retry on a flaky connection and a client that
// optimistically fires twice must all leave exactly one row and move the
// counter exactly once — which is why the counter increment is conditional on
// the insert having actually inserted.
func (l *Likes) Like(ctx context.Context, userID, reelID uuid.UUID) (bool, error) {
	tag, err := l.pool.Exec(ctx, `
		INSERT INTO reel_likes (user_id, reel_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, reel_id) DO NOTHING`, userID, reelID)
	if err != nil {
		return false, fmt.Errorf("social: like: %w", err)
	}

	created := tag.RowsAffected() > 0
	if created && l.counters != nil {
		// Best-effort: the row is the truth and the counter is the decoration.
		// Failing the request because a cache write failed would turn a Redis
		// blip into a like button that appears broken.
		_ = l.counters.Incr(ctx, reelID, "likes", 1)
	}
	return created, nil
}

// Unlike removes a like, reporting whether there was one.
func (l *Likes) Unlike(ctx context.Context, userID, reelID uuid.UUID) (bool, error) {
	tag, err := l.pool.Exec(ctx,
		`DELETE FROM reel_likes WHERE user_id = $1 AND reel_id = $2`, userID, reelID)
	if err != nil {
		return false, fmt.Errorf("social: unlike: %w", err)
	}

	removed := tag.RowsAffected() > 0
	if removed && l.counters != nil {
		_ = l.counters.Incr(ctx, reelID, "likes", -1)
	}
	return removed, nil
}

// Viewer state for a feed page: which of these reels has this viewer liked or
// saved, and which of these authors do they follow.
//
// One query for the whole page rather than three per reel. On a 20-item feed
// page the naive version is 60 round-trips on the hottest read path in the
// product, and it is the kind of N+1 that only shows up under real page sizes.
type ViewerState struct {
	Liked    map[uuid.UUID]bool
	Saved    map[uuid.UUID]bool
	Followed map[uuid.UUID]bool
}

func (l *Likes) StateFor(ctx context.Context, userID uuid.NullUUID, reelIDs []uuid.UUID) (ViewerState, error) {
	state := ViewerState{
		Liked:    map[uuid.UUID]bool{},
		Saved:    map[uuid.UUID]bool{},
		Followed: map[uuid.UUID]bool{},
	}
	// An anonymous viewer has liked nothing and follows nobody. Returning empty
	// maps rather than nil means callers index without checking.
	if !userID.Valid || len(reelIDs) == 0 {
		return state, nil
	}

	rows, err := l.pool.Query(ctx, `
		SELECT r.id,
		       l.user_id IS NOT NULL AS liked,
		       s.user_id IS NOT NULL AS saved,
		       f.follower_id IS NOT NULL AS followed
		  FROM reels r
		  LEFT JOIN reel_likes l ON l.reel_id = r.id AND l.user_id = $1
		  LEFT JOIN reel_saves s ON s.reel_id = r.id AND s.user_id = $1
		  LEFT JOIN follows    f ON f.follower_id = $1
		                        AND f.followee_type = r.author_type
		                        AND f.followee_id = r.author_id
		 WHERE r.id = ANY($2)`, userID.UUID, reelIDs)
	if err != nil {
		return state, fmt.Errorf("social: viewer state: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var liked, saved, followed bool
		if err := rows.Scan(&id, &liked, &saved, &followed); err != nil {
			return state, fmt.Errorf("social: scan viewer state: %w", err)
		}
		state.Liked[id] = liked
		state.Saved[id] = saved
		state.Followed[id] = followed
	}
	return state, rows.Err()
}

// Note on what is deliberately *not* here: the feed's "reels by authors I
// follow" candidate source. It reads `follows` directly inside the retrieval
// query rather than round-tripping a list of author ids through this package,
// because a viewer who follows two thousand vendors would otherwise turn one
// feed request into a two-thousand-element IN list on the hottest read path.
