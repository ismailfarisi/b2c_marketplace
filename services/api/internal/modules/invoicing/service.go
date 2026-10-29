package invoicing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/tax"
	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct {
	pool *pgxpool.Pool
	tax  *tax.Service
	// supplierIsVendor encodes docs/README.md decision #4 in one place.
	//
	// If the vendor is the supplier of record, the vendor's TRN goes on the
	// customer's invoice and the platform invoices the vendor separately for
	// commission. If the platform is a deemed supplier, the platform's TRN goes
	// on the customer's invoice and the vendor invoices the platform.
	//
	// The decision is open. Rather than build for one and rewrite for the
	// other, both paths exist and this flag selects — because the two produce
	// *different legal documents from the same order*, and discovering that
	// after launch means re-issuing every invoice ever sent.
	supplierIsVendor bool
}

func NewService(pool *pgxpool.Pool, taxSvc *tax.Service, supplierIsVendor bool) *Service {
	return &Service{pool: pool, tax: taxSvc, supplierIsVendor: supplierIsVendor}
}

// IssueForOrder creates the customer's invoice for a captured order.
//
// Idempotent by a partial unique index rather than by a check-then-insert: two
// concurrent calls both see no invoice, both allocate a number, and the loser's
// transaction is rolled back by the index — which returns its number to the
// series, because the number was allocated inside that same transaction.
func (s *Service) IssueForOrder(ctx context.Context, orderID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID

	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		doc, buyerUserID, vendorID, err := s.buildFromOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if err := doc.Validate(); err != nil {
			return err
		}
		id, err = s.insert(ctx, tx, doc, insertRefs{
			OrderID:     &orderID,
			BuyerUserID: &buyerUserID,
			VendorID:    vendorID,
		})
		return err
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// buildFromOrder assembles the document from the captured quote.
//
// Reads `quote_lines` rather than `order_lines`. The quote is what the customer
// agreed to pay — it carries discounts, surcharges and the delivery fee, and it
// is versioned, so an adjusted order invoices the adjustment rather than the
// original estimate.
func (s *Service) buildFromOrder(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (
	Document, uuid.UUID, *uuid.UUID, error,
) {
	var (
		doc         Document
		buyerUserID uuid.UUID
		vendorID    uuid.UUID
		quoteID     uuid.UUID
		placedAt    time.Time
		deliveredAt *time.Time
	)

	err := tx.QueryRow(ctx, `
		SELECT q.id, q.currency, o.user_id, b.vendor_id, o.created_at,
		       coalesce(v.country, 'AE'),
		       coalesce(v.legal_name, ''), coalesce(v.tax_registration_no, ''),
		       coalesce(u.display_name, ''),
		       (SELECT max(f.completed_at) FROM fulfillments f
		         WHERE f.order_id = o.id AND f.status = 'completed')
		  FROM orders o
		  JOIN price_quotes q ON q.order_id = o.id AND q.superseded_by IS NULL
		  JOIN vendor_branches b ON b.id = o.branch_id
		  JOIN vendors v ON v.id = b.vendor_id
		  LEFT JOIN users u ON u.id = o.user_id
		 WHERE o.id = $1`, orderID).
		Scan(&quoteID, &doc.Currency, &buyerUserID, &vendorID, &placedAt, &doc.Country,
			&doc.Supplier.Name, &doc.Supplier.TRN, &doc.Buyer.Name, &deliveredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return doc, uuid.Nil, nil, httpx.ErrNotFound
	}
	if err != nil {
		return doc, uuid.Nil, nil, fmt.Errorf("invoicing: load order: %w", err)
	}

	// An office lunch is bought by the company, so the company is the buyer
	// of record and its TRN makes this a full tax invoice.
	var companyName, companyTRN string
	if err := tx.QueryRow(ctx, `
		SELECT c.legal_name, coalesce(c.trn, '')
		  FROM company_charges ch JOIN companies c ON c.id = ch.company_id
		 WHERE ch.order_id = $1 AND ch.kind = 'charge'`, orderID).Scan(&companyName, &companyTRN); err == nil {
		doc.Buyer.Name, doc.Buyer.TRN = companyName, companyTRN
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return doc, uuid.Nil, nil, fmt.Errorf("invoicing: load company buyer: %w", err)
	}

	var supplierVendor *uuid.UUID
	if s.supplierIsVendor {
		supplierVendor = &vendorID
	} else {
		// Deemed-supplier model: the platform's own registration replaces the
		// vendor's on the customer-facing document.
		reg, err := s.platformRegistration(ctx, tx, doc.Country)
		if err != nil {
			return doc, uuid.Nil, nil, err
		}
		doc.Supplier = reg
	}

	if deliveredAt != nil {
		doc.SupplyDate = SupplyDate(*deliveredAt, placedAt)
	} else {
		doc.SupplyDate = placedAt
	}

	rows, err := tx.Query(ctx, `
		SELECT kind, label, coalesce(label_ar, ''), amount_minor, coalesce(tax_rate_bp, 0)
		  FROM quote_lines
		 WHERE quote_id = $1 AND kind <> 'tax'
		 ORDER BY sort_order, label`, quoteID)
	if err != nil {
		return doc, uuid.Nil, nil, fmt.Errorf("invoicing: load quote lines: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			kind, label, labelAR string
			amount               int64
			rateBP               int
		)
		if err := rows.Scan(&kind, &label, &labelAR, &amount, &rateBP); err != nil {
			return doc, uuid.Nil, nil, err
		}
		// Prices are tax-inclusive (PLAN.md §7), so every quote line's amount is
		// a gross and the split runs here rather than at checkout.
		net, taxMinor := tax.Split(amount, rateBP)

		category := string(tax.Standard)
		if rateBP == 0 {
			category = string(tax.ZeroRated)
		}

		doc.Lines = append(doc.Lines, Line{
			Description:   label,
			DescriptionAR: labelAR,
			Quantity:      1,
			UnitNetMinor:  net,
			NetMinor:      net,
			TaxCategory:   category,
			TaxRateBP:     rateBP,
			TaxMinor:      taxMinor,
			GrossMinor:    amount,
		})
		doc.NetMinor += net
		doc.TaxMinor += taxMinor
		doc.GrossMinor += amount
	}
	if err := rows.Err(); err != nil {
		return doc, uuid.Nil, nil, err
	}

	doc.Kind = KindFor(doc.GrossMinor, doc.Buyer.TRN)
	return doc, buyerUserID, supplierVendor, nil
}

func (s *Service) platformRegistration(ctx context.Context, tx pgx.Tx, country string) (Party, error) {
	var p Party
	err := tx.QueryRow(ctx, `
		SELECT legal_name, registration_no, coalesce(address_line, '')
		  FROM tax_registrations
		 WHERE owner_type = 'platform' AND country = $1 AND valid_to IS NULL`, country).
		Scan(&p.Name, &p.TRN, &p.Address)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, fmt.Errorf("invoicing: the platform has no %s tax registration on file, "+
			"so it cannot be the supplier of record there", country)
	}
	if err != nil {
		return p, fmt.Errorf("invoicing: load platform registration: %w", err)
	}
	return p, nil
}

type insertRefs struct {
	OrderID             *uuid.UUID
	SubscriptionCycleID *uuid.UUID
	PackagePurchaseID   *uuid.UUID
	PayoutStatementID   *uuid.UUID
	BuyerUserID         *uuid.UUID
	VendorID            *uuid.UUID
	CorrectsInvoiceID   *uuid.UUID
}

// insert allocates the number and writes the document.
//
// `nextNumber` runs first and inside the same transaction. That ordering is the
// whole design: the lock is held from allocation to commit, so two concurrent
// issues serialise, and a failure anywhere after allocation rolls the number
// back rather than burning it.
func (s *Service) insert(ctx context.Context, tx pgx.Tx, doc Document, refs insertRefs) (uuid.UUID, error) {
	year := doc.SupplyDate.Year()

	seriesID, seq, err := nextNumber(ctx, tx, doc.Country, doc.Kind, year)
	if err != nil {
		return uuid.Nil, err
	}
	number := FormatNumber(Prefix(doc.Kind), doc.Country, year, seq)

	supplierType := "platform"
	var supplierID *uuid.UUID
	if refs.VendorID != nil {
		supplierType = "vendor"
		supplierID = refs.VendorID
	}

	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO invoices (
		    series_id, number, sequence_no, kind,
		    order_id, subscription_cycle_id, package_purchase_id, payout_statement_id,
		    supplier_type, supplier_id, supplier_name, supplier_trn,
		    buyer_name, buyer_trn, buyer_user_id, buyer_address,
		    country, currency, net_minor, tax_minor, gross_minor,
		    supply_date, corrects_invoice_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, nullif($12, ''),
		        nullif($13, ''), nullif($14, ''), $15, nullif($16, ''),
		        $17, $18, $19, $20, $21, $22, $23)
		RETURNING id`,
		seriesID, number, seq, string(doc.Kind),
		refs.OrderID, refs.SubscriptionCycleID, refs.PackagePurchaseID, refs.PayoutStatementID,
		supplierType, supplierID, doc.Supplier.Name, doc.Supplier.TRN,
		doc.Buyer.Name, doc.Buyer.TRN, refs.BuyerUserID, doc.Buyer.Address,
		doc.Country, doc.Currency, doc.NetMinor, doc.TaxMinor, doc.GrossMinor,
		doc.SupplyDate, refs.CorrectsInvoiceID).Scan(&id)
	if err != nil {
		if isUniqueViolation(err, "invoices_one_per_order_idx") {
			// The one-invoice-per-order index fired. Someone else got there
			// first, and the number this transaction allocated goes back with
			// the rollback rather than leaving a gap.
			return uuid.Nil, fmt.Errorf("%w: this order already has a tax invoice", httpx.ErrConflict)
		}
		return uuid.Nil, fmt.Errorf("invoicing: insert invoice: %w", err)
	}

	for i, l := range doc.Lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO invoice_lines (
			    invoice_id, line_no, description, description_ar, quantity,
			    unit_net_minor, net_minor, tax_category, tax_rate_bp, tax_minor, gross_minor)
			VALUES ($1, $2, $3, nullif($4, ''), $5, $6, $7, $8, $9, $10, $11)`,
			id, i+1, l.Description, l.DescriptionAR, l.Quantity,
			l.UnitNetMinor, l.NetMinor, l.TaxCategory, l.TaxRateBP, l.TaxMinor, l.GrossMinor); err != nil {
			return uuid.Nil, fmt.Errorf("invoicing: insert line %d: %w", i+1, err)
		}
	}

	if doc.Country == "SA" {
		if err := s.chainZATCA(ctx, tx, id, doc); err != nil {
			return uuid.Nil, err
		}
	}

	return id, nil
}

