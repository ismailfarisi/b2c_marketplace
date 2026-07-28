package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

// CatalogQueries serves the read side.
//
// Read-only and query-shaped, so it does not go through a service layer — there
// is no invariant to protect and no transaction to coordinate. Adding one would
// be ceremony, not structure.
type CatalogQueries struct {
	pool *pgxpool.Pool
}

func NewCatalogQueries(pool *pgxpool.Pool) *CatalogQueries {
	return &CatalogQueries{pool: pool}
}

func (c *CatalogQueries) ListOfferings(w http.ResponseWriter, r *http.Request) {
	branchID, err := uuid.Parse(chi.URLParam(r, "branchId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	rows, err := c.pool.Query(r.Context(), `
		SELECT id, branch_id, service_type, name, name_ar, description,
		       base_price_minor, currency, price_is_estimate,
		       lead_time_minutes, image_url, is_active
		  FROM service_offerings
		 WHERE branch_id = $1
		   AND is_active
		   AND deleted_at IS NULL
		   AND ($2 = '' OR service_type = $2)
		 ORDER BY sort_order, name`,
		branchID, r.URL.Query().Get("service_type"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	defer rows.Close()

	items := []map[string]any{}
	for rows.Next() {
		var (
			id, bID                       uuid.UUID
			serviceType, name, currency   string
			nameAr, description, imageURL *string
			basePrice                     int64
			isEstimate, isActive          bool
			leadTime                      int
		)
		if err := rows.Scan(&id, &bID, &serviceType, &name, &nameAr, &description,
			&basePrice, &currency, &isEstimate, &leadTime, &imageURL, &isActive); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
		items = append(items, map[string]any{
			"id":           id,
			"branch_id":    bID,
			"service_type": serviceType,
			"name":         name,
			"name_ar":      nameAr,
			"description":  description,
			"base_price": map[string]any{
				"amount":   basePrice,
				"currency": currency,
			},
			"price_is_estimate": isEstimate,
			"lead_time_minutes": leadTime,
			"image_url":         imageURL,
			"is_active":         isActive,
		})
	}
	if err := rows.Err(); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

// GetAvailability returns only windows with remaining capacity.
//
// A window absent from the response is full, closed, or outside the operating
// calendar, and the client cannot tell which. That is deliberate: "full" is
// commercially sensitive to a vendor, and "closed" invites a retry loop.
func (c *CatalogQueries) GetAvailability(w http.ResponseWriter, r *http.Request) {
	branchID, err := uuid.Parse(chi.URLParam(r, "branchId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	q := r.URL.Query()
	resourceKey := q.Get("resource_key")
	if resourceKey == "" {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "resource_key", Code: "required",
		}}})
		return
	}

	from, err1 := time.Parse("2006-01-02", q.Get("from"))
	to, err2 := time.Parse("2006-01-02", q.Get("to"))
	if err1 != nil || err2 != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "from/to", Code: "invalid", Message: "expected YYYY-MM-DD",
		}}})
		return
	}
	// Bounded because an unbounded range is an accidental table scan across
	// every bucket a branch has ever had, on an endpoint the app calls on
	// every screen open.
	if to.Before(from) || to.Sub(from) > 31*24*time.Hour {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "to", Code: "out_of_range", Message: "range must be 0–31 days after `from`",
		}}})
		return
	}

	rows, err := c.pool.Query(r.Context(), `
		SELECT b.bucket_date, b.window_id, w.starts_at, w.ends_at,
		       (b.capacity_limit - b.consumed - b.reserved)::int AS remaining
		  FROM capacity_buckets b
		  JOIN time_windows w ON w.id = b.window_id
		 WHERE b.branch_id = $1
		   AND b.resource_key = $2
		   AND b.bucket_date BETWEEN $3 AND $4
		   AND b.capacity_limit > b.consumed + b.reserved
		   AND w.is_active
		 ORDER BY b.bucket_date, w.starts_at`,
		branchID, resourceKey, from, to)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	defer rows.Close()

	type window struct {
		WindowID  uuid.UUID `json:"window_id"`
		StartsAt  string    `json:"starts_at"`
		EndsAt    string    `json:"ends_at"`
		Remaining int       `json:"remaining"`
	}
	byDate := map[string][]window{}
	var order []string

	for rows.Next() {
		var (
			date             time.Time
			windowID         uuid.UUID
			startsAt, endsAt time.Time
			remaining        int
		)
		if err := rows.Scan(&date, &windowID, &startsAt, &endsAt, &remaining); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
		key := date.Format("2006-01-02")
		if _, seen := byDate[key]; !seen {
			order = append(order, key)
		}
		byDate[key] = append(byDate[key], window{
			WindowID:  windowID,
			StartsAt:  startsAt.Format("15:04"),
			EndsAt:    endsAt.Format("15:04"),
			Remaining: remaining,
		})
	}
	if err := rows.Err(); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(order))
	for _, date := range order {
		items = append(items, map[string]any{"date": date, "windows": byDate[date]})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}
