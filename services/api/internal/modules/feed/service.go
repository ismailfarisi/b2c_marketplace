package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

// Item is one reel as the feed serves it.
//
// Deliberately narrow. The author-facing reel carries failure codes, probe
// output and moderation state; none of that belongs in a response served to
// every customer, and the way to guarantee it never leaks is for the serving
// type not to have the fields.
type Item struct {
	ID          uuid.UUID
	AuthorType  string
	AuthorID    uuid.UUID
	AuthorName  string
	SubjectType *string
	SubjectID   *uuid.UUID
	CategoryID  *uuid.UUID
	Caption     string
	DurationMS  int
	PosterKey   string
	PreviewKey  string
	LQIP        string
	Renditions  []Rendition
	Counters    Counters
	PublishedAt time.Time
	Score       float64
}

type Rendition struct {
	Label       string
	Width       int
	Height      int
	BitrateKbps int
	PlaylistURL string
}

type Counters struct {
	Views    int64
	Likes    int64
	Comments int64
	Shares   int64
	Saves    int64
}

// Query is a feed request.
type Query struct {
	// Point is the viewer's location. Required: the whole product proposition
	// is local discovery, and a feed served without a location is a different
	// product that we have not designed.
	Lat, Lng float64
	Category *uuid.UUID
	Cursor   Cursor
	Limit    int

	// ViewerID is empty for anonymous browsing, which is supported: putting a
	// login wall in front of the front door would be fatal to it.
	ViewerID uuid.NullUUID
	// ViewerKey namespaces the seen-set. Falls back to a device identifier for
	// anonymous viewers.
	ViewerKey string
}

type Page struct {
	Items      []Item
	NextCursor string
}

// CounterSource reads engagement counts from the hot store.
type CounterSource interface {
	Batch(ctx context.Context, reelIDs []uuid.UUID) (map[uuid.UUID]Counters, error)
}

type Service struct {
	pool     *pgxpool.Pool
	seen     SeenStore
	counters CounterSource
}

func NewService(pool *pgxpool.Pool, seen SeenStore, counters CounterSource) *Service {
	if seen == nil {
		seen = NoopSeenStore{}
	}
	return &Service{pool: pool, seen: seen, counters: counters}
}

// Defaults for a feed page. Twenty is roughly three swipes of buffer — enough
// that prefetch stays ahead of the user, small enough that a page the user
// abandons has not cost much.
const (
	defaultLimit   = 20
	maxLimit       = 50
	candidateLimit = 300
	// recencyWindow bounds the candidate scan. A reel older than this is not
	// competitive under chronological ranking anyway, and the bound is what
	// keeps the GiST scan from growing with the whole table.
	recencyWindow = 30 * 24 * time.Hour
	// maxPerAuthor caps one author's share of a page. Two, not one: a vendor
	// with a genuinely good pair of reels should be able to show both.
	maxPerAuthor = 2
	maxPerCat    = 10
)

