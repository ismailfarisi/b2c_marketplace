// Package addresses is the customer's saved delivery addresses.
//
// GCC addressing is landmark-based, so the point is what delivery coverage is
// checked against and the text fields are what the driver reads.
package addresses

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
	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type Address struct {
	ID             uuid.UUID `json:"id"`
	Label          string    `json:"label"`
	RecipientName  *string   `json:"recipient_name"`
	RecipientPhone *string   `json:"recipient_phone"`
	Building       *string   `json:"building"`
	Flat           *string   `json:"flat"`
	Street         *string   `json:"street"`
	Landmark       *string   `json:"landmark"`
	Area           *string   `json:"area"`
	City           string    `json:"city"`
	Emirate        *string   `json:"emirate"`
	Notes          *string   `json:"notes"`
	Country        string    `json:"country"`
	Point          GeoPoint  `json:"point"`
	IsDefault      bool      `json:"is_default"`
	CreatedAt      time.Time `json:"created_at"`
}

type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type Input struct {
	Label          string    `json:"label"`
	RecipientName  *string   `json:"recipient_name"`
	RecipientPhone *string   `json:"recipient_phone"`
	Building       *string   `json:"building"`
	Flat           *string   `json:"flat"`
	Street         *string   `json:"street"`
	Landmark       *string   `json:"landmark"`
	Area           *string   `json:"area"`
	City           string    `json:"city"`
	Emirate        *string   `json:"emirate"`
	Notes          *string   `json:"notes"`
	Country        string    `json:"country"`
	Point          *GeoPoint `json:"point"`
	IsDefault      bool      `json:"is_default"`
}

// validate trims the input and reports field errors in the caller's language:
// the apps show errors[].message under the field, on Arabic screens too.
func (in *Input) validate(locale string) error {
	var fields []httpx.FieldError
	in.Label, in.City = strings.TrimSpace(in.Label), strings.TrimSpace(in.City)
	if in.Label == "" || len(in.Label) > 40 {
		fields = append(fields, httpx.FieldError{Field: "label", Code: "required", Message: say(locale, "Give the address a short name, like Home.", "أعطِ العنوان اسمًا قصيرًا، مثل المنزل.")})
	}
	if in.City == "" || len(in.City) > 80 {
		fields = append(fields, httpx.FieldError{Field: "city", Code: "required", Message: say(locale, "Enter the city.", "أدخل المدينة.")})
	}
	if in.Point == nil || in.Point.Lat < -90 || in.Point.Lat > 90 || in.Point.Lng < -180 || in.Point.Lng > 180 ||
		(in.Point.Lat == 0 && in.Point.Lng == 0) {
		fields = append(fields, httpx.FieldError{Field: "point", Code: "invalid_point", Message: say(locale, "Pin the address on the map.", "حدّد موقع العنوان على الخريطة.")})
	}
	in.Country = strings.ToUpper(strings.TrimSpace(in.Country))
	if in.Country == "" {
		in.Country = "AE"
	}
	if len(in.Country) != 2 {
		fields = append(fields, httpx.FieldError{Field: "country", Code: "invalid", Message: say(locale, "Use a two-letter country code.", "استخدم رمز الدولة المكوّن من حرفين.")})
	}
	for name, v := range map[string]*string{
		"recipient_name": in.RecipientName, "recipient_phone": in.RecipientPhone,
		"building": in.Building, "flat": in.Flat, "street": in.Street, "landmark": in.Landmark,
		"area": in.Area, "emirate": in.Emirate, "notes": in.Notes,
	} {
		if v != nil && len(*v) > 300 {
			fields = append(fields, httpx.FieldError{Field: name, Code: "too_long", Message: say(locale, "This is too long.", "هذا النص طويل جدًا.")})
		}
	}
	if len(fields) > 0 {
		return &httpx.ValidationError{Fields: fields}
	}
	return nil
}

const cols = `id, label, recipient_name, recipient_phone, building, flat, street,
	landmark, area, city, emirate, notes, country, ST_Y(point::geometry), ST_X(point::geometry),
	is_default, created_at`

