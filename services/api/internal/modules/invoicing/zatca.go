package invoicing

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// ZATCA — KSA e-invoicing (Fatoora), Phase 2.
//
// PLAN.md §7 calls this "weeks of work — scope before committing to a KSA date"
// and docs/README.md decision #6 ("KSA in year one?") is still open. What is
// built here is deliberately the part that *cannot* be deferred, and only that
// part.
//
// The deferrable half is the cryptography: signing requires a CSID issued by
// ZATCA against a real commercial registration, obtained through an onboarding
// flow that cannot be run speculatively. Writing an unsigned XML generator now
// would produce code nobody can test against anything.
//
// The non-deferrable half is the **hash chain**. Every invoice's hash includes
// the hash of the one before it, so the chain is only sound if it has been
// unbroken since the first KSA invoice ever issued. There is no retrofit: you
// cannot go back and chain invoices that were issued without one, because
// altering an issued invoice is precisely what the chain exists to detect. If
// KSA launches and the first month of invoices has no ICV and no previous hash,
// the remedy is re-issuing them to customers.
//
// So the chain ships now, empty, and starts working the day a KSA invoice is
// first issued.

// GenesisHash is the value ZATCA specifies for the first invoice in a chain:
// the base64 SHA-256 of the single character "0".
//
// A constant rather than NULL so verification has one code path instead of two.
// A nullable previous-hash means every consumer of this table writes `if
// previous == nil` and one of them eventually gets it wrong.
const GenesisHash = "NWZlY2ViNjZmZmM4NmYzOGQ5NTI3ODZjNmQ2OTZjNzljMmRiYzIzOWRkNGU5MWI0NjcyOWQ3M2EyN2ZiNTdlOQ=="

// Flow is how an invoice reaches ZATCA.
type Flow string

const (
	// Reporting — simplified (B2C) invoices, reported within 24 hours. The
	// customer gets the invoice immediately.
	Reporting Flow = "reporting"
	// Clearance — standard (B2B) invoices, which must be cleared by ZATCA
	// *before* being given to the buyer. This changes when the PDF may be
	// emailed, which is why the flow is stored rather than inferred.
	Clearance Flow = "clearance"
)

// FlowFor picks the submission flow from the document kind.
func FlowFor(k Kind) Flow {
	if k == Simplified {
		return Reporting
	}
	return Clearance
}

// ChainHash computes an invoice's hash from its canonical XML and its
// predecessor's hash.
//
// The previous hash is prepended to the digest input, which is what makes the
// chain a chain: changing any earlier invoice changes every hash after it.
func ChainHash(canonicalXML []byte, previousHash string) string {
	h := sha256.New()
	h.Write([]byte(previousHash))
	h.Write(canonicalXML)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// VerifyChain checks a run of submissions.
//
// Returns the index of the first break, or -1 when the chain is intact. Used by
// a reconciliation job rather than on the write path, because the write path
// enforces the chain by construction and the job exists to catch the case where
// it did not.
func VerifyChain(hashes []ChainLink) int {
	for i, link := range hashes {
		want := GenesisHash
		if i > 0 {
			want = hashes[i-1].Hash
		}
		if link.PreviousHash != want {
			return i
		}
		if i > 0 && link.ICV != hashes[i-1].ICV+1 {
			// A gap in the counter is a break even when the hashes line up,
			// which is exactly what a deleted-then-reinserted row looks like.
			return i
		}
	}
	return -1
}

type ChainLink struct {
	ICV          int64
	Hash         string
	PreviousHash string
}

// QRTLV builds the TLV-encoded QR payload printed on a KSA invoice.
//
// The format is tag-length-value with single-byte tags and lengths, base64 of
// the concatenation. Five mandatory fields for a simplified invoice, in this
// order — the order is part of the specification, not a convention:
//
//	1  seller name
//	2  seller VAT registration number
//	3  timestamp, ISO 8601
//	4  invoice total including VAT
//	5  VAT total
//
// Written out by hand rather than pulled from a library because it is thirty
// lines, it is exactly specified, and the alternative is a dependency whose
// correctness we could not check either.
func QRTLV(sellerName, sellerVAT string, at time.Time, totalWithVAT, vatTotal string) (string, error) {
	fields := []struct {
		tag   byte
		value string
	}{
		{1, sellerName},
		{2, sellerVAT},
		{3, at.UTC().Format(time.RFC3339)},
		{4, totalWithVAT},
		{5, vatTotal},
	}

	var buf []byte
	for _, f := range fields {
		v := []byte(f.value)
		if len(v) > 255 {
			// A single-byte length cannot describe it. Truncating would produce
			// a QR that scans and lies; failing means someone notices.
			return "", fmt.Errorf("zatca: field %d is %d bytes, over the 255-byte TLV limit",
				f.tag, len(v))
		}
		if len(v) == 0 {
			return "", fmt.Errorf("zatca: field %d is mandatory and empty", f.tag)
		}
		buf = append(buf, f.tag, byte(len(v)))
		buf = append(buf, v...)
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// ParseQRTLV decodes a payload back into its fields.
//
// Exists for the test and for support: "the customer says the QR does not
// scan" is otherwise unanswerable without a phone.
func ParseQRTLV(payload string) (map[byte]string, error) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("zatca: not base64: %w", err)
	}
	out := map[byte]string{}
	for i := 0; i < len(raw); {
		if i+2 > len(raw) {
			return nil, fmt.Errorf("zatca: truncated TLV header at byte %d", i)
		}
		tag, length := raw[i], int(raw[i+1])
		i += 2
		if i+length > len(raw) {
			return nil, fmt.Errorf("zatca: tag %d claims %d bytes, %d remain", tag, length, len(raw)-i)
		}
		out[tag] = string(raw[i : i+length])
		i += length
	}
	return out, nil
}

// ValidateKSAVATNumber checks the shape ZATCA requires.
//
// 15 digits, first and last both '3'. Shape-checked here as well as in the
// database because the failure otherwise surfaces as a rejected submission days
// later, by which time the invoice has been given to a customer.
func ValidateKSAVATNumber(n string) error {
	n = strings.TrimSpace(n)
	if len(n) != 15 {
		return fmt.Errorf("zatca: VAT number must be 15 digits, got %d", len(n))
	}
	for i, r := range n {
		if r < '0' || r > '9' {
			return fmt.Errorf("zatca: VAT number has a non-digit at position %d", i+1)
		}
	}
	if n[0] != '3' || n[14] != '3' {
		return fmt.Errorf("zatca: VAT number must start and end with 3")
	}
	return nil
}
