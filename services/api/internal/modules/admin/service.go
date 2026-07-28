// Package admin is disputes and the audit trail.
//
// Two things that look like plumbing and are not. A marketplace's real product
// at Phase 7 is not the happy path — it is what happens when the food arrives
// cold, the driver never came, or a vendor's payout is AED 43 short. Every one
// of those is a conversation, and a conversation with no record is a
// conversation both parties remember differently.
package admin

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
	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// RequireOps checks platform staff membership on every privileged call.
//
// A table lookup, not a token claim, for the reason Phase 2 gave when
// `platform_admins` was created: revoking someone must take effect immediately,
// and a claim stays valid until the access token expires.
func (s *Service) RequireOps(ctx context.Context, userID uuid.UUID) error {
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM platform_admins WHERE user_id = $1`, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("check admin: %w", err)
	}
	if role != "ops" && role != "superadmin" {
		// A moderator judges content. Refunding money and suspending vendors is
		// a different job with a different blast radius, and one role covering
		// both is how a content moderator ends up able to move cash.
		return fmt.Errorf("%w: this action needs ops or superadmin, you are %s",
			httpx.ErrForbidden, role)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// The audit trail
// ─────────────────────────────────────────────────────────────────────────────

type Action struct {
	ActorID     uuid.UUID
	Action      string
	SubjectType string
	SubjectID   uuid.NullUUID
	// Reason is required by the database, not just by this struct. A refund with
	// no stated reason is indistinguishable from an unauthorised one six months
	// later, and the moment to capture it is the moment it is decided.
	Reason    string
	Before    any
	After     any
	RequestID string
}

// Record writes an audit row inside the caller's transaction.
//
// Taking a pgx.Tx is the point, the same one the ledger makes: an audit entry
// that commits separately from the action it records is worse than no audit
// entry, because it looks authoritative while disagreeing with what happened.
func (s *Service) Record(ctx context.Context, tx pgx.Tx, a Action) error {
	if a.Reason == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "every privileged action needs a stated reason",
		}}}
	}

	before, err := marshalOrNil(a.Before)
	if err != nil {
		return err
	}
	after, err := marshalOrNil(a.After)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO admin_actions
			(actor_id, action, subject_type, subject_id, reason, before_state, after_state, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''))`,
		a.ActorID, a.Action, a.SubjectType, a.SubjectID, a.Reason,
		before, after, a.RequestID,
	); err != nil {
		return fmt.Errorf("record admin action: %w", err)
	}
	return nil
}

func marshalOrNil(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal audit state: %w", err)
	}
	return b, nil
}

// History is who did what to a thing.
type AuditEntry struct {
	ActorID   uuid.UUID
	ActorName string
	Action    string
	Reason    string
	CreatedAt time.Time
}

