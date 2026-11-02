package invoicing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
)

// Printable is an issued invoice exactly as stored: the snapshot a printed or
// saved-as-PDF copy must reproduce, never re-derived from live rows.
type Printable struct {
	ID          uuid.UUID       `json:"id"`
	Number      string          `json:"number"`
	Kind        string          `json:"kind"`
	Status      string          `json:"status"`
	Country     string          `json:"country"`
	Currency    string          `json:"currency"`
	Supplier    PrintableParty  `json:"supplier"`
	Buyer       PrintableParty  `json:"buyer"`
	SupplyDate  string          `json:"supply_date"`
	IssuedAt    time.Time       `json:"issued_at"`
	Corrects    *string         `json:"corrects_number"`
	NetMinor    int64           `json:"net_minor"`
	TaxMinor    int64           `json:"tax_minor"`
	GrossMinor  int64           `json:"gross_minor"`
	Lines       []PrintableLine `json:"lines"`
	buyerUserID *uuid.UUID
}

type PrintableParty struct {
	Name    string `json:"name"`
	TRN     string `json:"trn"`
	Address string `json:"address"`
}

type PrintableLine struct {
	Description   string  `json:"description"`
	DescriptionAR string  `json:"description_ar"`
	Quantity      float64 `json:"quantity"`
	UnitNetMinor  int64   `json:"unit_net_minor"`
	NetMinor      int64   `json:"net_minor"`
	TaxRateBP     int     `json:"tax_rate_bp"`
	TaxMinor      int64   `json:"tax_minor"`
	GrossMinor    int64   `json:"gross_minor"`
}

// Get loads one invoice for printing. buyer restricts it to that customer
// (nil: staff); anyone else's invoice is not found.
func (s *Service) Get(ctx context.Context, id uuid.UUID, buyer *uuid.UUID) (*Printable, error) {
	var p Printable
	var supply time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT i.id, i.number, i.kind, i.status, i.country, i.currency,
		       i.supplier_name, coalesce(i.supplier_trn, ''),
		       coalesce(i.buyer_name, ''), coalesce(i.buyer_trn, ''), coalesce(i.buyer_address, ''),
		       i.supply_date, i.issued_at, c.number, i.net_minor, i.tax_minor, i.gross_minor, i.buyer_user_id
		  FROM invoices i LEFT JOIN invoices c ON c.id = i.corrects_invoice_id
		 WHERE i.id = $1`, id).Scan(
		&p.ID, &p.Number, &p.Kind, &p.Status, &p.Country, &p.Currency,
		&p.Supplier.Name, &p.Supplier.TRN, &p.Buyer.Name, &p.Buyer.TRN, &p.Buyer.Address,
		&supply, &p.IssuedAt, &p.Corrects, &p.NetMinor, &p.TaxMinor, &p.GrossMinor, &p.buyerUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("invoicing: get: %w", err)
	}
	if buyer != nil && (p.buyerUserID == nil || *p.buyerUserID != *buyer) {
		return nil, httpx.ErrNotFound
	}
	p.SupplyDate = supply.Format("2006-01-02")

	rows, err := s.pool.Query(ctx, `
		SELECT description, coalesce(description_ar, ''), quantity::float8, unit_net_minor, net_minor,
		       tax_rate_bp, tax_minor, gross_minor
		  FROM invoice_lines WHERE invoice_id = $1 ORDER BY line_no`, id)
	if err != nil {
		return nil, fmt.Errorf("invoicing: get lines: %w", err)
	}
	defer rows.Close()
	p.Lines = []PrintableLine{}
	for rows.Next() {
		var l PrintableLine
		if err := rows.Scan(&l.Description, &l.DescriptionAR, &l.Quantity, &l.UnitNetMinor, &l.NetMinor,
			&l.TaxRateBP, &l.TaxMinor, &l.GrossMinor); err != nil {
			return nil, fmt.Errorf("invoicing: get lines: %w", err)
		}
		p.Lines = append(p.Lines, l)
	}
	return &p, rows.Err()
}
