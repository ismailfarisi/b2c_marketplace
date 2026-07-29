package adminview

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The supply read models — PLAN-ADMIN-CONSOLE §4.4 and the reel half of §4.2.
//
// The gap this closes first is the one the plan calls the largest in the console
// that already exists: **a moderator reviewing a reel cannot watch it.** The
// queue shows a caption, a poster key and a report count, and asks for a
// decision on a video nobody can play. Approving on a thumbnail is not
// moderation, it is a coin toss with an audit trail.

// ── reel detail ─────────────────────────────────────────────────────────────

// ReelDetail is everything about one reel, including why it is stuck.
type ReelDetail struct {
	ID          uuid.UUID
	State       string
	Visibility  string
	Caption     string
	Locale      string
	AuthorType  string
	AuthorID    uuid.UUID
	AuthorName  string
	SubjectType string
	SubjectID   *uuid.UUID
	// CategoryID is carried raw. There is deliberately no categories table yet
	// (see migration 14) — inventing a taxonomy in a ranking migration is where
	// taxonomies go to be wrong — so there is nothing to resolve it against and
	// pretending otherwise would be a join to a table that does not exist.
	CategoryID  *uuid.UUID
	DurationMS  int
	CreatedAt   time.Time
	PublishedAt *time.Time
	RemovedAt   *time.Time

	// The pipeline, in the order it runs. These four timestamps are the whole
	// diagnostic: a reel stuck in `transcoding` with `metadata_ready_at` set and
	// nothing after it failed in a different place to one that never probed.
	BytesReadyAt    *time.Time
	MetadataReadyAt *time.Time
	UploadExpiresAt *time.Time
	Attempts        int
	FailureCode     string
	FailureDetail   string

	// Asset carries the source object. Nil for a reel whose upload never
	// completed, which is itself the answer to "why is this draft empty".
	AssetID    *uuid.UUID
	S3Key      string
	PosterKey  string
	PreviewKey string
	Bytes      *int64
	MimeType   string
	Width      *int
	Height     *int
	ProbedAt   *time.Time

	CoverageKind string
	HasCoverage  bool

	Renditions []Rendition
	Reports    int
	Engagement ReelEngagement
}

// Rendition is one transcoded output.
type Rendition struct {
	Label       string
	Width       int
	Height      int
	BitrateKbps int
	PlaylistURL string
}

// ReelEngagement is what the reel earned.
type ReelEngagement struct {
	Views    int64
	Likes    int64
	Comments int64
	Shares   int64
	Saves    int64
}

