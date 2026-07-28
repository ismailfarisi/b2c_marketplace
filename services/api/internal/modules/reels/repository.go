package reels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

// Repository is hand-written pgx, matching the rest of the codebase. sqlc
// generates the read models once the queries stop changing weekly; the pipeline
// queries are still moving.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Reel is the author-facing view. The feed has its own, narrower row type — a
// feed page that carried failure codes and probe output would be shipping the
// vendor's dirty laundry to every customer.
type Reel struct {
	ID            uuid.UUID
	AuthorType    string
	AuthorID      uuid.UUID
	SubjectType   *string
	SubjectID     *uuid.UUID
	MediaAssetID  *uuid.UUID
	Caption       string
	CaptionLocale string
	CategoryID    *uuid.UUID
	DurationMS    *int
	State         State
	Visibility    string
	FailureCode   string
	FailureDetail string
	PosterKey     string
	PreviewKey    string
	LQIP          string
	Tags          []string
	PublishedAt   *time.Time
	CreatedAt     time.Time
	UploadExpires *time.Time
}

type CreateDraftInput struct {
	AuthorType  string
	AuthorID    uuid.UUID
	SubjectType *string
	SubjectID   *uuid.UUID
	Bucket      string
	S3Key       string
	MimeType    string
	UploadTTL   time.Duration
}

// CreateDraft inserts the media asset and the draft reel together.
//
// One transaction because a reel row pointing at no asset is unrecoverable —
// the S3 event that later arrives for the uploaded object has nothing to
// correlate against, and the bytes become an orphan nobody bills anybody for
// and nobody deletes.
func (r *Repository) CreateDraft(ctx context.Context, tx pgx.Tx, in CreateDraftInput) (reelID, assetID uuid.UUID, err error) {
	err = tx.QueryRow(ctx, `
		INSERT INTO media_assets (owner_type, owner_id, s3_key, bucket, mime_type)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		in.AuthorType, in.AuthorID, in.S3Key, in.Bucket, nullableText(in.MimeType),
	).Scan(&assetID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert media asset: %w", err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO reels (author_type, author_id, subject_type, subject_id,
		                   media_asset_id, state, upload_expires_at)
		VALUES ($1, $2, $3, $4, $5, 'draft', now() + make_interval(secs => $6))
		RETURNING id`,
		in.AuthorType, in.AuthorID, in.SubjectType, in.SubjectID, assetID,
		// make_interval rather than a cast from a Go duration string: Postgres
		// does not parse "2h0m0s", and that failure only surfaces at runtime on
		// the upload path.
		in.UploadTTL.Seconds(),
	).Scan(&reelID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert reel: %w", err)
	}

	if err := r.recordTransition(ctx, tx, reelID, nil, StateDraft, ActorAuthor(in.AuthorID.String()), "upload requested"); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return reelID, assetID, nil
}

// LoadSnapshot reads the reel FOR UPDATE together with every precondition
// Advance needs.
//
// One query, not five. The whole point of taking the row lock is that the
// answer must not change between the reads — issuing them separately would
// leave a window where the rendition count is stale relative to the state, and
// that window is exactly where the §1.1 race lives.
func (r *Repository) LoadSnapshot(ctx context.Context, tx pgx.Tx, reelID uuid.UUID) (Snapshot, error) {
	var s Snapshot
	var state string
	var decision *string

	err := tx.QueryRow(ctx, `
		SELECT r.state::text,
		       r.bytes_ready_at    IS NOT NULL,
		       r.metadata_ready_at IS NOT NULL,
		       (a.probed_at IS NOT NULL AND a.duration_ms IS NOT NULL),
		       (SELECT count(*) FROM reel_renditions rr WHERE rr.reel_id = r.id),
		       NOT EXISTS (
		           SELECT 1 FROM moderation_jobs mj
		            WHERE mj.reel_id = r.id AND mj.state IN ('queued', 'running')
		       ),
		       (SELECT md.decision FROM moderation_decisions md
		         WHERE md.reel_id = r.id ORDER BY md.at DESC LIMIT 1),
		       r.coverage IS NOT NULL
		  FROM reels r
		  LEFT JOIN media_assets a ON a.id = r.media_asset_id
		 WHERE r.id = $1
		   FOR NO KEY UPDATE OF r`,
		reelID,
	).Scan(&state, &s.BytesReady, &s.MetadataReady, &s.ProbeOK,
		&s.RenditionCount, &s.ModerationComplete, &decision, &s.CoverageResolved)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, httpx.ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("load reel snapshot: %w", err)
	}

	s.State = State(state)
	if decision != nil {
		s.Decision = *decision
	}
	// A reel with no moderation job scheduled has vacuously "complete"
	// moderation by the NOT EXISTS above. Correct it: no decision means the
	// moderation step has not run, and letting that count as complete would
	// walk an unmoderated reel straight to approved.
	if s.State == StateModerating && s.Decision == "" {
		s.ModerationComplete = false
	}
	return s, nil
}

