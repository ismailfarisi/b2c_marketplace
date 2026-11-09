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

// Removing the default must not leave the customer without one: the most
// recently saved address left takes over. Removing a non-default keeps it.
func TestDeletePromotesNewestRemaining(t *testing.T) {
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
	var user uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (phone) VALUES ($1) RETURNING id`,
		fmt.Sprintf("+97157%07d", rand.IntN(10_000_000))).Scan(&user); err != nil {
		t.Fatalf("user: %v", err)
	}
	s := NewService(pool)
	add := func(label string) Address {
		a, err := s.Create(ctx, user, Input{Label: label, City: "Dubai", Point: &GeoPoint{Lat: 25.2, Lng: 55.3}})
		if err != nil {
			t.Fatalf("create %s: %v", label, err)
		}
		return a
	}
	home, gym, office := add("Home"), add("Gym"), add("Office")
	defaultOf := func() (uuid.UUID, int) {
		t.Helper()
		list, err := s.List(ctx, user)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var id uuid.UUID
		n := 0
		for _, a := range list {
			if a.IsDefault {
				id, n = a.ID, n+1
			}
		}
		return id, n
	}
	if id, n := defaultOf(); id != home.ID || n != 1 {
		t.Fatalf("default = %v (%d), want home", id, n)
	}

	if err := s.Delete(ctx, user, gym.ID); err != nil {
		t.Fatalf("delete gym: %v", err)
	}
	if id, n := defaultOf(); id != home.ID || n != 1 {
		t.Fatalf("after removing a non-default: default = %v (%d), want home", id, n)
	}

	if err := s.Delete(ctx, user, home.ID); err != nil {
		t.Fatalf("delete home: %v", err)
	}
	if id, n := defaultOf(); id != office.ID || n != 1 {
		t.Fatalf("after removing the default: default = %v (%d), want office", id, n)
	}

	if err := s.Delete(ctx, user, office.ID); err != nil {
		t.Fatalf("delete office: %v", err)
	}
	if _, n := defaultOf(); n != 0 {
		t.Fatalf("no addresses left but %d defaults", n)
	}
	if err := s.Delete(ctx, user, office.ID); !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("second delete: want 404, got %v", err)
	}
}

func TestCoverageAnswersOnlyTheCallersAddresses(t *testing.T) {
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
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v\n%s", err, sql)
		}
	}
	user, other := uuid.New(), uuid.New()
	exec(`INSERT INTO users (id, phone) VALUES ($1, $2), ($3, $4)`, user, fmt.Sprintf("+97157%07d", rand.IntN(10_000_000)),
		other, fmt.Sprintf("+97157%07d", rand.IntN(10_000_000)))
	vendor, near, far := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO vendors (id, slug, legal_name, trade_name) VALUES ($1, $2, 'C LLC', 'C')`, vendor, "coverage-"+vendor.String())
	for _, b := range []struct {
		id  uuid.UUID
		lng float64
	}{{near, 55.23}, {far, 56.50}} {
		exec(`INSERT INTO vendor_branches (id, vendor_id, name, point, service_area) VALUES ($1, $2, 'C',
			ST_SetSRID(ST_MakePoint($3, 25.14), 4326)::geography,
			ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($3, 25.14), 4326)::geography, 10000)::geometry)::geography)`, b.id, vendor, b.lng)
	}
	home, theirs, deleted := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO addresses (id, user_id, label, city, point) VALUES
		($1, $4, 'Home', 'Dubai', ST_SetSRID(ST_MakePoint(55.25, 25.14), 4326)::geography),
		($2, $5, 'Theirs', 'Dubai', ST_SetSRID(ST_MakePoint(55.25, 25.14), 4326)::geography),
		($3, $4, 'Old', 'Dubai', ST_SetSRID(ST_MakePoint(55.25, 25.14), 4326)::geography)`, home, theirs, deleted, user, other)
	exec(`UPDATE addresses SET deleted_at = now() WHERE id = $1`, deleted)

	s := NewService(pool)
	items, err := s.Coverage(ctx, user, []uuid.UUID{near, far}, []uuid.UUID{home, theirs, deleted})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].BranchID != near || !items[0].Delivers || items[1].BranchID != far || items[1].Delivers ||
		items[0].AddressID != home || items[1].AddressID != home {
		t.Fatalf("items = %+v", items)
	}
	if _, err := s.Coverage(ctx, user, make([]uuid.UUID, CoverageMaxBranches+1), []uuid.UUID{home}); err == nil {
		t.Fatal("too many kitchens must be refused")
	}
	if _, err := s.Coverage(ctx, user, []uuid.UUID{near}, nil); err == nil {
		t.Fatal("no addresses must be refused")
	}
}