// LoadReel composes the reel, its asset, its renditions and its counters.
func (s *Service) LoadReel(ctx context.Context, id uuid.UUID) (*ReelDetail, error) {
	var r ReelDetail
	var caption, locale, subjectType, failureCode, failureDetail, coverageKind *string
	var s3Key, posterKey, previewKey, mimeType *string

	err := s.pool.QueryRow(ctx, `
		SELECT r.id, r.state::text, r.visibility, r.caption, r.caption_locale,
		       r.author_type, r.author_id,
		       coalesce(nullif(concat_ws(' · ', v.trade_name, br.name), ''),
		                u.display_name, u.phone, ''),
		       r.subject_type, r.subject_id, r.category_id,
		       coalesce(r.duration_ms, 0), r.created_at, r.published_at, r.removed_at,
		       r.bytes_ready_at, r.metadata_ready_at, r.upload_expires_at,
		       r.attempts, r.failure_code, r.failure_detail,
		       r.coverage_kind, r.coverage IS NOT NULL,
		       a.id, a.s3_key, r.poster_key, r.preview_key,
		       a.bytes, a.mime_type, a.width, a.height, a.probed_at
		  FROM reels r
		  LEFT JOIN media_assets a ON a.id = r.media_asset_id
		  -- A reel is authored by a *branch* or a user, never by a vendor
		  -- directly — the check constraint on reels.author_type allows exactly
		  -- those two. Joining on 'vendor' matches nothing, silently, and
		  -- produces a detail page with a blank author and a supply report of
		  -- zero. Which is how this was found.
		  LEFT JOIN vendor_branches br ON r.author_type = 'vendor_branch' AND br.id = r.author_id
		  LEFT JOIN vendors v ON v.id = br.vendor_id
		  LEFT JOIN users u ON r.author_type = 'user' AND u.id = r.author_id
		 WHERE r.id = $1`, id).
		Scan(&r.ID, &r.State, &r.Visibility, &caption, &locale,
			&r.AuthorType, &r.AuthorID, &r.AuthorName,
			&subjectType, &r.SubjectID, &r.CategoryID,
			&r.DurationMS, &r.CreatedAt, &r.PublishedAt, &r.RemovedAt,
			&r.BytesReadyAt, &r.MetadataReadyAt, &r.UploadExpiresAt,
			&r.Attempts, &failureCode, &failureDetail,
			&coverageKind, &r.HasCoverage,
			&r.AssetID, &s3Key, &posterKey, &previewKey,
			&r.Bytes, &mimeType, &r.Width, &r.Height, &r.ProbedAt)
	if err != nil {
		return nil, notFoundOr("load reel", err)
	}

	r.Caption = deref(caption)
	r.Locale = deref(locale)
	r.SubjectType = deref(subjectType)
	r.FailureCode = deref(failureCode)
	r.FailureDetail = deref(failureDetail)
	r.CoverageKind = deref(coverageKind)
	r.S3Key = deref(s3Key)
	r.PosterKey = deref(posterKey)
	r.PreviewKey = deref(previewKey)
	r.MimeType = deref(mimeType)

	rows, err := s.pool.Query(ctx, `
		SELECT label, width, height, bitrate_kbps, playlist_url
		  FROM reel_renditions WHERE reel_id = $1 ORDER BY height`, id)
	if err != nil {
		return nil, wrap("renditions", err)
	}
	for rows.Next() {
		var rd Rendition
		if err := rows.Scan(&rd.Label, &rd.Width, &rd.Height, &rd.BitrateKbps, &rd.PlaylistURL); err != nil {
			rows.Close()
			return nil, err
		}
		r.Renditions = append(r.Renditions, rd)
	}
	rows.Close()

	// Counters are best-effort: a reel detail page that fails to load because
	// an engagement table is slow is worse than one that shows zeroes. The
	// moderation decision does not depend on them.
	_ = s.pool.QueryRow(ctx, `
		SELECT views, likes, comments, shares, saves
		  FROM reel_counters WHERE reel_id = $1`, id).
		Scan(&r.Engagement.Views, &r.Engagement.Likes,
			&r.Engagement.Comments, &r.Engagement.Shares, &r.Engagement.Saves)

	_ = s.pool.QueryRow(ctx,
		`SELECT count(*) FROM reel_reports WHERE reel_id = $1`, id).
		Scan(&r.Reports)

	return &r, nil
}

