// Package support is customer help tickets: customers open and reply from the
// apps or web, staff answer from the admin console.
package support

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/notify"
)

const (
	MaxSubject = 200
	MaxBody    = 4000
)

type Notifier interface {
	Enqueue(ctx context.Context, tx pgx.Tx, m notify.Message) (uuid.UUID, error)
}

type Service struct {
	pool     *pgxpool.Pool
	notifier Notifier
}

// NewService takes an optional notifier (nil: staff replies are not pushed).
func NewService(pool *pgxpool.Pool, n Notifier) *Service { return &Service{pool: pool, notifier: n} }

type Ticket struct {
	ID             uuid.UUID  `json:"id"`
	Subject        string     `json:"subject"`
	Status         string     `json:"status"`
	SubscriptionID *uuid.UUID `json:"subscription_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	// Staff views only.
	UserID *uuid.UUID `json:"user_id,omitempty"`
	Phone  string     `json:"phone,omitempty"`
}

type Message struct {
	ID        uuid.UUID `json:"id"`
	FromStaff bool      `json:"from_staff"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type Thread struct {
	Ticket
	Messages []Message `json:"messages"`
}

func invalid(field, code, msg string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: code, Message: msg}}}
}

func clean(field, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return s, invalid(field, "required", "cannot be empty")
	}
	if len([]rune(s)) > max {
		return s, invalid(field, "too_long", fmt.Sprintf("at most %d characters", max))
	}
	return s, nil
}

// Open starts a ticket with its first message. subscriptionID, if given, must
// be the customer's own.
func (s *Service) Open(ctx context.Context, userID uuid.UUID, subject, body string, subscriptionID *uuid.UUID) (*Thread, error) {
	subject, err := clean("subject", subject, MaxSubject)
	if err != nil {
		return nil, err
	}
	if body, err = clean("body", body, MaxBody); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if subscriptionID != nil {
		var mine bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM subscriptions WHERE id = $1 AND user_id = $2)`,
			*subscriptionID, userID).Scan(&mine); err != nil {
			return nil, err
		}
		if !mine {
			return nil, invalid("subscription_id", "not_found", "not one of your subscriptions")
		}
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO support_tickets (user_id, subject, subscription_id)
		VALUES ($1, $2, $3) RETURNING id`, userID, subject, subscriptionID).Scan(&id); err != nil {
		return nil, fmt.Errorf("support: open: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO support_messages (ticket_id, author_id, from_staff, body)
		VALUES ($1, $2, false, $3)`, id, userID, body); err != nil {
		return nil, fmt.Errorf("support: open: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Thread(ctx, id, &userID)
}

// minePageSize is how many tickets one page of the customer's list holds
// (a var so tests can page through a handful).
var minePageSize = 50

// Mine lists a customer's tickets, most recent activity first, one page at a
// time: before (nil: the first page) is the cursor of the previous page's
// last row, and next is set while there are older tickets.
func (s *Service) Mine(ctx context.Context, userID uuid.UUID, before *httpx.KeysetCursor) (
	items []Ticket, next *httpx.KeysetCursor, err error,
) {
	var at *time.Time
	var id *uuid.UUID
	if before != nil {
		at, id = &before.At, &before.ID
	}
	items, err = s.list(ctx, `WHERE t.user_id = $1
		AND ($2::timestamptz IS NULL OR (t.updated_at, t.id) < ($2, $3::uuid))
		ORDER BY t.updated_at DESC, t.id DESC LIMIT $4`, userID, at, id, minePageSize+1)
	if err != nil {
		return nil, nil, err
	}
	if len(items) > minePageSize {
		items = items[:minePageSize]
		last := items[minePageSize-1]
		next = &httpx.KeysetCursor{At: last.UpdatedAt, ID: last.ID}
	}
	return items, next, nil
}

// Queue lists tickets for staff by status ("" = open and answered), oldest
// waiting first so the longest-waiting customer is on top.
func (s *Service) Queue(ctx context.Context, status string) ([]Ticket, error) {
	if status == "" {
		return s.list(ctx, `WHERE t.status <> 'closed' ORDER BY (t.status = 'open') DESC, t.updated_at LIMIT 200`)
	}
	if err := validStatus(status); err != nil {
		return nil, err
	}
	return s.list(ctx, `WHERE t.status = $1 ORDER BY t.updated_at DESC LIMIT 200`, status)
}

func (s *Service) list(ctx context.Context, where string, args ...any) ([]Ticket, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id, t.subject, t.status, t.subscription_id, t.created_at, t.updated_at,
		t.user_id, coalesce(u.phone, '') FROM support_tickets t JOIN users u ON u.id = t.user_id `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("support: list: %w", err)
	}
	defer rows.Close()
	out := []Ticket{}
	for rows.Next() {
		var t Ticket
		var user uuid.UUID
		if err := rows.Scan(&t.ID, &t.Subject, &t.Status, &t.SubscriptionID, &t.CreatedAt, &t.UpdatedAt, &user, &t.Phone); err != nil {
			return nil, fmt.Errorf("support: list: %w", err)
		}
		t.UserID = &user
		out = append(out, t)
	}
	return out, rows.Err()
}

