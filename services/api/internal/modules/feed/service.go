package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ranking"
	"github.com/marketplace/api/internal/seam/subject"
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

	Score float64
	// Explore marks a slot given to a reel that had not earned its place on
	// score. Surfaced so that the impression it produces can be told apart from
	// an earned one — without that, exploration cost is unmeasurable and the
	// first cost-cutting exercise deletes it.
	Explore bool
	// Followed is carried through to the response so the client can render the
	// relationship without a second round-trip.
	Followed bool

	// Sponsored marks a paid slot (Phase 9). Surfaced to the client because
	// labelling advertising is a legal requirement in both markets, and
	// surfaced in the analytics for a second reason: a sponsored impression
	// that counted as an organic one would let money buy organic rank.
	Sponsored bool
	// DeliveryID identifies the sold impression. Echoed back on the view event
	// to convert a served impression into a charged one; empty on organic
	// items.
	DeliveryID uuid.NullUUID

	// TimeSignal is when the thing this reel is about stops being true, read
	// from the projection the owning vertical writes. Nil for a reel with no
	// time dimension, which is the majority and is a normal state.
	//
	// The feed carries it and never interprets it. There is no branch on Mode
	// anywhere in this package — the two surfaces that care read EndsAt, which
	// is an instant and not a vertical's business logic.
	TimeSignal *subject.TimeSignal

	// DistanceKM is the viewer's distance from the reel's origin point, nil when
	// the reel has none.
	//
	// Already computed for ranking; lifted onto the item because SurfaceNearby
	// orders by it. Not serialised — the client shows "2.1km" from its own
	// geometry and a server-rendered distance would be wrong the moment the
	// viewer walked anywhere.
	DistanceKM *float64
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

	// Surface is which tab the viewer is on. Zero value is not valid — callers
	// go through ParseSurface, which defaults rather than fails. Get treats an
	// empty value as SurfaceForYou so that an internal caller constructing a
	// Query by hand gets the ranked feed instead of no feed.
	Surface Surface

	// SubjectTypes narrows the feed to one or more verticals — the filter row.
	//
	// A slice rather than a single value because "Cars and Meals" is a coherent
	// thing to ask for and a single value would force the client to fetch twice
	// and interleave two ranked pages by hand, which is not ranking.
	//
	// Empty means every vertical, and that includes reels with no subject at
	// all. That default is load-bearing: most reels today have no subject, and a
	// filter that silently excluded them would make "All" show less than the
	// unfiltered feed.
	SubjectTypes []string

	// ViewerID is empty for anonymous browsing, which is supported: putting a
	// login wall in front of the front door would be fatal to it.
	ViewerID uuid.NullUUID
	// ViewerKey namespaces the seen-set, the interest profile and the
	// exploration slot seed. Falls back to a device identifier for anonymous
	// viewers.
	ViewerKey string

	// Locale is the language the viewer is browsing in. Unused by organic
	// retrieval — a reel is watchable in either language and filtering on it
	// would halve an already-thin feed — and used by sponsored targeting, where
	// an advertiser with Arabic creative can reasonably decline English
	// viewers.
	Locale string
}

type Page struct {
	Items      []Item
	NextCursor string
	// Profile names the ranking configuration that produced this page. Returned
	// so that "the feed looked wrong at 14:20" can be answered rather than
	// discussed.
	Profile string
}

// CounterSource reads engagement counts from the hot store.
type CounterSource interface {
	Batch(ctx context.Context, reelIDs []uuid.UUID) (map[uuid.UUID]Counters, error)
}

// SponsoredSource fills paid slots (Phase 9).
//
// Declared here rather than imported, and the interface is one method wide on
// purpose. What the feed knows about advertising is "here is a request, give me
// back some reels and their delivery ids". It does not know what a campaign is,
// what a bid is, how pacing works or that money exists — and it must not, or
// the next revenue idea becomes a change to the feed.
//
// The reverse direction matters more. The boosts package is handed the viewer's
// *location*, and its own query applies `ST_Intersects(r.coverage, viewer)` —
// the same test, against the same column, that this file's retrieval makes. A
// sponsored reel is eligible only where its author actually serves. There is no
// parameter here that could relax that,
// which is the point: PLAN-VIDEO-FIRST §10's promise is "everything here is
// available to you right now", and an ad system exempt from it would sell the
// one claim the product has.
type SponsoredSource interface {
	Fill(ctx context.Context, req SponsoredRequest) ([]SponsoredItem, error)
}

// SponsoredRequest is what the feed asks for.
type SponsoredRequest struct {
	Lat, Lng     float64
	ViewerKey    string
	ViewerID     uuid.NullUUID
	Category     *uuid.UUID
	Locale       string
	ExcludeReels []uuid.UUID
	Slots        int
}

// SponsoredItem is one sold slot.
type SponsoredItem struct {
	ReelID uuid.UUID
	// DeliveryID is returned to the client and echoed back on the view event,
	// which is what converts a served impression into a charged one. Without it
	// the confirmation would have to be matched on (viewer, reel) and would
	// charge for organic views of a boosted reel.
	DeliveryID uuid.UUID
}

