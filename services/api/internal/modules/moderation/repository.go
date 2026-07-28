package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// StartJob claims the layer for this reel.
//
// ON CONFLICT resets rather than inserting a second row: the unique constraint
// on (reel_id, layer) means a retry supersedes its predecessor, so the decision
// step never has to work out which of three 'poster' rows is the live one.
func (r *Repository) StartJob(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, layer, provider string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO moderation_jobs (reel_id, layer, provider, state, started_at)
		VALUES ($1, $2, $3, 'running', now())
		ON CONFLICT (reel_id, layer) DO UPDATE
		   SET provider = EXCLUDED.provider,
		       state = 'running',
		       started_at = now(),
		       finished_at = NULL,
		       error_detail = NULL
		RETURNING id`,
		reelID, layer, provider).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("start moderation job: %w", err)
	}
	return id, nil
}

func (r *Repository) FinishJob(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, layerErr error) error {
	state, detail := "done", ""
	if layerErr != nil {
		state, detail = "error", layerErr.Error()
	}
	_, err := r.exec(ctx, tx, `
		UPDATE moderation_jobs
		   SET state = $2, finished_at = now(), error_detail = $3
		 WHERE id = $1`, jobID, state, nullableText(detail))
	if err != nil {
		return fmt.Errorf("finish moderation job: %w", err)
	}
	return nil
}

// SaveLabels stores every raw detection, including the ones that crossed
// nothing. §3.2 calls for tuning these thresholds against the review queue's
// overturn rate, and that is only possible against labels that were kept.
func (r *Repository) SaveLabels(ctx context.Context, tx pgx.Tx, jobID, reelID uuid.UUID, labels []Label, raw any) error {
	if len(labels) == 0 {
		return nil
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal moderation raw: %w", err)
	}
	for _, l := range labels {
		if _, err := tx.Exec(ctx, `
			INSERT INTO moderation_labels (job_id, reel_id, category, confidence, timestamp_ms, raw)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			jobID, reelID, l.Category, l.Confidence, nullableInt(l.TimestampMS), body,
		); err != nil {
			return fmt.Errorf("save moderation label: %w", err)
		}
	}
	return nil
}

func (r *Repository) SaveDecision(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, d Decision, by string, moderatorID *uuid.UUID, notes string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO moderation_decisions (reel_id, decision, decided_by, moderator_id, categories, notes)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		reelID, string(d.Verdict), by, moderatorID, d.Categories, nullableText(notes))
	if err != nil {
		return fmt.Errorf("save moderation decision: %w", err)
	}
	return nil
}

// LoadTrust reads the author's standing, defaulting a missing row to a fresh
// author rather than erroring. A first-time poster legitimately has no row.
func (r *Repository) LoadTrust(ctx context.Context, tx pgx.Tx, authorType string, authorID uuid.UUID) (TrustState, error) {
	var s TrustState
	var tier string
	err := tx.QueryRow(ctx, `
		SELECT tier, published_count, strike_count, last_strike_at
		  FROM author_trust
		 WHERE author_type = $1 AND author_id = $2`,
		authorType, authorID).Scan(&tier, &s.PublishedCount, &s.StrikeCount, &s.LastStrikeAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrustState{Tier: TierNew}, nil
	}
	if err != nil {
		return TrustState{}, fmt.Errorf("load author trust: %w", err)
	}
	s.Tier = Tier(tier)
	return s, nil
}

// RecordPublished increments the author's published count and recomputes the
// tier from it, so the stored tier can never disagree with the counts that
// justify it.
func (r *Repository) RecordPublished(ctx context.Context, tx pgx.Tx, authorType string, authorID uuid.UUID, now time.Time) error {
	var s TrustState
	var tier string
	err := tx.QueryRow(ctx, `
		INSERT INTO author_trust (author_type, author_id, published_count)
		VALUES ($1, $2, 1)
		ON CONFLICT (author_type, author_id) DO UPDATE
		   SET published_count = author_trust.published_count + 1,
		       updated_at = now()
		RETURNING tier, published_count, strike_count, last_strike_at`,
		authorType, authorID).Scan(&tier, &s.PublishedCount, &s.StrikeCount, &s.LastStrikeAt)
	if err != nil {
		return fmt.Errorf("record published: %w", err)
	}
	return r.setTier(ctx, tx, authorType, authorID, ComputeTier(s, now))
}

