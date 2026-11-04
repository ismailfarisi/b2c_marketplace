package addresses

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

func TestSetDefaultMovesTheDefault(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	newUser := func() uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO users (phone) VALUES ($1) RETURNING id`,
			fmt.Sprintf("+97157%07d", rand.IntN(10_000_000))).Scan(&id); err != nil {
			t.Fatalf("user: %v", err)
		}
		return id
	}
	s := NewService(pool)
	user, other := newUser(), newUser()
	add := func(u uuid.UUID, label string) Address {
		a, err := s.Create(ctx, u, Input{Label: label, City: "Dubai", Point: &GeoPoint{Lat: 25.2, Lng: 55.3}})
		if err != nil {
			t.Fatalf("create %s: %v", label, err)
		}
		return a
	}
	home, office := add(user, "Home"), add(user, "Office")
	if !home.IsDefault || office.IsDefault {
		t.Fatalf("first address should be the default: home=%v office=%v", home.IsDefault, office.IsDefault)
	}

	got, err := s.SetDefault(ctx, user, office.ID)
	if err != nil || !got.IsDefault {
		t.Fatalf("set default: %v %+v", err, got)
	}
	list, err := s.List(ctx, user)
	if err != nil || len(list) != 2 || list[0].ID != office.ID || list[1].IsDefault {
		t.Fatalf("list after set default: %v %+v", err, list)
	}

	if _, err := s.SetDefault(ctx, other, office.ID); !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("someone else's address: want 404, got %v", err)
	}
	if err := s.Delete(ctx, user, home.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.SetDefault(ctx, user, home.ID); !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("removed address: want 404, got %v", err)
	}
}