func (s *Service) History(ctx context.Context, subjectType string, subjectID uuid.UUID) ([]AuditEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.actor_id, COALESCE(u.display_name, u.phone), a.action, a.reason, a.created_at
		  FROM admin_actions a
		  JOIN users u ON u.id = a.actor_id
		 WHERE a.subject_type = $1 AND a.subject_id = $2
		 ORDER BY a.created_at DESC
		 LIMIT 200`, subjectType, subjectID)
	if err != nil {
		return nil, fmt.Errorf("load audit history: %w", err)
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ActorID, &e.ActorName, &e.Action, &e.Reason, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Disputes
// ─────────────────────────────────────────────────────────────────────────────

// SLA is how long ops has, by dispute kind.
//
// Written down as a table rather than left to a queue that is worked in
// whatever order it appears. The three at four hours are the ones where the
// customer is still waiting for something: everything else is money, and money
// can wait a day without anyone going hungry.
var SLA = map[string]time.Duration{
	"not_delivered":       4 * time.Hour,
	"damaged":             4 * time.Hour,
	"wrong_item":          4 * time.Hour,
	"late":                24 * time.Hour,
	"quality":             24 * time.Hour,
	"driver_conduct":      24 * time.Hour,
	"charged_incorrectly": 48 * time.Hour,
	"payout_amount":       72 * time.Hour,
	"other":               48 * time.Hour,
}

const DefaultSLA = 48 * time.Hour

func DueBy(kind string, now time.Time) time.Time {
	d, ok := SLA[kind]
	if !ok {
		d = DefaultSLA
	}
	return now.Add(d)
}

type OpenInput struct {
	SubjectType  string
	SubjectID    uuid.UUID
	RaisedByType string
	RaisedByID   uuid.UUID
	Kind         string
	Body         string
}

type Dispute struct {
	ID          uuid.UUID
	ReferenceNo string
	SubjectType string
	SubjectID   uuid.UUID
	Kind        string
	State       string
	DueAt       *time.Time
	AssignedTo  uuid.NullUUID
	Resolution  string
	CreatedAt   time.Time
}

// Open raises a dispute and stamps its clock.
//
// The due time is set here from the kind, rather than left null and filled in by
// whoever picks it up. A dispute with no due time is one nobody is late on, and
// a queue where nothing is ever late is a queue nobody works.
func (s *Service) Open(ctx context.Context, in OpenInput) (*Dispute, error) {
	if in.Kind == "" {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "kind", Code: "required",
		}}}
	}

	var d Dispute
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO disputes
				(reference_no, subject_type, subject_id, raised_by_type, raised_by_id, kind, due_at)
			VALUES (generate_reference('DSP'), $1, $2, $3, $4, $5, $6)
			RETURNING id, reference_no, subject_type, subject_id, kind, state, due_at, assigned_to, created_at`,
			in.SubjectType, in.SubjectID, in.RaisedByType, in.RaisedByID, in.Kind,
			DueBy(in.Kind, time.Now()),
		).Scan(&d.ID, &d.ReferenceNo, &d.SubjectType, &d.SubjectID, &d.Kind,
			&d.State, &d.DueAt, &d.AssignedTo, &d.CreatedAt)
		if err != nil {
			return fmt.Errorf("open dispute: %w", err)
		}

		if in.Body != "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO dispute_events (dispute_id, actor_type, actor_id, kind, body)
				VALUES ($1, $2, $3, 'message', $4)`,
				d.ID, in.RaisedByType, in.RaisedByID, in.Body); err != nil {
				return fmt.Errorf("record dispute message: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Resolve closes a dispute with a stated outcome.
//
// `resolution` comes from a closed list and is separate from the free-text note,
// because "how often do we refund" is a question ops asks weekly and a note
// cannot answer it. The audit row is written in the same transaction — a refund
// decision and the record of who made it must not be able to come apart.
func (s *Service) Resolve(ctx context.Context, disputeID, actorID uuid.UUID,
	resolution string, amountMinor int64, note, requestID string) error {

	if !validResolutions[resolution] {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "resolution", Code: "invalid",
			Message: "one of refunded, partially_refunded, redelivered, credited, no_action, vendor_charged, driver_penalised",
		}}}
	}
	if note == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "note", Code: "required",
			Message: "a resolution needs a reason someone can read back",
		}}}
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var previous string
		if err := tx.QueryRow(ctx,
			`SELECT state FROM disputes WHERE id = $1 FOR UPDATE`, disputeID).Scan(&previous); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load dispute: %w", err)
		}
		if previous == "resolved" || previous == "rejected" {
			return fmt.Errorf("%w: this dispute is already %s", httpx.ErrConflict, previous)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE disputes
			   SET state = 'resolved', resolution = $2,
			       resolution_amount_minor = NULLIF($3, 0), resolution_note = $4,
			       resolved_by = $5, resolved_at = now()
			 WHERE id = $1`,
			disputeID, resolution, amountMinor, note, actorID); err != nil {
			return fmt.Errorf("resolve dispute: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO dispute_events
				(dispute_id, actor_type, actor_id, kind, body, from_state, to_state)
			VALUES ($1, 'admin', $2, 'state_change', $3, $4, 'resolved')`,
			disputeID, actorID, note, previous); err != nil {
			return fmt.Errorf("record dispute event: %w", err)
		}

		return s.Record(ctx, tx, Action{
			ActorID:     actorID,
			Action:      "dispute.resolve",
			SubjectType: "dispute",
			SubjectID:   uuid.NullUUID{UUID: disputeID, Valid: true},
			Reason:      note,
			Before:      map[string]any{"state": previous},
			After: map[string]any{
				"state": "resolved", "resolution": resolution, "amount_minor": amountMinor,
			},
			RequestID: requestID,
		})
	})
}

var validResolutions = map[string]bool{
	"refunded": true, "partially_refunded": true, "redelivered": true,
	"credited": true, "no_action": true, "vendor_charged": true, "driver_penalised": true,
}

// Queue is the ops worklist: overdue first, then closest to due.
//
// Ordered by the clock rather than by age. An eight-hour-old payout query with a
// 72-hour SLA is not more urgent than a one-hour-old "it never arrived" with a
// four-hour one, and a queue sorted by age says it is.
func (s *Service) Queue(ctx context.Context, limit int) ([]Dispute, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, reference_no, subject_type, subject_id, kind, state, due_at,
		       assigned_to, COALESCE(resolution, ''), created_at
		  FROM disputes
		 WHERE state IN ('open', 'investigating', 'awaiting_response')
		 ORDER BY due_at NULLS LAST
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("load dispute queue: %w", err)
	}
	defer rows.Close()

	var out []Dispute
	for rows.Next() {
		var d Dispute
		if err := rows.Scan(&d.ID, &d.ReferenceNo, &d.SubjectType, &d.SubjectID,
			&d.Kind, &d.State, &d.DueAt, &d.AssignedTo, &d.Resolution, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan dispute: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Assign puts a name against a dispute.
func (s *Service) Assign(ctx context.Context, disputeID, assigneeID, actorID uuid.UUID) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE disputes SET assigned_to = $2, state = 'investigating'
			 WHERE id = $1 AND state IN ('open', 'investigating', 'awaiting_response')`,
			disputeID, assigneeID)
		if err != nil {
			return fmt.Errorf("assign dispute: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: this dispute is closed", httpx.ErrConflict)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO dispute_events (dispute_id, actor_type, actor_id, kind, to_state)
			VALUES ($1, 'admin', $2, 'state_change', 'investigating')`, disputeID, actorID)
		return err
	})
}