// Get serves one page.
func (s *Service) Get(ctx context.Context, q Query) (*Page, error) {
	if q.Lat == 0 && q.Lng == 0 {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "lat", Code: "required",
			Message: "a location is required — the feed is local by definition",
		}}}
	}
	if q.Limit <= 0 {
		q.Limit = defaultLimit
	}
	if q.Limit > maxLimit {
		q.Limit = maxLimit
	}

	// A Redis failure degrades the feed to "shows repeats" rather than failing
	// the request. The front door stays up.
	seenIDs, err := s.seen.Recent(ctx, q.ViewerKey, seenCap)
	if err != nil {
		slog.WarnContext(ctx, "seen-set unavailable, serving with repeats", "error", err)
		seenIDs = nil
	}

	items, err := s.query(ctx, q, seenIDs)
	if err != nil {
		return nil, err
	}

	// Spacing, not filtering — the per-author cap already happened in SQL.
	items = Spread(items, func(i Item) string { return i.AuthorID.String() })

	if err := s.attachCounters(ctx, items); err != nil {
		// Counters are decoration. A feed with zeroes on it is worth serving;
		// a 500 because the counter store hiccuped is not.
		slog.WarnContext(ctx, "counters unavailable", "error", err)
	}

	page := &Page{Items: items}
	if len(items) == q.Limit {
		last := items[len(items)-1]
		page.NextCursor = Cursor{
			Score:       last.Score,
			PublishedAt: last.PublishedAt,
			ID:          last.ID,
		}.Encode()
	}

	// Marking happens here rather than on a client callback because a client
	// that crashes mid-page would otherwise see the same reels forever. The
	// cost of being slightly wrong (marking a reel the user swiped past without
	// watching) is a repeat they do not get; the cost of the other direction is
	// a feed stuck in a loop.
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	if err := s.seen.Mark(ctx, q.ViewerKey, ids); err != nil {
		slog.WarnContext(ctx, "failed to mark seen", "error", err)
	}

	return page, nil
}