// nextNumber allocates the next position in a series.
//
// `FOR UPDATE` on one row. It serialises invoice issue per country/kind/year,
// which sounds alarming and is not: an invoice is issued once per order rather
// than once per request, the lock is held for the length of one insert, and the
// alternative — a gap in a tax sequence — is a finding rather than a
// performance problem.
func nextNumber(ctx context.Context, tx pgx.Tx, country string, kind Kind, year int) (uuid.UUID, int64, error) {
	var (
		id  uuid.UUID
		seq int64
	)

	// Created on first use rather than seeded, because a series is defined by
	// the first document that needs it and pre-creating every country × kind ×
	// year would be a migration every January.
	err := tx.QueryRow(ctx, `
		INSERT INTO invoice_series (country, kind, year, prefix, next_number)
		VALUES ($1, $2, $3, $4, 1)
		ON CONFLICT (country, kind, year) DO NOTHING
		RETURNING id`, country, string(kind), year, Prefix(kind)).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, fmt.Errorf("invoicing: ensure series: %w", err)
	}

	if err := tx.QueryRow(ctx, `
		SELECT id, next_number FROM invoice_series
		 WHERE country = $1 AND kind = $2 AND year = $3
		   FOR UPDATE`, country, string(kind), year).Scan(&id, &seq); err != nil {
		return uuid.Nil, 0, fmt.Errorf("invoicing: lock series: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE invoice_series SET next_number = next_number + 1 WHERE id = $1`, id); err != nil {
		return uuid.Nil, 0, fmt.Errorf("invoicing: advance series: %w", err)
	}

	return id, seq, nil
}