type Service struct {
	pool      *pgxpool.Pool
	seen      SeenStore
	counters  CounterSource
	weights   ranking.Provider
	sponsored SponsoredSource
}

// WithSponsored enables paid slots.
//
// A setter rather than a constructor argument, because a deployment without an
// ad system is the normal one for a long time — PLAN-VIDEO-FIRST §7 says boosts
// only work "after you have enough organic demand that reach is worth buying" —
// and threading a nil through three call sites to say so is worse than this.
func (s *Service) WithSponsored(src SponsoredSource) *Service {
	s.sponsored = src
	return s
}

func NewService(pool *pgxpool.Pool, seen SeenStore, counters CounterSource, weights ranking.Provider) *Service {
	if seen == nil {
		seen = NoopSeenStore{}
	}
	if weights == nil {
		// Ranking with compiled-in defaults beats not ranking. A feed that
		// refuses to serve because a config provider was not wired is a blank
		// front door.
		weights = ranking.StaticProvider{W: ranking.DefaultWeights()}
	}
	return &Service{pool: pool, seen: seen, counters: counters, weights: weights}
}

// Defaults for a feed page. Twenty is roughly three swipes of buffer — enough
// that prefetch stays ahead of the user, small enough that a page the user
// abandons has not cost much.
const (
	defaultLimit = 20
	maxLimit     = 50

	// recencyWindow bounds the candidate scan. A reel older than this is not
	// competitive under any weighting we ship, and the bound is what keeps the
	// GiST scan from growing with the whole table.
	recencyWindow = 30 * 24 * time.Hour

	// eligibleCap bounds the materialised eligible set. In a launch district
	// this is never reached; at city scale it is what stops one request from
	// building a working set proportional to the catalogue.
	eligibleCap = 2000

	// Per-source candidate quotas. They sum to more than one page by a wide
	// margin on purpose — ranking needs something to choose between, and a
	// candidate set the size of the page is a candidate set that does nothing.
	freshQuota    = 200
	followedQuota = 80
	affinityQuota = 120
	exploreQuota  = 80

	// affinityFloor is the affinity at which a category becomes a retrieval
	// source rather than merely a scoring nudge. Below it the signal is one
	// watched reel and a coincidence.
	affinityFloor = 0.2

	// maxPerAuthor caps one author's share of a page. Two, not one: a vendor
	// with a genuinely good pair of reels should be able to show both.
	maxPerAuthor = 2
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
	if q.Surface == "" {
		q.Surface = SurfaceForYou
	}

	weights := s.weights.Weights(ctx)

	// Two exclusion sources, unioned. Redis is the broad one — seven days of
	// impressions across every session. The cursor is the narrow one and the
	// authoritative one for pagination: without it a deployment with no Redis
	// returns page 1 forever.
	seenIDs, err := s.seen.Recent(ctx, q.seenKey(), seenCap)
	if err != nil {
		slog.WarnContext(ctx, "seen-set unavailable, paginating from the cursor alone", "error", err)
		seenIDs = nil
	}
	exclude := union(seenIDs, q.Cursor.Excluded)

	candidates, err := s.retrieve(ctx, q, exclude, weights)
	if err != nil {
		return nil, err
	}

	items := s.rank(candidates, q, weights)
	items = s.injectSponsored(ctx, q, items)

	if err := s.attachCounters(ctx, items); err != nil {
		// Counters are decoration. A feed with zeroes on it is worth serving;
		// a 500 because the counter store hiccuped is not.
		slog.WarnContext(ctx, "counters unavailable", "error", err)
	}

	page := &Page{Items: items, Profile: profileName(s.weights)}
	if len(items) > 0 {
		served := make([]uuid.UUID, len(items))
		for i, it := range items {
			served[i] = it.ID
		}

		// A cursor only on a full page. A short page means the eligible set is
		// exhausted, and §10 of the plan argues that a feed which *can* be
		// exhausted is the product — finite and locally relevant beats infinite
		// and irrelevant. Handing back a cursor here would have clients paging
		// into emptiness to discover what the server already knows.
		if len(items) == q.Limit {
			page.NextCursor = q.Cursor.Next(served).Encode()
		}

		// Marking happens here rather than on a client callback because a client
		// that crashes mid-page would otherwise see the same reels forever. The
		// cost of being slightly wrong (marking a reel the user swiped past
		// without watching) is a repeat they do not get; the cost of the other
		// direction is a feed stuck in a loop.
		if err := s.seen.Mark(ctx, q.seenKey(), served); err != nil {
			slog.WarnContext(ctx, "failed to mark seen", "error", err)
		}
	}

	return page, nil
}

