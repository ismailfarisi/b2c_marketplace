package trust

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Raise records one piece of evidence.
//
// It satisfies both vehicles.Signals and chat.Signals — the same three-argument
// sentence, written once. Idempotent against the partial unique index on open
// signals, so a detector that re-runs nightly does not produce a hundred
// identical rows for the same listing and drive its score to zero through sheer
// repetition. That failure mode is not hypothetical: it is what an additive
// model does when the writer is not idempotent.
func (s *Service) Raise(ctx context.Context, tx pgx.Tx, subjectType string, subjectID uuid.UUID,
	kind string, severity int, detail map[string]any) error {

	var payload []byte
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("marshal fraud detail: %w", err)
		}
		payload = b
	}
	if severity <= 0 {
		severity = 50
	}
	if severity > 100 {
		severity = 100
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO fraud_signals (subject_type, subject_id, kind, severity, source, detail)
		VALUES ($1, $2, $3, $4, 'system', $5)
		ON CONFLICT (subject_type, subject_id, kind) WHERE state = 'open'
		DO NOTHING`,
		subjectType, subjectID, kind, severity, payload,
	); err != nil {
		return fmt.Errorf("raise fraud signal: %w", err)
	}
	return nil
}

// RaiseReport is the buyer-facing path. Separate from Raise because its source
// is 'report', and a signal a person raised is treated differently from one a
// query did: reports are never auto-dismissed by a detector re-run.
func (s *Service) RaiseReport(ctx context.Context, subjectType string, subjectID, reporterID uuid.UUID, reason string) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO fraud_signals (subject_type, subject_id, kind, severity, source, detail)
			VALUES ($1, $2, 'buyer_report', 50, 'report', $3)
			ON CONFLICT (subject_type, subject_id, kind) WHERE state = 'open'
			DO UPDATE SET
				-- A second person reporting the same listing is more evidence,
				-- not the same evidence. Severity climbs; the row stays one row
				-- so the reviewer sees one item rather than a flood.
				severity = LEAST(100, fraud_signals.severity + 15)`,
			subjectType, subjectID, mustJSON(map[string]any{
				"reporter_id": reporterID.String(), "reason": reason,
			}),
		); err != nil {
			return fmt.Errorf("raise report: %w", err)
		}
		return nil
	})
}

// Load reads the evidence for a subject.
//
// For a user it also reads the signals on that user's listings. A seller with
// five listings each carrying one mild signal is the pattern being looked for,
// and scoring the user in isolation from their inventory would miss it
// completely — each listing looks fine, and the account is the fraud.
func (s *Service) Load(ctx context.Context, subjectType string, subjectID uuid.UUID) ([]Signal, error) {
	sql := `
		SELECT kind, severity, state, created_at
		  FROM fraud_signals
		 WHERE subject_type = $1 AND subject_id = $2
		   AND created_at > now() - $3::interval`
	if subjectType == "user" {
		sql = `
			SELECT f.kind, f.severity, f.state, f.created_at
			  FROM fraud_signals f
			 WHERE f.created_at > now() - $3::interval
			   AND (
			        (f.subject_type = $1 AND f.subject_id = $2)
			     OR (f.subject_type = 'listing' AND f.subject_id IN (
			            SELECT id FROM vehicle_listings WHERE seller_id = $2))
			     OR (f.subject_type = 'conversation' AND f.subject_id IN (
			            SELECT id FROM conversations WHERE seller_id = $2 OR buyer_id = $2))
			   )`
	}

	rows, err := s.pool.Query(ctx, sql, subjectType, subjectID, Window.String())
	if err != nil {
		return nil, fmt.Errorf("load fraud signals: %w", err)
	}
	defer rows.Close()

	var out []Signal
	for rows.Next() {
		var sig Signal
		if err := rows.Scan(&sig.Kind, &sig.Severity, &sig.State, &sig.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan fraud signal: %w", err)
		}
		out = append(out, sig)
	}
	return out, rows.Err()
}