// chainZATCA appends the invoice to the KSA hash chain.
//
// Reads the previous link under the same transaction. The ICV and the previous
// hash are taken from the highest existing ICV rather than from a counter,
// because the chain is the authority — a counter that drifted from the chain
// would produce submissions ZATCA rejects with no way to tell which is right.
func (s *Service) chainZATCA(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID, doc Document) error {
	var (
		prevICV  int64
		prevHash string
	)
	err := tx.QueryRow(ctx, `
		SELECT icv, invoice_hash FROM zatca_submissions ORDER BY icv DESC LIMIT 1
		    FOR UPDATE`).Scan(&prevICV, &prevHash)
	if errors.Is(err, pgx.ErrNoRows) {
		prevICV, prevHash = 0, GenesisHash
	} else if err != nil {
		return fmt.Errorf("invoicing: read zatca chain head: %w", err)
	}

	// The canonical XML is not generated yet — signing needs a CSID that cannot
	// be obtained until KSA onboarding runs. Hashing the stable identity of the
	// document keeps the chain well-formed and ordered in the meantime, and the
	// submission stays `pending` so nothing pretends it was reported.
	payload := fmt.Sprintf("%s|%s|%d|%d|%s",
		invoiceID, doc.Currency, doc.GrossMinor, doc.TaxMinor, doc.SupplyDate.Format("2006-01-02"))
	hash := ChainHash([]byte(payload), prevHash)

	if _, err := tx.Exec(ctx, `
		INSERT INTO zatca_submissions (invoice_id, icv, invoice_hash, previous_hash, flow, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')`,
		invoiceID, prevICV+1, hash, prevHash, string(FlowFor(doc.Kind))); err != nil {
		return fmt.Errorf("invoicing: append to zatca chain: %w", err)
	}
	return nil
}