func scan(row pgx.Row) (Address, error) {
	var a Address
	err := row.Scan(&a.ID, &a.Label, &a.RecipientName, &a.RecipientPhone, &a.Building, &a.Flat,
		&a.Street, &a.Landmark, &a.Area, &a.City, &a.Emirate, &a.Notes, &a.Country, &a.Point.Lat, &a.Point.Lng, &a.IsDefault, &a.CreatedAt)
	return a, err
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Address, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM addresses
		 WHERE user_id = $1 AND deleted_at IS NULL ORDER BY is_default DESC, created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("addresses: list: %w", err)
	}
	defer rows.Close()
	out := []Address{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("addresses: scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Create saves an address. The first one is the default.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (Address, error) {
	var a Address
	if err := in.validate(httpx.LocaleFromContext(ctx)); err != nil {
		return a, err
	}
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var has bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM addresses
			 WHERE user_id = $1 AND deleted_at IS NULL)`, userID).Scan(&has); err != nil {
			return fmt.Errorf("addresses: count: %w", err)
		}
		def := in.IsDefault || !has
		if def {
			if _, err := tx.Exec(ctx, `UPDATE addresses SET is_default = false, updated_at = now()
				 WHERE user_id = $1 AND is_default`, userID); err != nil {
				return fmt.Errorf("addresses: clear default: %w", err)
			}
		}
		var err error
		a, err = scan(tx.QueryRow(ctx, `
			INSERT INTO addresses (user_id, label, recipient_name, recipient_phone, building,
			    flat, street, landmark, area, city, emirate, notes, point, is_default, country)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			        ST_SetSRID(ST_MakePoint($14, $13), 4326)::geography, $15, $16)
			RETURNING `+cols,
			userID, in.Label, in.RecipientName, in.RecipientPhone, in.Building, in.Flat,
			in.Street, in.Landmark, in.Area, in.City, in.Emirate, in.Notes, in.Point.Lat, in.Point.Lng, def, in.Country))
		if err != nil {
			return fmt.Errorf("addresses: insert: %w", err)
		}
		return nil
	})
	return a, err
}

// Delete soft-deletes so subscriptions and past orders that point at the
// address keep resolving. Someone else's address is 404. Removing the default
// hands it to the most recently saved address left, so checkout always has
// one to offer first.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var wasDefault bool
		err := tx.QueryRow(ctx, `SELECT is_default FROM addresses
			 WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`, id, userID).Scan(&wasDefault)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("addresses: find: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE addresses
			   SET deleted_at = now(), is_default = false, updated_at = now()
			 WHERE id = $1`, id); err != nil {
			return fmt.Errorf("addresses: delete: %w", err)
		}
		if !wasDefault {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE addresses SET is_default = true, updated_at = now()
			 WHERE id = (SELECT id FROM addresses
			              WHERE user_id = $1 AND deleted_at IS NULL
			              ORDER BY created_at DESC, id DESC LIMIT 1)`, userID); err != nil {
			return fmt.Errorf("addresses: promote default: %w", err)
		}
		return nil
	})
}

// DefaultOnly is the 422 for PATCH is_default:false: the default moves by
// choosing another address, never by clearing it.
func DefaultOnly(ctx context.Context) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{
		Field: "is_default", Code: "invalid",
		Message: say(httpx.LocaleFromContext(ctx),
			"To change your default address, choose another address as the default.",
			"لتغيير عنوانك الافتراضي، اختر عنوانًا آخر ليكون الافتراضي."),
	}}}
}

// SetDefault makes one address the one checkout offers first. Someone else's
// address, or a removed one, is 404.
func (s *Service) SetDefault(ctx context.Context, userID, id uuid.UUID) (Address, error) {
	var a Address
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM addresses
			 WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL)`, id, userID).Scan(&ok); err != nil {
			return fmt.Errorf("addresses: find: %w", err)
		}
		if !ok {
			return httpx.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `UPDATE addresses SET is_default = false, updated_at = now()
			 WHERE user_id = $1 AND is_default AND id <> $2`, userID, id); err != nil {
			return fmt.Errorf("addresses: clear default: %w", err)
		}
		var err error
		a, err = scan(tx.QueryRow(ctx, `UPDATE addresses SET is_default = true, updated_at = now()
			 WHERE id = $1 AND user_id = $2 RETURNING `+cols, id, userID))
		if err != nil {
			return fmt.Errorf("addresses: set default: %w", err)
		}
		return nil
	})
	return a, err
}

func say(locale, en, ar string) string {
	if locale == "ar" {
		return ar
	}
	return en
}

// Coverage caps keep the matrix a client can ask for small: a kitchen list
// page of branches against the customer's handful of saved addresses.
const (
	CoverageMaxBranches  = 60
	CoverageMaxAddresses = 6
)

// CoverageItem says whether one kitchen delivers to one of the customer's
// addresses.
type CoverageItem struct {
	BranchID  uuid.UUID `json:"branch_id"`
	AddressID uuid.UUID `json:"address_id"`
	Delivers  bool      `json:"delivers"`
}

// Coverage answers, for every (branch, address) pair, whether the kitchen's
// delivery area contains the address. Only the caller's own live addresses are
// considered; other ids are left out rather than reported.
func (s *Service) Coverage(ctx context.Context, userID uuid.UUID, branchIDs, addressIDs []uuid.UUID) ([]CoverageItem, error) {
	if len(branchIDs) == 0 {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "branch_ids", Code: "required", Message: "choose at least one kitchen"}}}
	}
	if len(branchIDs) > CoverageMaxBranches {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "branch_ids", Code: "too_many",
			Message: fmt.Sprintf("at most %d kitchens per request", CoverageMaxBranches)}}}
	}
	if len(addressIDs) == 0 {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "address_ids", Code: "required", Message: "choose at least one address"}}}
	}
	if len(addressIDs) > CoverageMaxAddresses {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "address_ids", Code: "too_many",
			Message: fmt.Sprintf("at most %d addresses per request", CoverageMaxAddresses)}}}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, a.id, COALESCE(ST_Intersects(b.service_area, a.point), false)
		  FROM unnest($1::uuid[]) WITH ORDINALITY AS bi(id, ord)
		  JOIN vendor_branches b ON b.id = bi.id
		  CROSS JOIN unnest($2::uuid[]) WITH ORDINALITY AS ai(id, ord)
		  JOIN addresses a ON a.id = ai.id AND a.user_id = $3 AND a.deleted_at IS NULL
		 ORDER BY bi.ord, ai.ord`, branchIDs, addressIDs, userID)
	if err != nil {
		return nil, fmt.Errorf("addresses: coverage: %w", err)
	}
	defer rows.Close()
	out := []CoverageItem{}
	for rows.Next() {
		var it CoverageItem
		if err := rows.Scan(&it.BranchID, &it.AddressID, &it.Delivers); err != nil {
			return nil, fmt.Errorf("addresses: scan coverage: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