// Rescore recomputes and stores. Never increments: the stored row is a cache of
// the fold, and deleting the whole table would cost only the time to rebuild it.
func (s *Service) Rescore(ctx context.Context, subjectType string, subjectID uuid.UUID) (int, Band, error) {
	signals, err := s.Load(ctx, subjectType, subjectID)
	if err != nil {
		return 0, "", err
	}
	score, band := Score(signals, time.Now())

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO trust_scores (subject_type, subject_id, score, band, signal_count, computed_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (subject_type, subject_id) DO UPDATE
		   SET score = EXCLUDED.score, band = EXCLUDED.band,
		       signal_count = EXCLUDED.signal_count, computed_at = now()`,
		subjectType, subjectID, score, string(band), len(signals),
	); err != nil {
		return 0, "", fmt.Errorf("store trust score: %w", err)
	}
	return score, band, nil
}

// RescoreDirty refreshes subjects whose evidence has changed since their score
// was computed, plus subjects whose score has simply gone stale.
//
// The second half matters more than it looks. Scores decay with time and
// nothing writes a signal when that happens, so a purely event-driven refresh
// would leave a seller restricted forever by evidence that has long since
// stopped counting — with no event to notice it.
func (s *Service) RescoreDirty(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.subject_type, f.subject_id
		  FROM (
		      SELECT DISTINCT subject_type, subject_id, max(created_at) AS latest
		        FROM fraud_signals
		       WHERE created_at > now() - $2::interval
		       GROUP BY subject_type, subject_id
		  ) f
		  LEFT JOIN trust_scores t
		         ON t.subject_type = f.subject_type AND t.subject_id = f.subject_id
		 WHERE t.computed_at IS NULL
		    OR t.computed_at < f.latest
		    OR t.computed_at < now() - interval '24 hours'
		 ORDER BY t.computed_at NULLS FIRST
		 LIMIT $1`, limit, Window.String())
	if err != nil {
		return 0, fmt.Errorf("find dirty subjects: %w", err)
	}

	type subject struct {
		kind string
		id   uuid.UUID
	}
	var subjects []subject
	for rows.Next() {
		var sub subject
		if err := rows.Scan(&sub.kind, &sub.id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan dirty subject: %w", err)
		}
		subjects = append(subjects, sub)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	// Listing signals move their seller's score too, so every listing rescored
	// here drags its owner along. Collected rather than rescored inline so a
	// seller with forty flagged listings is rescored once.
	sellers := map[uuid.UUID]bool{}
	done := 0
	for _, sub := range subjects {
		if _, _, err := s.Rescore(ctx, sub.kind, sub.id); err != nil {
			return done, err
		}
		done++
		if sub.kind == "listing" {
			var sellerID uuid.UUID
			if err := s.pool.QueryRow(ctx,
				`SELECT seller_id FROM vehicle_listings WHERE id = $1`, sub.id).Scan(&sellerID); err == nil {
				sellers[sellerID] = true
			}
		}
	}
	for sellerID := range sellers {
		if _, _, err := s.Rescore(ctx, "user", sellerID); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// Queue is the reviewer's worklist: open evidence, worst subjects first.
type QueueItem struct {
	SubjectType string
	SubjectID   uuid.UUID
	Kind        string
	Severity    int
	Detail      []byte
	Score       int
	Band        string
	CreatedAt   time.Time
}

func (s *Service) Queue(ctx context.Context, limit int) ([]QueueItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.subject_type, f.subject_id, f.kind, f.severity, f.detail,
		       COALESCE(t.score, 100), COALESCE(t.band, 'ok'), f.created_at
		  FROM fraud_signals f
		  LEFT JOIN trust_scores t
		         ON t.subject_type = f.subject_type AND t.subject_id = f.subject_id
		 WHERE f.state = 'open'
		 -- Worst subject first, then oldest evidence. Ordering by age alone
		 -- buries a stolen-vehicle hit behind three hundred chat_phone rows.
		 ORDER BY COALESCE(t.score, 100) ASC, f.created_at ASC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("load fraud queue: %w", err)
	}
	defer rows.Close()

	var out []QueueItem
	for rows.Next() {
		var q QueueItem
		if err := rows.Scan(&q.SubjectType, &q.SubjectID, &q.Kind, &q.Severity,
			&q.Detail, &q.Score, &q.Band, &q.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan fraud queue: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// Decide is the moderator's judgement on one signal.
func (s *Service) Decide(ctx context.Context, subjectType string, subjectID uuid.UUID,
	kind string, moderatorID uuid.UUID, confirm bool) error {

	state := "dismissed"
	if confirm {
		state = "confirmed"
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE fraud_signals
		   SET state = $4, resolved_by = $5, resolved_at = now()
		 WHERE subject_type = $1 AND subject_id = $2 AND kind = $3 AND state = 'open'`,
		subjectType, subjectID, kind, state, moderatorID,
	); err != nil {
		return fmt.Errorf("decide fraud signal: %w", err)
	}
	_, _, err := s.Rescore(ctx, subjectType, subjectID)
	return err
}

func mustJSON(v map[string]any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}