// Transition writes the state change and its history row, refusing anything the
// graph does not allow.
//
// The `WHERE state = $2` is not belt-and-braces — it is the concurrency
// control. Two jobs that both read `transcoding` and both decide to move to
// `moderating` would otherwise both succeed and enqueue moderation twice.
func (r *Repository) Transition(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, from, to State, actor Actor, reason string) error {
	if err := CanTransition(from, to); err != nil {
		return err
	}
	if from == to {
		return nil
	}

	// `to` is bound twice — once as reel_state and once as text. One parameter
	// carrying two different casts leaves Postgres to deduce a single type for
	// it, and which one it picks is not something to rely on.
	tag, err := tx.Exec(ctx, `
		UPDATE reels
		   SET state        = $3::reel_state,
		       published_at = CASE WHEN $4::text = 'published' THEN now() ELSE published_at END,
		       removed_at   = CASE WHEN $4::text = 'removed'   THEN now() ELSE removed_at   END
		 WHERE id = $1 AND state = $2::reel_state`,
		reelID, string(from), string(to), string(to))
	if err != nil {
		return fmt.Errorf("transition reel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Someone else moved it. At-least-once jobs make this normal, not
		// exceptional — surface it as a conflict and let the caller decide.
		return fmt.Errorf("%w: reel %s is no longer in %s", httpx.ErrConflict, reelID, from)
	}

	return r.recordTransition(ctx, tx, reelID, &from, to, actor, reason)
}

func (r *Repository) recordTransition(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, from *State, to State, actor Actor, reason string) error {
	var fromVal any
	if from != nil {
		fromVal = string(*from)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO reel_state_history (reel_id, from_state, to_state, actor_type, actor_id, reason)
		VALUES ($1, $2::reel_state, $3::reel_state, $4, $5, $6)`,
		reelID, fromVal, string(to), actor.Type, actorUUID(actor), nullableText(reason))
	if err != nil {
		return fmt.Errorf("record reel transition: %w", err)
	}
	return nil
}

// MarkBytesReady is called from the S3 event handler. The event is the trigger
// of record: it fires whether or not the client that uploaded is still alive.
func (r *Repository) MarkBytesReady(ctx context.Context, tx pgx.Tx, assetID uuid.UUID, bytes int64) (uuid.UUID, error) {
	var reelID uuid.UUID
	err := tx.QueryRow(ctx, `
		UPDATE media_assets SET bytes = $2 WHERE id = $1
		RETURNING id`, assetID, bytes).Scan(&reelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httpx.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("update media asset bytes: %w", err)
	}

	err = tx.QueryRow(ctx, `
		UPDATE reels
		   SET bytes_ready_at = COALESCE(bytes_ready_at, now())
		 WHERE media_asset_id = $1
		RETURNING id`, assetID).Scan(&reelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httpx.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("mark bytes ready: %w", err)
	}
	return reelID, nil
}

type MetadataInput struct {
	Caption       string
	CaptionLocale string
	CategoryID    *uuid.UUID
	Tags          []string
	Visibility    string
	SubjectType   *string
	SubjectID     *uuid.UUID
}

// SaveMetadata is idempotent by construction: it overwrites the same columns
// with the same values on a replay, and COALESCE keeps the first
// metadata_ready_at rather than sliding it forward. A client that retries the
// call must not look like a second, later submission.
func (r *Repository) SaveMetadata(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, in MetadataInput) error {
	tag, err := tx.Exec(ctx, `
		UPDATE reels
		   SET caption           = $2,
		       caption_locale    = $3,
		       category_id       = $4,
		       visibility        = $5,
		       subject_type      = COALESCE($6, subject_type),
		       subject_id        = COALESCE($7, subject_id),
		       metadata_ready_at = COALESCE(metadata_ready_at, now())
		 WHERE id = $1
		   AND state IN ('draft', 'uploaded', 'probing', 'transcoding')`,
		reelID, nullableText(in.Caption), nullableText(in.CaptionLocale),
		in.CategoryID, in.Visibility, in.SubjectType, in.SubjectID)
	if err != nil {
		return fmt.Errorf("save reel metadata: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Editing a caption after moderation has passed on the old one would
		// let a clean reel be swapped for a dirty one post-approval. That is
		// the oldest trick in content moderation and the answer is to refuse.
		return fmt.Errorf("%w: metadata can only be set before moderation", httpx.ErrConflict)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM reel_tags WHERE reel_id = $1`, reelID); err != nil {
		return fmt.Errorf("clear reel tags: %w", err)
	}
	for _, t := range in.Tags {
		if _, err := tx.Exec(ctx,
			`INSERT INTO reel_tags (reel_id, tag) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			reelID, t); err != nil {
			return fmt.Errorf("insert reel tag: %w", err)
		}
	}
	return nil
}

// SaveProbe records ffprobe output on the asset and mirrors duration onto the
// reel, where the feed reads it.
func (r *Repository) SaveProbe(ctx context.Context, tx pgx.Tx, assetID uuid.UUID, durationMS, width, height int, mime string, raw any) error {
	body, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal probe: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media_assets
		   SET duration_ms = $2, width = $3, height = $4, mime_type = $5,
		       probe = $6, probed_at = now()
		 WHERE id = $1`,
		assetID, durationMS, width, height, nullableText(mime), body); err != nil {
		return fmt.Errorf("save probe: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE reels SET duration_ms = $2 WHERE media_asset_id = $1`,
		assetID, durationMS); err != nil {
		return fmt.Errorf("mirror duration: %w", err)
	}
	return nil
}

type Rendition struct {
	Label       string
	Width       int
	Height      int
	BitrateKbps int
	PlaylistURL string
}

func (r *Repository) SaveRenditions(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, rs []Rendition, poster, preview, lqip string) error {
	for _, rend := range rs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO reel_renditions (reel_id, label, width, height, bitrate_kbps, playlist_url)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (reel_id, label) DO UPDATE
			   SET width = EXCLUDED.width, height = EXCLUDED.height,
			       bitrate_kbps = EXCLUDED.bitrate_kbps,
			       playlist_url = EXCLUDED.playlist_url`,
			reelID, rend.Label, rend.Width, rend.Height, rend.BitrateKbps, rend.PlaylistURL,
		); err != nil {
			return fmt.Errorf("save rendition %s: %w", rend.Label, err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE reels SET poster_key = $2, preview_key = $3, lqip = $4 WHERE id = $1`,
		reelID, nullableText(poster), nullableText(preview), nullableText(lqip)); err != nil {
		return fmt.Errorf("save reel artwork: %w", err)
	}
	return nil
}

// ResolveCoverage runs the §4.1 resolution. False means the branch has no
// service area — a publishable reel with no audience, which the caller must
// treat as a failure rather than publish into a void.
func (r *Repository) ResolveCoverage(ctx context.Context, tx pgx.Tx, reelID uuid.UUID) (bool, error) {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT resolve_reel_coverage($1)`, reelID).Scan(&ok); err != nil {
		return false, fmt.Errorf("resolve coverage: %w", err)
	}
	return ok, nil
}

func (r *Repository) Get(ctx context.Context, reelID uuid.UUID) (*Reel, error) {
	var v Reel
	var state string
	err := r.pool.QueryRow(ctx, `
		SELECT r.id, r.author_type, r.author_id, r.subject_type, r.subject_id,
		       r.media_asset_id, COALESCE(r.caption, ''), COALESCE(r.caption_locale, ''),
		       r.category_id, r.duration_ms, r.state::text, r.visibility,
		       COALESCE(r.failure_code, ''), COALESCE(r.failure_detail, ''),
		       COALESCE(r.poster_key, ''), COALESCE(r.preview_key, ''), COALESCE(r.lqip, ''),
		       COALESCE(ARRAY(SELECT tag FROM reel_tags t WHERE t.reel_id = r.id ORDER BY tag), '{}'),
		       r.published_at, r.created_at, r.upload_expires_at
		  FROM reels r
		 WHERE r.id = $1`, reelID,
	).Scan(&v.ID, &v.AuthorType, &v.AuthorID, &v.SubjectType, &v.SubjectID,
		&v.MediaAssetID, &v.Caption, &v.CaptionLocale, &v.CategoryID, &v.DurationMS,
		&state, &v.Visibility, &v.FailureCode, &v.FailureDetail,
		&v.PosterKey, &v.PreviewKey, &v.LQIP, &v.Tags,
		&v.PublishedAt, &v.CreatedAt, &v.UploadExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get reel: %w", err)
	}
	v.State = State(state)
	return &v, nil
}

// AuthorizeBranch answers "may this user act as this branch".
//
// A NULL branch_id on the membership means every branch of the vendor; a named
// one scopes a staff member to a single facility.
func (r *Repository) AuthorizeBranch(ctx context.Context, userID, branchID uuid.UUID) error {
	var allowed bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1
		      FROM vendor_users vu
		      JOIN vendor_branches b ON b.vendor_id = vu.vendor_id
		     WHERE vu.user_id = $1
		       AND b.id = $2
		       AND b.deleted_at IS NULL
		       AND (vu.branch_id IS NULL OR vu.branch_id = b.id)
		)`, userID, branchID).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("authorize branch: %w", err)
	}
	if !allowed {
		// Forbidden, not NotFound: the caller supplied a branch ID they already
		// know exists, so hiding it buys nothing and confuses the vendor
		// dashboard's error handling.
		return httpx.ErrForbidden
	}
	return nil
}