// Thread loads a ticket and its messages. owner restricts it to that
// customer (nil: staff); someone else's ticket is not found.
func (s *Service) Thread(ctx context.Context, id uuid.UUID, owner *uuid.UUID) (*Thread, error) {
	var t Thread
	var user uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT t.id, t.subject, t.status, t.subscription_id, t.created_at, t.updated_at,
		t.user_id, coalesce(u.phone, '') FROM support_tickets t JOIN users u ON u.id = t.user_id WHERE t.id = $1`, id).
		Scan(&t.ID, &t.Subject, &t.Status, &t.SubscriptionID, &t.CreatedAt, &t.UpdatedAt, &user, &t.Phone)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != nil && *owner != user) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("support: thread: %w", err)
	}
	if owner == nil {
		t.UserID = &user
	} else {
		t.Phone = ""
	}
	rows, err := s.pool.Query(ctx, `SELECT id, from_staff, body, created_at FROM support_messages
		WHERE ticket_id = $1 ORDER BY created_at, id`, id)
	if err != nil {
		return nil, fmt.Errorf("support: thread: %w", err)
	}
	defer rows.Close()
	t.Messages = []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.FromStaff, &m.Body, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("support: thread: %w", err)
		}
		t.Messages = append(t.Messages, m)
	}
	return &t, rows.Err()
}

// Reply adds a message. owner is the customer replying on their own ticket, or
// nil for staff. A customer reply reopens the ticket; a staff reply marks it
// answered and pushes the customer.
func (s *Service) Reply(ctx context.Context, id, authorID uuid.UUID, owner *uuid.UUID, body string) (*Thread, error) {
	body, err := clean("body", body, MaxBody)
	if err != nil {
		return nil, err
	}
	staff := owner == nil
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var user uuid.UUID
	var subject string
	err = tx.QueryRow(ctx, `SELECT user_id, subject FROM support_tickets WHERE id = $1 FOR UPDATE`, id).Scan(&user, &subject)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != nil && *owner != user) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("support: reply: %w", err)
	}
	var msgID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO support_messages (ticket_id, author_id, from_staff, body)
		VALUES ($1, $2, $3, $4) RETURNING id`, id, authorID, staff, body).Scan(&msgID); err != nil {
		return nil, fmt.Errorf("support: reply: %w", err)
	}
	status := "open"
	if staff {
		status = "answered"
	}
	if _, err := tx.Exec(ctx, `UPDATE support_tickets SET status = $2, updated_at = now() WHERE id = $1`, id, status); err != nil {
		return nil, fmt.Errorf("support: reply: %w", err)
	}
	if staff && s.notifier != nil {
		if _, err := s.notifier.Enqueue(ctx, tx, notify.Message{
			UserID: user.String(), TemplateKey: "support.reply", Category: notify.CategoryTransactional,
			// notify.Enqueue rewrites the /en prefix to the recipient's locale.
			Params:    map[string]string{"subject": subject, "link": "/en/support/" + id.String()},
			DedupeKey: "support-reply:" + msgID.String(),
		}); err != nil {
			return nil, fmt.Errorf("support: reply notify: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Thread(ctx, id, owner)
}

// SetStatus is staff closing or reopening a ticket.
func (s *Service) SetStatus(ctx context.Context, id uuid.UUID, status string) error {
	if err := validStatus(status); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE support_tickets SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("support: status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

func validStatus(s string) error {
	switch s {
	case "open", "answered", "closed":
		return nil
	}
	return invalid("status", "invalid", "open, answered or closed")
}
