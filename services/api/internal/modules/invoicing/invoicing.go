// Package invoicing issues tax invoices.
//
// The requirement that shapes this package is one sentence from DATA-MODEL.md
// §9: invoices need **sequential legal numbering**. That is the only identifier
// in this system a UUID cannot serve and a Postgres sequence cannot either —
// sequences skip on rollback by design, and a skipped number is a gap an
// auditor asks about.
//
// Everything else follows from it:
//
//   - Numbers are allocated under a row lock, inside the transaction that
//     inserts the invoice, so a rolled-back invoice takes its number back with
//     it.
//   - Issued invoices are never updated. A correction is a credit note, which
//     is a new document with its own number pointing at the old one.
//   - Supplier and buyer are snapshotted. A vendor who changes their legal name
//     has not changed what last month's invoice said, and reprinting from live
//     rows would quietly forge it.
package invoicing

import (
	"fmt"
	"strings"
	"time"
)

// Kind is which document this is.
type Kind string

const (
	// TaxInvoice is the full document: buyer details, TRN, line-level tax.
	TaxInvoice Kind = "tax_invoice"
	// Simplified is the B2C document. Both markets permit it below a threshold
	// and it omits the buyer's TRN — which is the normal case here, because a
	// person ordering dinner does not have one.
	Simplified Kind = "simplified"
	// CreditNote reverses part or all of an invoice.
	CreditNote Kind = "credit_note"
)

// SimplifiedThresholdMinor is the value below which a simplified invoice is
// permitted.
//
// 1,000 AED. KSA's threshold differs and the KSA path is not live (docs/README
// decision #6), so this is deliberately a single number with a comment rather
// than a jurisdiction table nobody has needed yet.
const SimplifiedThresholdMinor int64 = 100_000

// KindFor picks the document a transaction requires.
//
// A buyer TRN forces a full tax invoice regardless of value: a business
// customer who supplies their TRN is telling you they intend to reclaim the
// input tax, and they cannot do that from a simplified invoice.
func KindFor(grossMinor int64, buyerTRN string) Kind {
	if strings.TrimSpace(buyerTRN) != "" {
		return TaxInvoice
	}
	if grossMinor <= SimplifiedThresholdMinor {
		return Simplified
	}
	return TaxInvoice
}

// Prefix is the human-readable part of a number.
func Prefix(kind Kind) string {
	switch kind {
	case CreditNote:
		return "CRN"
	case Simplified:
		return "SIN"
	default:
		return "INV"
	}
}

// FormatNumber renders a series position as a printed invoice number.
//
// Zero-padded to six digits, which is 999,999 documents per country per kind
// per year before the width changes. The padding is cosmetic; the sequence is
// not, and the number is unique platform-wide because the country and year are
// in it.
func FormatNumber(prefix, country string, year int, seq int64) string {
	return fmt.Sprintf("%s-%s-%d-%06d", prefix, strings.ToUpper(country), year, seq)
}

// SupplyDate is the date the tax point falls on.
//
// Not the date of issue, and the distinction has consequences: an order
// delivered on 31 December and invoiced on 2 January belongs to the earlier tax
// period and takes the rate in force then. Passing `issuedAt` where this is
// expected is the bug that puts revenue in the wrong quarter.
func SupplyDate(deliveredAt, placedAt time.Time) time.Time {
	if !deliveredAt.IsZero() {
		return deliveredAt
	}
	return placedAt
}

// Line is one row of an invoice, already decomposed by the tax package.
type Line struct {
	Description   string
	DescriptionAR string
	Quantity      float64
	UnitNetMinor  int64
	NetMinor      int64
	TaxCategory   string
	TaxRateBP     int
	TaxMinor      int64
	GrossMinor    int64
}

// Party is a snapshot of one side of the transaction.
type Party struct {
	Name    string
	TRN     string
	Address string
}

// Document is a fully-formed invoice, before it has a number.
type Document struct {
	Kind       Kind
	Country    string
	Currency   string
	Supplier   Party
	Buyer      Party
	SupplyDate time.Time
	Lines      []Line
	NetMinor   int64
	TaxMinor   int64
	GrossMinor int64
}

// Validate catches the ways an invoice can be legally void before it acquires a
// number it can never give back.
//
// Order matters here in one place: totals are checked before the supplier's
// TRN, because a document that does not add up is a bug in this code and a
// document with no TRN is a bug in the vendor's onboarding. Reporting the
// second when the first is also true sends someone to the wrong place.
func (d Document) Validate() error {
	if len(d.Lines) == 0 {
		return fmt.Errorf("invoicing: an invoice with no lines describes nothing")
	}

	var net, tax, gross int64
	for i, l := range d.Lines {
		if l.NetMinor+l.TaxMinor != l.GrossMinor {
			return fmt.Errorf("invoicing: line %d does not close: %d + %d != %d",
				i+1, l.NetMinor, l.TaxMinor, l.GrossMinor)
		}
		net += l.NetMinor
		tax += l.TaxMinor
		gross += l.GrossMinor
	}
	if net != d.NetMinor || tax != d.TaxMinor || gross != d.GrossMinor {
		return fmt.Errorf("invoicing: totals (%d/%d/%d) disagree with the lines (%d/%d/%d)",
			d.NetMinor, d.TaxMinor, d.GrossMinor, net, tax, gross)
	}

	if strings.TrimSpace(d.Supplier.Name) == "" {
		return fmt.Errorf("invoicing: a tax invoice needs a supplier name")
	}
	// A supplier TRN is required on a full tax invoice and on a simplified one.
	// The difference between the two documents is the *buyer's* details, not the
	// supplier's — a common misreading, and the one that produces invoices a
	// buyer's accountant rejects.
	if strings.TrimSpace(d.Supplier.TRN) == "" && d.TaxMinor > 0 {
		return fmt.Errorf("invoicing: tax was charged but the supplier has no registration number")
	}
	if d.Kind == TaxInvoice && strings.TrimSpace(d.Buyer.Name) == "" {
		return fmt.Errorf("invoicing: a full tax invoice needs a buyer name")
	}
	if d.SupplyDate.IsZero() {
		return fmt.Errorf("invoicing: a tax point is required")
	}
	return nil
}