// RecordStrike is called when a rejection is upheld — by the automated gate or
// by a moderator sustaining a report.
func (r *Repository) RecordStrike(ctx context.Context, tx pgx.Tx, authorType string, authorID uuid.UUID, now time.Time) error {
	var s TrustState
	var tier string
	err := tx.QueryRow(ctx, `
		INSERT INTO author_trust (author_type, author_id, strike_count, last_strike_at)
		VALUES ($1, $2, 1, now())
		ON CONFLICT (author_type, author_id) DO UPDATE
		   SET strike_count = author_trust.strike_count + 1,
		       last_strike_at = now(),
		       updated_at = now()
		RETURNING tier, published_count, strike_count, last_strike_at`,
		authorType, authorID).Scan(&tier, &s.PublishedCount, &s.StrikeCount, &s.LastStrikeAt)
	if err != nil {
		return fmt.Errorf("record strike: %w", err)
	}
	return r.setTier(ctx, tx, authorType, authorID, ComputeTier(s, now))
}

func (r *Repository) setTier(ctx context.Context, tx pgx.Tx, authorType string, authorID uuid.UUID, tier Tier) error {
	_, err := tx.Exec(ctx, `
		UPDATE author_trust SET tier = $3, updated_at = now()
		 WHERE author_type = $1 AND author_id = $2`,
		authorType, authorID, string(tier))
	if err != nil {
		return fmt.Errorf("set author tier: %w", err)
	}
	return nil
}

// QueueItem is one row of the review queue.
type QueueItem struct {
	ReelID      uuid.UUID
	AuthorType  string
	AuthorID    uuid.UUID
	Caption     string
	PosterKey   string
	Categories  []string
	Tier        string
	WaitingFor  time.Duration
	ReportCount int
	EnteredAt   time.Time
}

// Queue returns pending reviews, oldest first.
//
// Oldest-first and not newest-first, and not by severity: the queue exists to
// meet a two-hour SLA, and any ordering that lets an item be overtaken lets one
// item sit forever. Severity belongs in staffing, not in ordering.
func (r *Repository) Queue(ctx context.Context, limit int) ([]QueueItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.author_type, r.author_id, COALESCE(r.caption, ''),
		       COALESCE(r.poster_key, ''),
		       COALESCE(d.categories, '{}'),
		       COALESCE(t.tier, 'new'),
		       COALESCE(rep.open_count, 0),
		       entered.at AS entered_at
		  FROM reels r
		  LEFT JOIN author_trust t
		         ON t.author_type = r.author_type AND t.author_id = r.author_id
		  LEFT JOIN LATERAL (
		       SELECT md.categories FROM moderation_decisions md
		        WHERE md.reel_id = r.id ORDER BY md.at DESC LIMIT 1
		  ) d ON true
		  LEFT JOIN LATERAL (
		       SELECT count(*) AS open_count FROM reel_reports rr
		        WHERE rr.reel_id = r.id AND rr.state = 'open'
		  ) rep ON true
		  JOIN LATERAL (
		       SELECT COALESCE(max(h.at), r.created_at) AS at
		         FROM reel_state_history h
		        WHERE h.reel_id = r.id AND h.to_state = 'review_pending'
		  ) entered ON true
		 WHERE r.state = 'review_pending'
		 ORDER BY entered_at ASC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("moderation queue: %w", err)
	}
	defer rows.Close()

	var out []QueueItem
	now := time.Now()
	for rows.Next() {
		var it QueueItem
		var reportCount int64
		if err := rows.Scan(&it.ReelID, &it.AuthorType, &it.AuthorID, &it.Caption,
			&it.PosterKey, &it.Categories, &it.Tier, &reportCount, &it.EnteredAt); err != nil {
			return nil, fmt.Errorf("scan queue item: %w", err)
		}
		it.ReportCount = int(reportCount)
		it.WaitingFor = now.Sub(it.EnteredAt)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *Repository) exec(ctx context.Context, tx pgx.Tx, sql string, args ...any) (int64, error) {
	if tx != nil {
		tag, err := tx.Exec(ctx, sql, args...)
		return tag.RowsAffected(), err
	}
	tag, err := r.pool.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

// ResolveReports closes every open report on a reel once a moderator has ruled
// on it. Leaving them open would keep the reel in the reported list forever and
// make the report count meaningless.
func (r *Repository) ResolveReports(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, upheld bool, moderatorID uuid.UUID) error {
	state := "dismissed"
	if upheld {
		state = "upheld"
	}
	_, err := tx.Exec(ctx, `
		UPDATE reel_reports
		   SET state = $2, resolved_by = $3, resolved_at = now()
		 WHERE reel_id = $1 AND state = 'open'`,
		reelID, state, moderatorID)
	if err != nil {
		return fmt.Errorf("resolve reports: %w", err)
	}
	return nil
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