// injectSponsored fills paid slots, and never at the cost of serving a page.
//
// Every failure here degrades to the organic page. That is not defensive
// programming for its own sake — it is the correct severity ordering. A feed
// that 500s because the ad system is slow has taken the product down to protect
// revenue, which is exactly backwards: the revenue exists because the feed
// works.
func (s *Service) injectSponsored(ctx context.Context, q Query, items []Item) []Item {
	if s.sponsored == nil || len(items) == 0 {
		return items
	}

	slots := sponsoredSlots(q.Limit)
	if len(slots) == 0 {
		return items
	}

	exclude := make([]uuid.UUID, len(items))
	for i, it := range items {
		exclude[i] = it.ID
	}

	filled, err := s.sponsored.Fill(ctx, SponsoredRequest{
		Lat: q.Lat, Lng: q.Lng,
		ViewerKey: q.ViewerKey, ViewerID: q.ViewerID,
		Category: q.Category, Locale: q.Locale,
		ExcludeReels: exclude, Slots: len(slots),
	})
	if err != nil {
		slog.WarnContext(ctx, "sponsored slots unavailable, serving organic", "error", err)
		return items
	}
	if len(filled) == 0 {
		return items
	}

	// The sold slots name reels; their content still has to be loaded, through
	// the same query the organic path uses. A separate serving shape for
	// sponsored reels is how an ad ends up rendering differently from the
	// content around it, which is both a labelling problem and an obvious one.
	ads, err := s.loadByIDs(ctx, filled)
	if err != nil {
		slog.WarnContext(ctx, "sponsored reels not loadable, serving organic", "error", err)
		return items
	}

	return Inject(items, ads, slots, q.Limit)
}

// sponsoredSlots mirrors boosts.SponsoredSlots.
//
// Duplicated deliberately rather than imported. `feed` is guarded by
// internal/app/seam_test.go against reaching into modules that know about
// specific business domains, and importing the ad package for three constants
// would be the first crack in that — the second import is always easier than
// the first. Three lines of arithmetic is a cheaper price than the precedent.
func sponsoredSlots(pageSize int) []int {
	const (
		maxShare = 6
		first    = 2
		gap      = 4
	)
	if pageSize <= first || pageSize/maxShare == 0 {
		return nil
	}
	max := pageSize / maxShare
	slots := make([]int, 0, max)
	for pos := first; pos < pageSize && len(slots) < max; pos += gap {
		slots = append(slots, pos)
	}
	return slots
}