func (r *Repository) InsertReport(ctx context.Context, reelID, reporterID uuid.UUID, reason, detail string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO reel_reports (reel_id, reporter_id, reason, detail)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (reel_id, reporter_id) DO NOTHING`,
		reelID, reporterID, reason, nullableText(detail))
	if err != nil {
		return fmt.Errorf("insert reel report: %w", err)
	}
	return nil
}

// RecordFailure stores why a pipeline step gave up. Without it a `failed` reel
// is indistinguishable from a reel the pipeline forgot about.
func (r *Repository) RecordFailure(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, code, detail string) error {
	_, err := tx.Exec(ctx, `
		UPDATE reels SET failure_code = $2, failure_detail = $3, attempts = attempts + 1
		 WHERE id = $1`, reelID, code, nullableText(detail))
	if err != nil {
		return fmt.Errorf("record reel failure: %w", err)
	}
	return nil
}

func (r *Repository) InsertOutbox(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, eventType string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload, idempotency_key)
		VALUES ('reel', $1, $2, $3, $4)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		reelID, eventType, body, fmt.Sprintf("reel:%s:%s", reelID, eventType))
	if err != nil {
		return fmt.Errorf("insert reel outbox event: %w", err)
	}
	return nil
}

func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func actorUUID(a Actor) any {
	if a.ID == nil {
		return nil
	}
	id, err := uuid.Parse(*a.ID)
	if err != nil {
		return nil
	}
	return id
}
