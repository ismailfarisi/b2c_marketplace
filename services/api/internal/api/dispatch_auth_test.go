package api

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/console"
	"github.com/marketplace/api/internal/modules/dispatch"
)

func dispatchRequest(user uuid.UUID, method, param, value string) *http.Request {
	r := httptest.NewRequest(method, "/", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add(param, value)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rc)
	return r.WithContext(httpx.WithUserID(ctx, user))
}

// The dispatch board, route planning and assignment are gated on branch
// membership: a stranger gets 404, a staff member can read but not write.
func TestDispatchRequiresBranchRole(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var branchID, vendorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id, vendor_id FROM vendor_branches LIMIT 1`).Scan(&branchID, &vendorID); err != nil {
		t.Skipf("no branch seeded: %v", err)
	}
	var user uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (phone) VALUES ($1) RETURNING id`,
		fmt.Sprintf("+97156%07d", rand.IntN(10_000_000))).Scan(&user); err != nil {
		t.Fatal(err)
	}

	h := &DriverHandlers{Dispatch: dispatch.NewService(pool, dispatch.Options{}), Console: console.NewService(pool)}

	w := httptest.NewRecorder()
	h.Board(w, dispatchRequest(user, http.MethodGet, "branchId", branchID.String()))
	if w.Code != http.StatusNotFound {
		t.Fatalf("stranger board: got %d, want 404", w.Code)
	}
	w = httptest.NewRecorder()
	h.BuildRoutes(w, dispatchRequest(user, http.MethodPost, "branchId", branchID.String()))
	if w.Code != http.StatusNotFound {
		t.Fatalf("stranger plan: got %d, want 404", w.Code)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO vendor_users (vendor_id, user_id, role) VALUES ($1, $2, 'staff')`, vendorID, user); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.BuildRoutes(w, dispatchRequest(user, http.MethodPost, "branchId", branchID.String()))
	if w.Code != http.StatusForbidden {
		t.Fatalf("staff plan: got %d, want 403", w.Code)
	}

	var routeID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM delivery_routes WHERE branch_id = $1 LIMIT 1`, branchID).Scan(&routeID); err == nil {
		w = httptest.NewRecorder()
		h.AssignRoute(w, dispatchRequest(user, http.MethodPost, "routeId", routeID.String()))
		if w.Code != http.StatusForbidden {
			t.Fatalf("staff assign: got %d, want 403", w.Code)
		}
	}
}