// loadByIDs materialises sponsored reels through the same projection the
// organic path uses.
func (s *Service) loadByIDs(ctx context.Context, filled []SponsoredItem) ([]Item, error) {
	ids := make([]uuid.UUID, len(filled))
	delivery := make(map[uuid.UUID]uuid.UUID, len(filled))
	for i, f := range filled {
		ids[i] = f.ReelID
		delivery[f.ReelID] = f.DeliveryID
	}

	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.author_type, r.author_id,
		       COALESCE(vb.name, u.display_name, '') AS author_name,
		       r.subject_type, r.subject_id, r.category_id,
		       COALESCE(r.caption, ''), COALESCE(r.duration_ms, 0),
		       COALESCE(r.poster_key, ''), COALESCE(r.preview_key, ''),
		       COALESCE(r.lqip, ''),
		       COALESCE((SELECT jsonb_agg(jsonb_build_object(
		           'label', rr.label, 'width', rr.width, 'height', rr.height,
		           'bitrate_kbps', rr.bitrate_kbps, 'playlist_url', rr.playlist_url))
		         FROM reel_renditions rr WHERE rr.reel_id = r.id), '[]'::jsonb),
		       r.published_at
		  FROM reels r
		  LEFT JOIN vendor_branches vb ON r.author_type = 'vendor_branch' AND vb.id = r.author_id
		  LEFT JOIN users u ON r.author_type = 'user' AND u.id = r.author_id
		 WHERE r.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("load sponsored reels: %w", err)
	}
	defer rows.Close()

	byID := map[uuid.UUID]Item{}
	for rows.Next() {
		var it Item
		var renditions []byte
		if err := rows.Scan(&it.ID, &it.AuthorType, &it.AuthorID, &it.AuthorName,
			&it.SubjectType, &it.SubjectID, &it.CategoryID, &it.Caption, &it.DurationMS,
			&it.PosterKey, &it.PreviewKey, &it.LQIP, &renditions, &it.PublishedAt); err != nil {
			return nil, err
		}
		it.Renditions = decodeRenditions(renditions)
		it.DeliveryID = uuid.NullUUID{UUID: delivery[it.ID], Valid: true}
		byID[it.ID] = it
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Returned in auction order rather than in whatever order Postgres chose,
	// so the highest-value winner takes the earliest slot.
	out := make([]Item, 0, len(filled))
	for _, f := range filled {
		if it, ok := byID[f.ReelID]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}

// One fetches a single reel by id, for a viewer who already knows which one
// they want.
//
// # Why this is not "the feed with a where clause"
//
// The feed's job is to *choose*, and almost all of it — retrieval, ranking,
// exploration slots, the seen-set, sponsored injection — is about choosing.
// None of that applies when a viewer taps a row in Saved: they have already
// chosen, and re-running the ranker over one row would be theatre.
//
// What does still apply is eligibility, and that is the whole reason this
// method exists rather than the handler reading the row itself. A saved reel
// can have been taken down, its subject can have sold, its author can have been
// blocked. `published` and not deleted is the floor; anything below it must
// 404 rather than play, because a reel that is out of the feed for a moderation
// reason must be out of *every* surface.
//
// Deliberately not geo-filtered, which is the one place this differs from the
// feed. Coverage decides who a reel is *shown* to unprompted; a viewer who
// saved a car in Dubai and opened the app in Al Ain has not stopped being
// entitled to look at it. The CTA still resolves against their location and
// will refuse if the thing genuinely cannot reach them, which is the correct
// place for that refusal.
func (s *Service) One(ctx context.Context, reelID uuid.UUID) (Item, bool, error) {
	items, err := s.loadByIDs(ctx, []SponsoredItem{{ReelID: reelID}})
	if err != nil {
		return Item{}, false, err
	}
	if len(items) == 0 {
		return Item{}, false, nil
	}

	// `state` and `visibility`, which between them are the whole gate. There is
	// no soft-delete column on reels — removal is the `removed` state, which
	// `published` already excludes — so checking for one was a wrong guess that
	// 500'd rather than failing open, which is at least the right direction.
	var state, visibility string
	err = s.pool.QueryRow(ctx,
		`SELECT state::text, COALESCE(visibility, 'public') FROM reels WHERE id = $1`, reelID,
	).Scan(&state, &visibility)
	if err != nil {
		return Item{}, false, fmt.Errorf("feed: reel state: %w", err)
	}
	// Visibility is checked here and not in the feed's own query, because the
	// feed never retrieves a non-public reel in the first place — its candidate
	// sources filter on it. This endpoint takes an id from a client, so it is
	// the one path where an unlisted reel could be fetched by guessing.
	if state != "published" || visibility != "public" {
		return Item{}, false, nil
	}

	it := items[0]
	// loadByIDs stamps a delivery id because its only caller until now was the
	// sponsored path. This reel was not delivered as an ad, and leaving a
	// fabricated id on it would attribute an organic view to a campaign.
	it.DeliveryID = uuid.NullUUID{}
	return it, true, nil
}

// candidate is one retrieved reel with its ranking features attached.
type candidate struct {
	Item
	features ranking.Features
}

// retrieve runs candidate generation and the hard filters.
//
// This is the half of the feed that stays in SQL, and the split is on the
// question being asked: "what may this viewer be shown" is about geometry,
// state and blocks, and Postgres answers it with indexes. "What is best" is a
// modelling question and it happens in Go, where it can be tested.
//
// PLAN-VIDEO-FIRST §3's candidate sources, unioned then deduplicated:
//
//	fresh + geo-local     the baseline; without it a viewer with no history
//	                      and no follows would get an empty feed
//	followed authors      the most explicit signal a viewer ever sends
//	category affinity     the interest profile, as retrieval rather than only
//	                      as a scoring nudge — a category the viewer loves is
//	                      worth *finding* reels in, not only re-ordering them
//	exploration pool      reels below the lifetime-impression floor. This one
//	                      is a candidate source and not merely a scoring bonus
//	                      on purpose: a reel with no signal cannot win on score
//	                      by construction, so if it is not retrieved separately
//	                      it is never retrieved at all
//
// The hard filters live inside `eligible` and apply to every source. §3 is
// explicit that deliverability and moderation state are filters, never soft
// signals — a vendor who cannot deliver to you is noise regardless of how well
// their reel performs.
func (s *Service) retrieve(ctx context.Context, q Query, exclude []uuid.UUID, w ranking.Weights) ([]candidate, error) {
	var categoryArg any
	if q.Category != nil {
		categoryArg = *q.Category
	}
	var viewerArg any
	if q.ViewerID.Valid {
		viewerArg = q.ViewerID.UUID
	}
	var viewerKeyArg any
	if q.ViewerKey != "" {
		viewerKeyArg = q.ViewerKey
	}

	args := []any{
		q.Lat, q.Lng, recencyWindow.Seconds(), categoryArg, exclude, viewerArg,
		eligibleCap, freshQuota, followedQuota,
		viewerKeyArg, affinityFloor, affinityQuota,
		w.ExplorationImpressionFloor, exploreQuota,
	}
	// $15 appears only inside the ending-soon predicate. pgx prepares the
	// statement from the SQL it is handed and rejects an argument the statement
	// never declared, so it is appended exactly when the text that references it
	// is present. Passing it unconditionally fails every other surface with an
	// arity error, which is a good failure — loud and immediate — but a failure.
	// The ending-soon horizon is $15, but only on that surface — its predicate
	// is the only text that mentions it. So the subject filter, which every
	// surface carries, is $15 or $16 depending on whether the horizon is there.
	//
	// Numbered rather than padded. Passing a nil placeholder to keep the indexes
	// fixed looks tidier and does not work: Postgres infers parameter types from
	// where they are used, and a parameter the statement never references is
	// `could not determine data type of parameter $15` at prepare time — a 500
	// on every request, which is how this was found.
	subjParam := "$15"
	if q.Surface == SurfaceEndingSoon {
		args = append(args, EndingSoonHorizon.Seconds())
		subjParam = "$16"
	}
	args = append(args, subjectTypesArg(q.SubjectTypes))

	rows, err := s.pool.Query(ctx, `
		WITH viewer AS (
		    SELECT ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography AS pt
		),
		eligible AS (
		    -- The hard filters. Referenced four times below, which makes
		    -- Postgres materialise it — deliberately: the geo scan is the
		    -- expensive part and it should run once, not once per source.
		    SELECT r.id, r.published_at, r.author_type, r.author_id, r.category_id,
		           ts.ends_at
		      FROM reels r
		      CROSS JOIN viewer v
		      -- The surface's narrowing happens here, inside the eligible CTE,
		      -- rather than in each of the four sources below. One place, so a
		      -- source added later cannot forget it — and a source that forgot
		      -- it would leak a closed auction into the Live now tab, which is
		      -- the single most obviously broken thing this feed could do.
		      --
		      -- LEFT rather than INNER even when the surface requires a signal:
		      -- the predicate is what filters. A reel with no signal row yields
		      -- NULL for the ends_at comparison, which is not true, so it drops
		      -- out for Live now and Ending soon and survives for the other two,
		      -- where the predicate is the constant true.
		      LEFT JOIN reel_time_signals ts
		             ON ts.subject_type = r.subject_type
		            AND ts.subject_id = r.subject_id
		     WHERE r.state = 'published'
		       AND (`+q.Surface.candidatePredicate()+`)
		       AND r.visibility = 'public'
		       -- The filter row. NULL-safe by construction: cardinality(NULL) is
		       -- NULL, so the coalesce is what makes "no filter" mean "keep
		       -- everything" rather than "keep nothing" — the same trap the
		       -- exclusion list below documents, and it has bitten this query
		       -- once already.
		       AND (coalesce(cardinality(`+subjParam+`::text[]), 0) = 0
		            OR r.subject_type = ANY(`+subjParam+`))
		       -- One predicate for both service-area polygons and radius
		       -- listings, because coverage was resolved at publish time.
		       AND ST_Intersects(r.coverage, v.pt)
		       AND r.published_at > now() - make_interval(secs => $3)
		       AND ($4::uuid IS NULL OR r.category_id = $4)
		       -- The coalesce is load-bearing. A nil slice arrives as SQL NULL;
		       -- id = ANY(NULL) is NULL, NOT NULL is NULL, and every row is
		       -- filtered out — an empty feed with no error anywhere. The union
		       -- helper above never returns nil, so the serving path was safe,
		       -- but it was safe by accident: the first other caller, the admin
		       -- feed inspector, hit this immediately.
		       AND NOT (r.id = ANY(coalesce($5::uuid[], ARRAY[]::uuid[])))
		       AND ($6::uuid IS NULL OR NOT EXISTS (
		               SELECT 1 FROM user_blocks b
		                WHERE b.user_id = $6
		                  AND b.blocked_type = r.author_type
		                  AND b.blocked_id = r.author_id))
		     ORDER BY `+q.Surface.eligibleOrder()+`
		     LIMIT $7
		),
		fresh AS (
		    SELECT id FROM eligible ORDER BY `+q.Surface.freshOrder()+` LIMIT $8
		),
		followed AS (
		    SELECT e.id FROM eligible e
		     WHERE $6::uuid IS NOT NULL
		       AND EXISTS (
		           SELECT 1 FROM follows f
		            WHERE f.follower_id = $6
		              AND f.followee_type = e.author_type
		              AND f.followee_id = e.author_id)
		     ORDER BY e.published_at DESC LIMIT $9
		),
		affine AS (
		    SELECT e.id FROM eligible e
		      JOIN viewer_interests vi
		        ON vi.viewer_key = $10 AND vi.category_id = e.category_id
		     WHERE vi.affinity > $11
		     ORDER BY vi.affinity DESC, e.published_at DESC LIMIT $12
		),
		explore AS (
		    SELECT e.id FROM eligible e
		      LEFT JOIN reel_features rf ON rf.reel_id = e.id
		     WHERE COALESCE(rf.lifetime_impressions, 0) < $13
		     ORDER BY e.published_at DESC LIMIT $14
		),
		candidate AS (
		    SELECT id FROM fresh
		    UNION SELECT id FROM followed
		    UNION SELECT id FROM affine
		    UNION SELECT id FROM explore
		)
		SELECT r.id, r.author_type, r.author_id, COALESCE(vb.name, '') AS author_name,
		       r.subject_type, r.subject_id, r.category_id,
		       COALESCE(r.caption, ''), COALESCE(r.duration_ms, 0),
		       COALESCE(r.poster_key, ''), COALESCE(r.preview_key, ''),
		       COALESCE(r.lqip, ''), r.published_at,
		       rend.renditions,

		       COALESCE(rf.impressions, 0), COALESCE(rf.views, 0),
		       COALESCE(rf.completions, 0), COALESCE(rf.clicks, 0),
		       COALESCE(rf.likes, 0), COALESCE(rf.comments, 0),
		       COALESCE(rf.shares, 0), COALESCE(rf.saves, 0),
		       COALESCE(rf.orders, 0), COALESCE(rf.lifetime_impressions, 0),

		       COALESCE(aq.published_count, 0), COALESCE(aq.impressions, 0),
		       COALESCE(aq.clicks, 0), COALESCE(aq.orders, 0),
		       COALESCE(aq.rejected_count, 0), COALESCE(aq.takedown_count, 0),

		       COALESCE(vi.affinity, 0)::float8,
		       ($6::uuid IS NOT NULL AND EXISTS (
		            SELECT 1 FROM follows f
		             WHERE f.follower_id = $6
		               AND f.followee_type = r.author_type
		               AND f.followee_id = r.author_id)) AS followed,
		       -- NULL when the reel has no origin point. Scored as neutral
		       -- rather than distant: demoting a reel for a data-collection
		       -- failure makes a pipeline bug look like a ranking decision.
		       CASE WHEN r.origin_point IS NULL THEN NULL
		            ELSE (ST_Distance(r.origin_point, v.pt) / 1000.0)::float8 END AS distance_km,
		       EXTRACT(EPOCH FROM (now() - r.published_at))::float8 AS age_seconds,

		       -- The time signal, joined for every surface and not only the two
		       -- that filter on it. A countdown is what the overlay draws; a
		       -- reel that reaches For you with a lot closing in four minutes
		       -- should say so, and returning it only on the Live now tab would
		       -- make urgency a property of which tab you happened to open.
		       ts.mode, ts.starts_at, ts.ends_at, ts.horizon_seconds,
		       ts.timezone, ts.recurring

		  FROM candidate c
		  JOIN reels r ON r.id = c.id
		  CROSS JOIN viewer v
		  LEFT JOIN reel_time_signals ts
		         ON ts.subject_type = r.subject_type
		        AND ts.subject_id = r.subject_id
		  LEFT JOIN vendor_branches vb
		         ON r.author_type = 'vendor_branch' AND vb.id = r.author_id
		  LEFT JOIN reel_features rf ON rf.reel_id = r.id
		  LEFT JOIN author_quality aq
		         ON aq.author_type = r.author_type AND aq.author_id = r.author_id
		  LEFT JOIN viewer_interests vi
		         ON vi.viewer_key = $10 AND vi.category_id = r.category_id
		  -- LATERAL rather than a join plus GROUP BY. Aggregating in SQL avoids
		  -- 20 extra round-trips per page — the definition of an N+1 on the
		  -- hottest read path — and the lateral form keeps the outer query from
		  -- needing a thirty-column GROUP BY that nobody will maintain.
		  LEFT JOIN LATERAL (
		      SELECT COALESCE(json_agg(json_build_object(
		                 'label', rr.label, 'width', rr.width, 'height', rr.height,
		                 'bitrate_kbps', rr.bitrate_kbps, 'playlist_url', rr.playlist_url
		             ) ORDER BY rr.bitrate_kbps), '[]') AS renditions
		        FROM reel_renditions rr WHERE rr.reel_id = r.id
		  ) rend ON true`, args...)
	if err != nil {
		return nil, fmt.Errorf("feed retrieval: %w", err)
	}
	defer rows.Close()

	var out []candidate
	for rows.Next() {
		var c candidate
		var renditions []byte
		var affinity float64
		var distance *float64
		var ageSeconds float64

		// The time signal, all six columns nullable together: a reel whose
		// subject has no projection row yields NULLs across the board, which is
		// the common case and not an error.
		var sigMode, sigTimezone *string
		var sigStartsAt, sigEndsAt *time.Time
		var sigHorizon *int32
		var sigRecurring *bool

		f := &c.features

		if err := rows.Scan(&c.ID, &c.AuthorType, &c.AuthorID, &c.AuthorName,
			&c.SubjectType, &c.SubjectID, &c.CategoryID, &c.Caption, &c.DurationMS,
			&c.PosterKey, &c.PreviewKey, &c.LQIP, &c.PublishedAt, &renditions,

			&f.Impressions, &f.Views, &f.Completions, &f.Clicks,
			&f.Likes, &f.Comments, &f.Shares, &f.Saves,
			&f.Orders, &f.LifetimeImpressions,

			&f.Author.Published, &f.Author.Impressions, &f.Author.Clicks,
			&f.Author.Orders, &f.Author.Rejected, &f.Author.Takedowns,

			&affinity, &c.Followed, &distance, &ageSeconds,
			&sigMode, &sigStartsAt, &sigEndsAt, &sigHorizon,
			&sigTimezone, &sigRecurring,
		); err != nil {
			return nil, fmt.Errorf("scan feed candidate: %w", err)
		}

		c.Renditions = decodeRenditions(renditions)
		f.Affinity = affinity
		f.Followed = c.Followed
		f.Age = time.Duration(ageSeconds * float64(time.Second))
		f.DistanceKM = ranking.UnknownDistance
		if distance != nil {
			f.DistanceKM = *distance
		}
		c.DistanceKM = distance

		// Mode is the discriminator: it is NOT NULL in the projection, so its
		// presence is exactly "this subject has a signal". Checking any other
		// column would be checking an optional one.
		if sigMode != nil {
			sig := &subject.TimeSignal{Mode: subject.Mode(*sigMode)}
			if sigStartsAt != nil {
				sig.StartsAt = *sigStartsAt
			}
			if sigEndsAt != nil {
				sig.EndsAt = *sigEndsAt
			}
			if sigHorizon != nil {
				sig.Horizon = time.Duration(*sigHorizon) * time.Second
			}
			if sigTimezone != nil {
				sig.Timezone = *sigTimezone
			}
			if sigRecurring != nil {
				sig.Recurring = *sigRecurring
			}
			// AsOf is stamped once for the whole page by Get, not here. Reading
			// the clock per row would hand a page of reels a spread of "server
			// now" values, and the client subtracts one of them from its own
			// clock to correct skew — so the correction would differ by reel and
			// two countdowns on the same page would disagree.
			c.TimeSignal = sig
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// seenKey namespaces the seen-set per *view*, not per viewer.
//
// Found the moment the filter row was first used, and it is worth stating
// plainly because the naive key looks obviously right: the viewer scrolls the
// unfiltered feed, every reel is marked seen, they tap "Cars" — and the one car
// they just scrolled past is excluded, so the tab they deliberately narrowed to
// comes back empty. The filter looks broken while working perfectly.
//
// The seen-set exists to stop *one feed* repeating itself as it pages. It was
// never meant to be a memory that follows the viewer across a change of intent,
// and narrowing to a vertical is exactly that: a new question, which deserves
// the best answer rather than the leftovers of the previous one.
//
// The cost is that a reel seen in For you can appear again under Cars. That is
// the correct trade — a viewer who asks for cars wants the cars, including one
// they half-watched a minute ago — and it is bounded, because within any single
// view paging still excludes properly.
func (q Query) seenKey() string {
	if q.ViewerKey == "" {
		return ""
	}
	key := q.ViewerKey + "|" + string(q.Surface)
	if len(q.SubjectTypes) > 0 {
		// Sorted, so ?subject_type=a&subject_type=b and the reverse are one
		// view rather than two half-remembered ones.
		types := append([]string(nil), q.SubjectTypes...)
		sort.Strings(types)
		key += "|" + strings.Join(types, ",")
	}
	return key + "|" + geoCell(q.Lat, q.Lng)
}

// geoCell buckets a point so the seen-set is scoped to *where* the feed was
// served, without resetting on GPS jitter.
//
// Location belongs in the key for the same reason the vertical does, and it was
// missed the first time: moving the feed to another district is a new question,
// not a filter over the old answer — which is what the client already says in
// as many words ("this is not a filter over the current results, it is a
// different feed"). Without it, a kitchen seen at Dubai Marina is silently
// excluded from a feed the viewer deliberately moved to Al Barsha to find, and
// the empty screen reads as "nothing here" rather than "you already saw it".
//
// Two decimal places is roughly a kilometre at this latitude. Finer would make
// a stationary phone's GPS wander mint a new key every few pages and forget
// what it had shown; coarser would merge districts that are genuinely different
// feeds. Rounded rather than truncated so that points either side of a boundary
// do not land in cells further apart than they are.
func geoCell(lat, lng float64) string {
	return fmt.Sprintf("%.2f,%.2f", lat, lng)
}

// subjectTypesArg normalises the filter for SQL.
//
// A nil slice rather than an empty one: cardinality of an empty array is 0 and
// of NULL is NULL, and the predicate coalesces either to "no filter". Passing
// nil keeps the intent visible in a query log — an empty array reads like a
// filter that matched nothing.
func subjectTypesArg(types []string) any {
	if len(types) == 0 {
		return nil
	}
	return types
}

// rank turns a candidate set into a page.
//
// Score → partition → interleave → diversify. The partition is the part worth
// reading twice: exploration candidates are ranked *against each other* and
// placed into reserved slots, rather than competing with everything else on a
// score they cannot win. A reel with no signal loses every comparison by
// construction, so "boost new reels a bit" does not work — the boost either
// does nothing or it outranks genuinely good content. Reserving slots is the
// only version of this that has a bounded, measurable cost.
func (s *Service) rank(candidates []candidate, q Query, w ranking.Weights) []Item {
	if len(candidates) == 0 {
		return nil
	}

	// One clock for the page. Every countdown on it is measured against the same
	// instant, and the same instant is stamped into every signal's AsOf, so the
	// skew correction a client computes from any reel is valid for all of them.
	now := time.Now().UTC()

	var ranked, explore []Item
	for _, c := range candidates {
		item := c.Item
		item.Score = ranking.Score(c.features, w)
		// The surface says what "best" means, and then the existing machinery —
		// exploration partition, per-author cap, interleave, spread — runs
		// unchanged on top of it. See Surface.overrideScore for why this is a
		// score override and not a re-sort of the finished page.
		q.Surface.overrideScore(&item, now)
		if item.TimeSignal != nil {
			item.TimeSignal.AsOf = now
		}
		if q.Surface.reservesExploration() && ranking.Eligible(c.features, w) {
			item.Explore = true
			explore = append(explore, item)
		} else {
			ranked = append(ranked, item)
		}
	}

	byScore := func(items []Item) {
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Score != items[j].Score {
				return items[i].Score > items[j].Score
			}
			// Deterministic tie-break. Without it, two reels with identical
			// scores swap places between requests and the page appears to
			// shuffle on every refresh.
			return items[i].ID.String() < items[j].ID.String()
		})
	}
	byScore(ranked)
	byScore(explore)

	// One exploration slot per author, so a single new vendor cannot take the
	// whole reserve. Two ranked slots, because a vendor with a genuinely good
	// pair should be able to show both.
	rankedCapped := capPerAuthor(ranked, maxPerAuthor)
	exploreCapped := capPerAuthor(explore, 1)

	slots := ranking.Slots(q.Limit, ranking.Seed(q.ViewerKey, q.Cursor.Page), w)
	page := ranking.Interleave(rankedCapped, exploreCapped, slots, q.Limit)

	// Backfill.
	//
	// Found in QA and it is the cold-start scenario rather than an edge case. In
	// a launch district almost everything is low-impression, so almost
	// everything lands in `explore` — and the one-per-author cap on that bucket
	// then means a district with one vendor serves a **one-item feed**, however
	// many reels that vendor has published.
	//
	// The cap is right for slot *allocation*: exploration is a scarce reserve
	// and one author should not own it. It is wrong as a cap on the *page*,
	// which already has its own limit — `maxPerAuthor` — enforced on the ranked
	// side and applied here across everything.
	//
	// So a short page is topped up in score order from whatever the bucket caps
	// dropped. The feed can still be exhausted (§10 wants that); what it can no
	// longer do is claim exhaustion while holding candidates back.
	page = backfill(page, q.Limit, ranked, explore)

	// Spacing, not filtering — the per-author cap already happened above. Note
	// that this can move an exploration item off its exact slot; the reserved
	// *count* is what the policy is about, and a page where two reels from one
	// vendor sit adjacent reads as "this feed is one laundry shop" regardless of
	// how it got that way.
	return Spread(page, func(i Item) string { return i.AuthorID.String() })
}

// capPerAuthor keeps the best n reels per author, preserving score order.
//
// Applied in Go rather than as a window function in SQL because the score it
// ranks by no longer exists in SQL. The cost is that the candidate query
// returns rows that will be discarded — which is the correct trade: retrieval
// is index work and this is a walk over a few hundred structs.
func capPerAuthor(items []Item, n int) []Item {
	if n <= 0 {
		return nil
	}
	seen := make(map[uuid.UUID]int, len(items))
	out := items[:0:0]
	for _, it := range items {
		if seen[it.AuthorID] >= n {
			continue
		}
		seen[it.AuthorID]++
		out = append(out, it)
	}
	return out
}

// union merges the two exclusion sources, deduplicated.
func union(a, b []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(a)+len(b))
	out := make([]uuid.UUID, 0, len(a)+len(b))
	for _, list := range [][]uuid.UUID{a, b} {
		for _, id := range list {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func profileName(p ranking.Provider) string {
	if named, ok := p.(interface{ Active() string }); ok {
		return named.Active()
	}
	return "static"
}

// decodeRenditions unpacks the aggregated JSON.
//
// A malformed aggregate yields no renditions rather than an error — the client
// falls back to the poster frame, which is a visible degradation the user can
// survive. A 500 on the front door is not.
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

// backfill tops a short page up from candidates the bucket caps dropped.
//
// Only ever *adds*, and only up to `limit`, and never past `maxPerAuthor` for
// any one author. A full page is returned untouched — this is dead weight on
// every request in a healthy district and the whole product in a thin one.
//
// Exploration items keep their flag when they arrive this way. They were still
// chosen without having earned it on score, and mislabelling them would put
// unearned impressions into the measurement of what exploration costs.
func backfill(page []Item, limit int, buckets ...[]Item) []Item {
	if len(page) >= limit {
		return page
	}

	present := make(map[uuid.UUID]bool, len(page))
	perAuthor := make(map[uuid.UUID]int, len(page))
	for _, it := range page {
		present[it.ID] = true
		perAuthor[it.AuthorID]++
	}

	for _, bucket := range buckets {
		for _, it := range bucket {
			if len(page) >= limit {
				return page
			}
			if present[it.ID] || perAuthor[it.AuthorID] >= maxPerAuthor {
				continue
			}
			present[it.ID] = true
			perAuthor[it.AuthorID]++
			page = append(page, it)
		}
	}
	return page
}
