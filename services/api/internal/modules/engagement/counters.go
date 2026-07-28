// Package engagement owns view/like/share counts and the client event stream.
//
// Two rules govern everything here, and both come from
// docs/PHASE-2-MEDIA-AND-FEED.md §5:
//
//  1. Counters live in Redis; Postgres holds the truth but is never on the
//     increment path. `UPDATE reels SET view_count = view_count + 1` serializes
//     every viewer of a popular reel behind one row lock — and popularity is
//     exactly when that matters.
//
//  2. Events are batched by the client. One HTTP request per event turns a
//     30-second viewing session into a dozen requests per user, which is a load
//     profile no amount of server capacity makes sensible.
package engagement

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/marketplace/api/internal/modules/feed"
)

// ViewMinimumMS is what counts as a view.
//
// Two seconds watched, deduped per (user, reel, session). Writing the
// definition down in code rather than in a doc matters because "views" will be
// quoted to vendors in a dashboard and fed to the Phase 4 ranking model, and
// two teams using two definitions of the same word is how a ranking model
// learns the wrong thing.
//
// Completion rate is tracked separately and deliberately: "did they watch it"
// and "did they finish it" answer different questions, and conflating them will
// mislead the ranking work.
const ViewMinimumMS = 2_000

// counterFields mirror reel_counters. The order is fixed so a pipeline read can
// unpack positionally.
var counterFields = []string{"views", "likes", "comments", "shares", "saves"}

type Counters struct {
	client *redis.Client
	pool   *pgxpool.Pool
}

func NewCounters(client *redis.Client, pool *pgxpool.Pool) *Counters {
	return &Counters{client: client, pool: pool}
}

func counterKey(reelID uuid.UUID) string { return "reel:counters:" + reelID.String() }

// dirtyKey is the set of reels whose Redis counts have moved since the last
// flush. Flushing every reel that has ever been counted would make the flush
// job's cost proportional to the catalogue rather than to the traffic.
const dirtyKey = "reel:counters:dirty"

// Incr bumps one field and marks the reel dirty, in one round-trip.
func (c *Counters) Incr(ctx context.Context, reelID uuid.UUID, field string, by int64) error {
	if !validField(field) {
		return fmt.Errorf("engagement: unknown counter field %q", field)
	}
	pipe := c.client.TxPipeline()
	pipe.HIncrBy(ctx, counterKey(reelID), field, by)
	pipe.SAdd(ctx, dirtyKey, reelID.String())
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("engagement: increment %s: %w", field, err)
	}
	return nil
}

// Batch reads counters for a feed page.
//
// Falls back to Postgres per missing reel rather than returning zeroes: a reel
// that has not been touched since the last Redis restart legitimately has no
// hash, and showing 0 views on a reel with 40,000 is worse than one extra
// query.
func (c *Counters) Batch(ctx context.Context, reelIDs []uuid.UUID) (map[uuid.UUID]feed.Counters, error) {
	out := make(map[uuid.UUID]feed.Counters, len(reelIDs))
	if len(reelIDs) == 0 {
		return out, nil
	}

	pipe := c.client.Pipeline()
	cmds := make([]*redis.SliceCmd, len(reelIDs))
	for i, id := range reelIDs {
		cmds[i] = pipe.HMGet(ctx, counterKey(id), counterFields...)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("engagement: batch read: %w", err)
	}

	var missing []uuid.UUID
	for i, cmd := range cmds {
		vals, err := cmd.Result()
		if err != nil || allNil(vals) {
			missing = append(missing, reelIDs[i])
			continue
		}
		out[reelIDs[i]] = feed.Counters{
			Views:    parseCount(vals[0]),
			Likes:    parseCount(vals[1]),
			Comments: parseCount(vals[2]),
			Shares:   parseCount(vals[3]),
			Saves:    parseCount(vals[4]),
		}
	}

	if len(missing) > 0 && c.pool != nil {
		if err := c.fillFromPostgres(ctx, missing, out); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (c *Counters) fillFromPostgres(ctx context.Context, ids []uuid.UUID, out map[uuid.UUID]feed.Counters) error {
	rows, err := c.pool.Query(ctx, `
		SELECT reel_id, views, likes, comments, shares, saves
		  FROM reel_counters WHERE reel_id = ANY($1)`, ids)
	if err != nil {
		return fmt.Errorf("engagement: postgres fallback: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var c feed.Counters
		if err := rows.Scan(&id, &c.Views, &c.Likes, &c.Comments, &c.Shares, &c.Saves); err != nil {
			return fmt.Errorf("engagement: scan counters: %w", err)
		}
		out[id] = c
	}
	return rows.Err()
}

// Flush writes dirty counters back to Postgres.
//
// GETDEL-style semantics are deliberately NOT used: Redis holds the running
// total and Postgres receives an absolute value, so a flush that fails leaves
// Redis authoritative and the next flush corrects it. Draining the deltas out
// of Redis and losing them mid-write would silently lose counts, and nobody
// ever notices a view count that is 3% low.
func (c *Counters) Flush(ctx context.Context, batch int) (int, error) {
	if batch <= 0 {
		batch = 500
	}
	ids, err := c.client.SPopN(ctx, dirtyKey, int64(batch)).Result()
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("engagement: pop dirty set: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	flushed := 0
	for _, raw := range ids {
		reelID, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		vals, err := c.client.HMGet(ctx, counterKey(reelID), counterFields...).Result()
		if err != nil {
			// Put it back — an unflushed reel must not fall out of the dirty
			// set, or its counts stop reaching Postgres entirely.
			c.client.SAdd(ctx, dirtyKey, raw)
			continue
		}

		if _, err := c.pool.Exec(ctx, `
			INSERT INTO reel_counters (reel_id, views, likes, comments, shares, saves, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (reel_id) DO UPDATE
			   SET views = GREATEST(reel_counters.views, EXCLUDED.views),
			       likes = GREATEST(reel_counters.likes, EXCLUDED.likes),
			       comments = GREATEST(reel_counters.comments, EXCLUDED.comments),
			       shares = GREATEST(reel_counters.shares, EXCLUDED.shares),
			       saves = GREATEST(reel_counters.saves, EXCLUDED.saves),
			       updated_at = now()`,
			reelID, parseCount(vals[0]), parseCount(vals[1]), parseCount(vals[2]),
			parseCount(vals[3]), parseCount(vals[4]),
		); err != nil {
			c.client.SAdd(ctx, dirtyKey, raw)
			return flushed, fmt.Errorf("engagement: flush counters: %w", err)
		}
		flushed++
	}
	return flushed, nil
}

// FlushInterval is §5's ~10 seconds. Short enough that a vendor watching their
// dashboard sees movement, long enough that the flush is a batch rather than a
// write-through.
const FlushInterval = 10 * time.Second

func validField(f string) bool {
	for _, c := range counterFields {
		if c == f {
			return true
		}
	}
	return false
}

func allNil(vals []any) bool {
	for _, v := range vals {
		if v != nil {
			return false
		}
	}
	return true
}

func parseCount(v any) int64 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
