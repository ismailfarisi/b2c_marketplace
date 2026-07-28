package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// SeenStore tracks which reels a viewer has already been shown.
//
// Redis, not a join. §4.2 is explicit about why: an impressions table grows
// without bound and a NOT EXISTS against it degrades as it does — and it
// degrades on the single hottest read path in the product. The table still
// exists for analytics; it is simply not on the serving path.
//
// An interface because an anonymous viewer has no durable identity to key on
// and because a Redis outage must degrade the feed to "shows repeats", never to
// "returns 500". The feed is the front door; it stays up.
type SeenStore interface {
	Recent(ctx context.Context, viewerKey string, limit int) ([]uuid.UUID, error)
	Mark(ctx context.Context, viewerKey string, ids []uuid.UUID) error
}

// SeenTTL is how long an impression suppresses a reel.
//
// Seven days, not forever. A user who has seen everything in their area would
// otherwise get an empty feed permanently, which is a worse failure than
// occasionally re-showing a good reel — and in a launch district with 50 reels
// it is the normal case, not an edge case.
const SeenTTL = 7 * 24 * time.Hour

// seenCap bounds the set. Without it, the ID array passed to Postgres grows
// unboundedly and the NOT (id = ANY($3)) predicate becomes the expensive part
// of a query written specifically to be cheap.
const seenCap = 500

type RedisSeenStore struct {
	client *redis.Client
}

func NewRedisSeenStore(client *redis.Client) *RedisSeenStore {
	return &RedisSeenStore{client: client}
}

func seenKey(viewerKey string) string { return "feed:seen:" + viewerKey }

// Recent returns the most recently seen IDs, newest first.
//
// A capped list rather than a set: the ordering is what lets the cap evict the
// oldest impressions, and a plain set gives no way to choose which 500 to keep.
func (s *RedisSeenStore) Recent(ctx context.Context, viewerKey string, limit int) ([]uuid.UUID, error) {
	if viewerKey == "" {
		return nil, nil
	}
	if limit <= 0 || limit > seenCap {
		limit = seenCap
	}
	vals, err := s.client.LRange(ctx, seenKey(viewerKey), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("feed: read seen set: %w", err)
	}

	out := make([]uuid.UUID, 0, len(vals))
	for _, v := range vals {
		id, err := uuid.Parse(v)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *RedisSeenStore) Mark(ctx context.Context, viewerKey string, ids []uuid.UUID) error {
	if viewerKey == "" || len(ids) == 0 {
		return nil
	}
	members := make([]any, len(ids))
	for i, id := range ids {
		members[i] = id.String()
	}

	key := seenKey(viewerKey)
	pipe := s.client.TxPipeline()
	pipe.LPush(ctx, key, members...)
	pipe.LTrim(ctx, key, 0, seenCap-1)
	pipe.Expire(ctx, key, SeenTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("feed: mark seen: %w", err)
	}
	return nil
}

// NoopSeenStore is what the feed falls back to when Redis is not configured.
//
// The consequence is stated rather than hidden: the feed repeats itself on
// every request. That is a degraded feed, not a broken one, and it is the right
// trade — the alternative is the front door returning 503 because a cache is
// down.
type NoopSeenStore struct{}

func (NoopSeenStore) Recent(context.Context, string, int) ([]uuid.UUID, error) { return nil, nil }
func (NoopSeenStore) Mark(context.Context, string, []uuid.UUID) error          { return nil }