// CreditNote reverses an issued invoice.
//
// A new document with its own number, never an update. The lines are copied
// with their signs flipped, so the credit note's own totals close and the pair
// nets to zero — which is what makes "was this order refunded" answerable from
// the documents rather than from the ledger.
func (s *Service) CreditNote(ctx context.Context, invoiceID uuid.UUID, reason string) (uuid.UUID, error) {
	var id uuid.UUID

	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			doc          Document
			supplierType string
			supplierID   uuid.NullUUID
			buyerUser    uuid.NullUUID
			orderID      uuid.NullUUID
			status       string
		)
		err := tx.QueryRow(ctx, `
			SELECT kind, country, currency, supplier_type, supplier_id,
			       supplier_name, coalesce(supplier_trn, ''),
			       coalesce(buyer_name, ''), coalesce(buyer_trn, ''), buyer_user_id,
			       supply_date, order_id, status
			  FROM invoices WHERE id = $1 FOR UPDATE`, invoiceID).
			Scan(&doc.Kind, &doc.Country, &doc.Currency, &supplierType, &supplierID,
				&doc.Supplier.Name, &doc.Supplier.TRN,
				&doc.Buyer.Name, &doc.Buyer.TRN, &buyerUser,
				&doc.SupplyDate, &orderID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("invoicing: load invoice: %w", err)
		}
		if status != "issued" {
			return fmt.Errorf("%w: invoice is %s", httpx.ErrConflict, status)
		}

		rows, err := tx.Query(ctx, `
			SELECT description, coalesce(description_ar, ''), quantity, unit_net_minor,
			       net_minor, tax_category, tax_rate_bp, tax_minor, gross_minor
			  FROM invoice_lines WHERE invoice_id = $1 ORDER BY line_no`, invoiceID)
		if err != nil {
			return fmt.Errorf("invoicing: load lines: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var l Line
			if err := rows.Scan(&l.Description, &l.DescriptionAR, &l.Quantity, &l.UnitNetMinor,
				&l.NetMinor, &l.TaxCategory, &l.TaxRateBP, &l.TaxMinor, &l.GrossMinor); err != nil {
				return err
			}
			l.UnitNetMinor, l.NetMinor = -l.UnitNetMinor, -l.NetMinor
			l.TaxMinor, l.GrossMinor = -l.TaxMinor, -l.GrossMinor
			doc.Lines = append(doc.Lines, l)
			doc.NetMinor += l.NetMinor
			doc.TaxMinor += l.TaxMinor
			doc.GrossMinor += l.GrossMinor
		}
		if err := rows.Err(); err != nil {
			return err
		}

		doc.Kind = CreditNote
		if reason != "" {
			doc.Lines[0].Description += " — " + reason
		}

		refs := insertRefs{CorrectsInvoiceID: &invoiceID}
		if buyerUser.Valid {
			refs.BuyerUserID = &buyerUser.UUID
		}
		if supplierID.Valid {
			refs.VendorID = &supplierID.UUID
		}
		// The subject is inherited so the credit note is findable from whatever
		// the original was for. Without it a refunded order shows one document.
		if orderID.Valid {
			refs.OrderID = &orderID.UUID
		}

		id, err = s.insert(ctx, tx, doc, refs)
		return err
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// List returns a buyer's invoices.
func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]Summary, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, number, kind, currency, gross_minor, tax_minor, supply_date, issued_at, status
		  FROM invoices WHERE buyer_user_id = $1
		 ORDER BY issued_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("invoicing: list: %w", err)
	}
	defer rows.Close()

	var out []Summary
	for rows.Next() {
		var s Summary
		if err := rows.Scan(&s.ID, &s.Number, &s.Kind, &s.Currency, &s.GrossMinor,
			&s.TaxMinor, &s.SupplyDate, &s.IssuedAt, &s.Status); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// isUniqueViolation reports whether err is a 23505 on the named constraint.
//
// Named rather than any-unique-violation, same as `ordering` and
// `subscriptions`: catching all of them would swallow a genuine bug in an
// unrelated index and report it as "already invoiced".
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

type Summary struct {
	ID         uuid.UUID
	Number     string
	Kind       string
	Currency   string
	GrossMinor int64
	TaxMinor   int64
	SupplyDate time.Time
	IssuedAt   time.Time
	Status     string
}