// ReelTimeline merges the state history with the moderation decisions.
func (s *Service) ReelTimeline(ctx context.Context, id uuid.UUID) ([]TimelineEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT h.at, 'pipeline', h.to_state::text,
		       coalesce(h.reason, coalesce(h.from_state::text, 'start') || ' → ' || h.to_state::text),
		       coalesce(u.display_name, u.phone, h.actor_type, '')
		  FROM reel_state_history h
		  LEFT JOIN users u ON u.id = h.actor_id
		 WHERE h.reel_id = $1
		UNION ALL
		SELECT a.created_at, 'staff', a.action, coalesce(a.reason, ''),
		       coalesce(su.display_name, su.phone, '')
		  FROM admin_actions a
		  LEFT JOIN users su ON su.id = a.actor_id
		 WHERE a.subject_type = 'reel' AND a.subject_id = $1
		 ORDER BY 1 DESC
		 LIMIT 100`, id)
	if err != nil {
		return nil, wrap("reel timeline", err)
	}
	defer rows.Close()

	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var actor string
		if err := rows.Scan(&e.At, &e.Source, &e.Kind, &e.Detail, &actor); err != nil {
			return nil, err
		}
		e.Actor = actor
		out = append(out, e)
	}
	return out, rows.Err()
}

// MediaRef is what a signed-URL request resolves to before it is signed.
//
// Returned separately from the URL so the handler can decide *whether* to sign,
// and record that it did. Reading media is the most sensitive read in the
// console — it is somebody's unpublished video — and the audit row for it has to
// name the object, not just the reel.
type MediaRef struct {
	ReelID    uuid.UUID
	State     string
	OwnerType string
	OwnerID   uuid.UUID
	S3Key     string
	PosterKey string
	MimeType  string
}

// ResolveReelMedia finds the source object behind a reel.
func (s *Service) ResolveReelMedia(ctx context.Context, reelID uuid.UUID) (*MediaRef, error) {
	var m MediaRef
	var s3Key, posterKey, mimeType *string
	err := s.pool.QueryRow(ctx, `
		SELECT r.id, r.state::text, r.author_type, r.author_id,
		       a.s3_key, r.poster_key, a.mime_type
		  FROM reels r
		  LEFT JOIN media_assets a ON a.id = r.media_asset_id
		 WHERE r.id = $1`, reelID).
		Scan(&m.ReelID, &m.State, &m.OwnerType, &m.OwnerID, &s3Key, &posterKey, &mimeType)
	if err != nil {
		return nil, notFoundOr("resolve reel media", err)
	}
	m.S3Key = deref(s3Key)
	m.PosterKey = deref(posterKey)
	m.MimeType = deref(mimeType)
	return &m, nil
}

// ── vendor supply health ────────────────────────────────────────────────────

// SupplyGap is a vendor who has stopped publishing.
type SupplyGap struct {
	VendorID      uuid.UUID
	Vendor        string
	City          string
	Status        string
	Published     int
	PendingReview int
	LastPublished *time.Time
	// DaysQuiet is null for a vendor who has never published at all — which is a
	// different problem to one that stopped, and merging them into "999 days"
	// would sort the two together and hide the distinction.
	DaysQuiet *int
}

// LoadSupplyGaps is the list the content operator works from.
//
// Active vendors only. A suspended vendor not publishing is not a supply
// problem, it is the suspension working, and putting them in this list means
// the operator chases someone they are supposed to be ignoring.
func (s *Service) LoadSupplyGaps(ctx context.Context, quietDays int, limit int) ([]SupplyGap, error) {
	if quietDays <= 0 {
		quietDays = 14
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx, `
		WITH activity AS (
		    -- Through the branch. Reels are authored by branches, so grouping by
		    -- author_id directly counts per *branch* and a two-branch vendor
		    -- looks half as active as it is.
		    SELECT br.vendor_id,
		           count(*) FILTER (WHERE r.state = 'published') AS published,
		           count(*) FILTER (WHERE r.state IN ('review_pending', 'moderating')) AS pending,
		           max(r.published_at) AS last_published
		      FROM reels r
		      JOIN vendor_branches br ON br.id = r.author_id
		     WHERE r.author_type = 'vendor_branch'
		     GROUP BY br.vendor_id
		)
		SELECT v.id, v.trade_name, coalesce(b.city, ''), v.status,
		       coalesce(a.published, 0), coalesce(a.pending, 0), a.last_published,
		       CASE WHEN a.last_published IS NULL THEN NULL
		            ELSE extract(day FROM now() - a.last_published)::int END
		  FROM vendors v
		  LEFT JOIN activity a ON a.vendor_id = v.id
		  LEFT JOIN LATERAL (
		      SELECT city FROM vendor_branches WHERE vendor_id = v.id
		       ORDER BY created_at LIMIT 1
		  ) b ON true
		 WHERE v.status = 'active' AND v.deleted_at IS NULL
		   AND (a.last_published IS NULL OR a.last_published < now() - make_interval(days => $1))
		 -- Never-published first: a vendor onboarded and never activated is a
		 -- different conversation to one who has gone quiet, and it is the more
		 -- urgent of the two.
		 ORDER BY a.last_published NULLS FIRST, v.trade_name
		 LIMIT $2`, quietDays, limit)
	if err != nil {
		return nil, wrap("supply gaps", err)
	}
	defer rows.Close()

	var out []SupplyGap
	for rows.Next() {
		var g SupplyGap
		if err := rows.Scan(&g.VendorID, &g.Vendor, &g.City, &g.Status,
			&g.Published, &g.PendingReview, &g.LastPublished, &g.DaysQuiet); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ── coverage ────────────────────────────────────────────────────────────────

// Coverage is published reel supply in one area.
type Coverage struct {
	// Emirate then city, which is the administrative hierarchy here and the one
	// a coverage conversation is actually held in.
	Emirate    string
	City       string
	Reels      int
	Vendors    int
	LastPublic *time.Time
}

// LoadCoverage answers "which districts have content".
//
// The content-supply risk from the video-first plan, made visible: a feed in a
// district with four reels shows the same four reels to everyone, and that is
// indistinguishable from a broken ranker until someone counts.
func (s *Service) LoadCoverage(ctx context.Context) ([]Coverage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT coalesce(b.emirate, 'unknown'), coalesce(b.city, 'unknown'),
		       count(DISTINCT r.id), count(DISTINCT v.id), max(r.published_at)
		  FROM vendors v
		  JOIN vendor_branches b ON b.vendor_id = v.id
		  LEFT JOIN reels r ON r.author_type = 'vendor_branch' AND r.author_id = b.id
		       AND r.state = 'published'
		 WHERE v.deleted_at IS NULL
		 GROUP BY b.emirate, b.city
		 ORDER BY 3, 1, 2`)
	if err != nil {
		return nil, wrap("coverage", err)
	}
	defer rows.Close()

	var out []Coverage
	for rows.Next() {
		var c Coverage
		if err := rows.Scan(&c.Emirate, &c.City, &c.Reels, &c.Vendors, &c.LastPublic); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
