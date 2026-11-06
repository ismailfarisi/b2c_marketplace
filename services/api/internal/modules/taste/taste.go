// Package taste remembers what a customer loves and is tired of: a heart or a
// "not again" on a kitchen or a dish, and how adventurous their next rotation
// should be. Renewal proposals and default dish picks read it.
package taste

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

const (
	KindKitchen = "kitchen"
	KindDish    = "dish"

	Favourite = "favourite"
	Avoid     = "avoid"

	Familiar = "familiar"
	Balanced = "balanced"
	Surprise = "surprise"
)

var (
	Kinds      = []string{KindKitchen, KindDish}
	Sentiments = []string{Favourite, Avoid}
	Adventures = []string{Familiar, Balanced, Surprise}
)

// Taste is one remembered like or dislike.
type Taste struct {
	SubjectKind string    `json:"subject_kind"`
	SubjectID   uuid.UUID `json:"subject_id"`
	Sentiment   string    `json:"sentiment"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Preferences is the customer's meal preferences; absent rows read as balanced.
type Preferences struct {
	Adventure string `json:"adventure"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func invalid(field, code, msg string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: code, Message: msg}}}
}

// List is everything the customer has marked, newest first.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Taste, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT subject_kind, subject_id, sentiment, updated_at
		  FROM meal_tastes WHERE user_id = $1
		 ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("taste: list: %w", err)
	}
	defer rows.Close()
	out := []Taste{}
	for rows.Next() {
		var t Taste
		if err := rows.Scan(&t.SubjectKind, &t.SubjectID, &t.Sentiment, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("taste: scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Set marks a kitchen or dish; marking it again with the other sentiment flips it.
func (s *Service) Set(ctx context.Context, userID uuid.UUID, kind string, subjectID uuid.UUID, sentiment string) (Taste, error) {
	if !slices.Contains(Kinds, kind) {
		return Taste{}, invalid("subject_kind", "invalid", "subject_kind must be kitchen or dish")
	}
	if !slices.Contains(Sentiments, sentiment) {
		return Taste{}, invalid("sentiment", "invalid", "sentiment must be favourite or avoid")
	}
	if subjectID == uuid.Nil {
		return Taste{}, invalid("subject_id", "required", "subject_id is required")
	}
	var exists bool
	table := "vendor_branches"
	if kind == KindDish {
		table = "menu_items"
	}
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1)`, subjectID).Scan(&exists); err != nil {
		return Taste{}, fmt.Errorf("taste: subject: %w", err)
	}
	if !exists {
		return Taste{}, invalid("subject_id", "not_found", "no such "+kind)
	}
	var t Taste
	err := s.pool.QueryRow(ctx, `
		INSERT INTO meal_tastes (user_id, subject_kind, subject_id, sentiment)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, subject_kind, subject_id)
		DO UPDATE SET sentiment = EXCLUDED.sentiment, updated_at = now()
		RETURNING subject_kind, subject_id, sentiment, updated_at`,
		userID, kind, subjectID, sentiment).Scan(&t.SubjectKind, &t.SubjectID, &t.Sentiment, &t.UpdatedAt)
	if err != nil {
		return Taste{}, fmt.Errorf("taste: set: %w", err)
	}
	return t, nil
}

// Clear forgets a mark; clearing one that is not there is fine.
func (s *Service) Clear(ctx context.Context, userID uuid.UUID, kind string, subjectID uuid.UUID) error {
	if !slices.Contains(Kinds, kind) {
		return invalid("subject_kind", "invalid", "subject_kind must be kitchen or dish")
	}
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM meal_tastes WHERE user_id = $1 AND subject_kind = $2 AND subject_id = $3`,
		userID, kind, subjectID); err != nil {
		return fmt.Errorf("taste: clear: %w", err)
	}
	return nil
}

// Preferences reads the customer's meal preferences.
func (s *Service) Preferences(ctx context.Context, userID uuid.UUID) (Preferences, error) {
	p := Preferences{Adventure: Balanced}
	err := s.pool.QueryRow(ctx, `SELECT adventure FROM meal_preferences WHERE user_id = $1`, userID).Scan(&p.Adventure)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return p, fmt.Errorf("taste: preferences: %w", err)
	}
	return p, nil
}

// SetPreferences stores the adventure setting.
func (s *Service) SetPreferences(ctx context.Context, userID uuid.UUID, adventure string) (Preferences, error) {
	if !slices.Contains(Adventures, adventure) {
		return Preferences{}, invalid("adventure", "invalid", "adventure must be familiar, balanced or surprise")
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meal_preferences (user_id, adventure) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET adventure = EXCLUDED.adventure, updated_at = now()`,
		userID, adventure); err != nil {
		return Preferences{}, fmt.Errorf("taste: set preferences: %w", err)
	}
	return Preferences{Adventure: adventure}, nil
}