// query is the §4.2 shape: candidate → scored → diversified.
//
// Written in three CTEs even though Phase 2's score is a constant expression of
// published_at, because Phase 4 replaces exactly one of them. Collapsing it now
// into a single ORDER BY published_at would save four lines and cost the
// endpoint's rewrite later.
func (s *Service) query(ctx context.Context, q Query, seen []uuid.UUID) ([]Item, error) {
	var categoryArg any
	if q.Category != nil {
		categoryArg = *q.Category
	}
	var viewerArg any
	if q.ViewerID.Valid {
		viewerArg = q.ViewerID.UUID
	}

	seenArg := make([]uuid.UUID, len(seen))
	copy(seenArg, seen)

	// Keyset predicate. Empty on the first page; on subsequent pages it is
	// (score, id) < cursor, which under chronological ranking reduces to
	// (published_at, id).
	var cursorScore any
	var cursorID any
	if !q.Cursor.IsZero() {
		cursorScore = q.Cursor.Score
		cursorID = q.Cursor.ID
	}

	rows, err := s.pool.Query(ctx, `
		WITH candidate AS (
		    SELECT r.id, r.author_type, r.author_id, r.category_id, r.subject_type,
		           r.subject_id, COALESCE(r.caption, '') AS caption,
		           COALESCE(r.duration_ms, 0) AS duration_ms,
		           COALESCE(r.poster_key, '') AS poster_key,
		           COALESCE(r.preview_key, '') AS preview_key,
		           COALESCE(r.lqip, '') AS lqip,
		           r.published_at
		      FROM reels r
		     WHERE r.state = 'published'
		       AND r.visibility = 'public'
		       -- One predicate for both service-area polygons and radius
		       -- listings, because coverage was resolved at publish time.
		       AND ST_Intersects(r.coverage, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography)
		       AND r.published_at > now() - make_interval(secs => $3)
		       AND ($4::uuid IS NULL OR r.category_id = $4)
		       AND NOT (r.id = ANY($5::uuid[]))
		       AND ($6::uuid IS NULL OR NOT EXISTS (
		               SELECT 1 FROM user_blocks b
		                WHERE b.user_id = $6
		                  AND b.blocked_type = r.author_type
		                  AND b.blocked_id = r.author_id))
		     ORDER BY r.published_at DESC
		     LIMIT $7
		),
		scored AS (
		    SELECT c.*,
		           -- PHASE 2: recency decay only.
		           -- PHASE 4: replace this expression with the ranking model's
		           -- score. Nothing outside this CTE reads published_at for
		           -- ordering, so that swap is local.
		           -- Cast to float8 explicitly: EXTRACT returns numeric, and a
		           -- numeric score would then be compared against a float8
		           -- cursor parameter and scanned into a Go float64.
		           (1.0 / (1.0 + EXTRACT(EPOCH FROM (now() - c.published_at)) / 86400.0))::float8 AS score
		      FROM candidate c
		),
		diversified AS (
		    SELECT s.*,
		           row_number() OVER (PARTITION BY s.author_id   ORDER BY s.score DESC, s.id) AS author_rank,
		           row_number() OVER (PARTITION BY s.category_id ORDER BY s.score DESC, s.id) AS cat_rank
		      FROM scored s
		)
		SELECT d.id, d.author_type, d.author_id,
		       COALESCE(vb.name, '') AS author_name,
		       d.subject_type, d.subject_id, d.category_id, d.caption, d.duration_ms,
		       d.poster_key, d.preview_key, d.lqip, d.published_at, d.score,
		       COALESCE(json_agg(json_build_object(
		           'label', rr.label, 'width', rr.width, 'height', rr.height,
		           'bitrate_kbps', rr.bitrate_kbps, 'playlist_url', rr.playlist_url
		       ) ORDER BY rr.bitrate_kbps) FILTER (WHERE rr.id IS NOT NULL), '[]') AS renditions
		  FROM diversified d
		  LEFT JOIN vendor_branches vb
		         ON d.author_type = 'vendor_branch' AND vb.id = d.author_id
		  LEFT JOIN reel_renditions rr ON rr.reel_id = d.id
		 WHERE d.author_rank <= $8
		   AND d.cat_rank <= $9
		   AND ($10::float8 IS NULL OR (d.score, d.id) < ($10::float8, $11::uuid))
		 GROUP BY d.id, d.author_type, d.author_id, vb.name, d.subject_type, d.subject_id,
		          d.category_id, d.caption, d.duration_ms, d.poster_key, d.preview_key,
		          d.lqip, d.published_at, d.score
		 ORDER BY d.score DESC, d.id
		 LIMIT $12`,
		q.Lat, q.Lng, recencyWindow.Seconds(), categoryArg, seenArg, viewerArg,
		candidateLimit, maxPerAuthor, maxPerCat, cursorScore, cursorID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("feed query: %w", err)
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		var it Item
		var renditions []byte
		if err := rows.Scan(&it.ID, &it.AuthorType, &it.AuthorID, &it.AuthorName,
			&it.SubjectType, &it.SubjectID, &it.CategoryID, &it.Caption, &it.DurationMS,
			&it.PosterKey, &it.PreviewKey, &it.LQIP, &it.PublishedAt, &it.Score,
			&renditions); err != nil {
			return nil, fmt.Errorf("scan feed item: %w", err)
		}
		it.Renditions = decodeRenditions(renditions)
		out = append(out, it)
	}
	return out, rows.Err()
}

// decodeRenditions unpacks the aggregated JSON.
//
// Aggregating in SQL rather than issuing a second query per page: a feed page
// is 20 reels × 3 renditions, and 20 extra round-trips on the hottest read path
// is the definition of an N+1. A malformed aggregate yields no renditions
// rather than an error — the client falls back to the poster frame, which is a
// visible degradation the user can survive.
func decodeRenditions(raw []byte) []Rendition {
	if len(raw) == 0 {
		return nil
	}
	var rows []struct {
		Label       string `json:"label"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		BitrateKbps int    `json:"bitrate_kbps"`
		PlaylistURL string `json:"playlist_url"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	out := make([]Rendition, 0, len(rows))
	for _, r := range rows {
		out = append(out, Rendition{
			Label: r.Label, Width: r.Width, Height: r.Height,
			BitrateKbps: r.BitrateKbps, PlaylistURL: r.PlaylistURL,
		})
	}
	return out
}

func (s *Service) attachCounters(ctx context.Context, items []Item) error {
	if s.counters == nil || len(items) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	counts, err := s.counters.Batch(ctx, ids)
	if err != nil {
		return err
	}
	for i := range items {
		if c, ok := counts[items[i].ID]; ok {
			items[i].Counters = c
		}
	}
	return nil
}
