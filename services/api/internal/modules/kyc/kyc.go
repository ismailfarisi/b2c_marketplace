// Package kyc gates money leaving the platform.
//
// PLAN.md §7 lists it in one line — "Trade licence, Emirates ID / national ID,
// IBAN verification before first payout" — and the load-bearing words are
// *before first payout*. This is not a profile-completeness score that unlocks
// a badge. It is a precondition on a bank transfer, and `payouts.Issue` refuses
// without it.
//
// The rule the whole package exists to enforce, stated once:
//
//	An approved identity, a verified bank account, and neither of them expired.
//
// Every function here is in service of being able to answer that in one call at
// the moment a statement is issued, rather than trusting a flag set by whatever
// code path last touched the vendor.
package kyc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Tier is how much identity a subject needs.
//
// Not uniform, because the risk is not uniform. A private seller listing one
// car needs to be a real person. A dealer taking weekly payouts needs a trade
// licence and a bank account in the same legal name — and docs/README.md
// decision #7 (private sellers, dealers, or both) is precisely the question of
// which of these the market is made of.
type Tier string

const (
	// Basic — a verified phone and a name. What a private seller needs to list.
	Basic Tier = "basic"
	// Verified — government ID. What anyone receiving money needs.
	Verified Tier = "verified"
	// Enhanced — trade licence, signatory authority, ownership. What a company
	// receiving money needs.
	Enhanced Tier = "enhanced"
)

// RequiredTier returns the tier a subject must reach before being paid.
func RequiredTier(subjectType string) Tier {
	switch subjectType {
	case "vendor":
		return Enhanced
	case "driver", "user":
		return Verified
	default:
		return Enhanced
	}
}

// requiredDocuments is what each tier needs on file, verified and unexpired.
var requiredDocuments = map[Tier][]string{
	Basic:    {},
	Verified: {"emirates_id"},
	Enhanced: {"trade_licence", "authorised_signatory"},
}

// Status is the outcome of the eligibility check.
//
// A struct rather than a bool, because "why not" is the whole value. A vendor
// told only "you cannot be paid" opens a support ticket; a vendor told "your
// trade licence expired on 12 June" renews it.
type Status struct {
	Eligible bool
	// Reasons is empty when eligible, and every entry is something the subject
	// can act on.
	Reasons []string
}

func (s Status) Error() string {
	if s.Eligible {
		return ""
	}
	return fmt.Sprintf("kyc: not eligible for payout: %v", s.Reasons)
}

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Eligible answers the payout question.
//
// Recomputed from the documents on every call rather than read from a cached
// flag. The expensive-looking choice is the correct one here: the thing that
// makes a previously-eligible vendor ineligible is a *date passing*, and no
// write happens when a licence expires. A cached flag would keep paying them.
func (s *Service) Eligible(ctx context.Context, subjectType string, subjectID uuid.UUID) (Status, error) {
	return s.eligible(ctx, s.pool, subjectType, subjectID)
}

// EligibleTx is the same check inside a caller's transaction, so a statement
// cannot be issued in a transaction that raced the revocation of the case it
// checked.
func (s *Service) EligibleTx(ctx context.Context, tx pgx.Tx, subjectType string, subjectID uuid.UUID) (Status, error) {
	return s.eligible(ctx, tx, subjectType, subjectID)
}

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s *Service) eligible(ctx context.Context, q querier, subjectType string, subjectID uuid.UUID) (Status, error) {
	tier := RequiredTier(subjectType)
	st := Status{}

	var (
		caseID    uuid.UUID
		caseTier  Tier
		status    string
		expiresAt *time.Time
	)
	err := q.QueryRow(ctx, `
		SELECT id, tier, status, expires_at
		  FROM kyc_cases
		 WHERE subject_type = $1 AND subject_id = $2 AND status = 'approved'
		 ORDER BY CASE tier WHEN 'enhanced' THEN 3 WHEN 'verified' THEN 2 ELSE 1 END DESC
		 LIMIT 1`, subjectType, subjectID).Scan(&caseID, &caseTier, &status, &expiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		st.Reasons = append(st.Reasons, "no approved identity check on file")
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("kyc: load case: %w", err)
	}

	if rank(caseTier) < rank(tier) {
		st.Reasons = append(st.Reasons,
			fmt.Sprintf("identity is verified to %s, %s is required", caseTier, tier))
	}
	if expiresAt != nil && expiresAt.Before(time.Now()) {
		st.Reasons = append(st.Reasons,
			fmt.Sprintf("identity check expired on %s", expiresAt.Format("2 January 2006")))
	}

	// Documents, checked individually. A case approved six months ago whose
	// trade licence has since lapsed is not an approved case any more, and
	// nothing wrote to it when the date passed.
	missing, err := s.missingDocuments(ctx, q, caseID, tier)
	if err != nil {
		return st, err
	}
	st.Reasons = append(st.Reasons, missing...)

	// The bank account.
	var (
		acctStatus string
		holder     string
	)
	err = q.QueryRow(ctx, `
		SELECT status, holder_name FROM payout_accounts
		 WHERE owner_type = $1 AND owner_id = $2 AND status IN ('pending', 'verified')`,
		subjectType, subjectID).Scan(&acctStatus, &holder)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		st.Reasons = append(st.Reasons, "no bank account on file")
	case err != nil:
		return st, fmt.Errorf("kyc: load payout account: %w", err)
	case acctStatus != "verified":
		st.Reasons = append(st.Reasons, "bank account is awaiting verification")
	}

	st.Eligible = len(st.Reasons) == 0
	return st, nil
}

func (s *Service) missingDocuments(ctx context.Context, q querier, caseID uuid.UUID, tier Tier) ([]string, error) {
	required := requiredDocuments[tier]
	if len(required) == 0 {
		return nil, nil
	}

	rows, err := q.Query(ctx, `
		SELECT kind, expires_on FROM kyc_documents
		 WHERE case_id = $1 AND status = 'verified'`, caseID)
	if err != nil {
		return nil, fmt.Errorf("kyc: load documents: %w", err)
	}
	defer rows.Close()

	held := map[string]*time.Time{}
	for rows.Next() {
		var kind string
		var expires *time.Time
		if err := rows.Scan(&kind, &expires); err != nil {
			return nil, err
		}
		held[kind] = expires
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []string
	for _, kind := range required {
		expires, ok := held[kind]
		if !ok {
			out = append(out, "missing a verified "+readable(kind))
			continue
		}
		if expires != nil && expires.Before(time.Now()) {
			out = append(out, fmt.Sprintf("%s expired on %s",
				readable(kind), expires.Format("2 January 2006")))
		}
	}
	return out, nil
}

func rank(t Tier) int {
	switch t {
	case Enhanced:
		return 3
	case Verified:
		return 2
	default:
		return 1
	}
}

func readable(kind string) string {
	switch kind {
	case "trade_licence":
		return "trade licence"
	case "emirates_id":
		return "Emirates ID"
	case "national_id":
		return "national ID"
	case "authorised_signatory":
		return "authorised signatory letter"
	default:
		return kind
	}
}

// --- bank accounts ----------------------------------------------------------

// AddPayoutAccount records a bank account and retires the previous one.
//
// Every change re-enters verification, unconditionally. "The vendor changed
// their IBAN" is the single most common payout-fraud pattern there is — an
// attacker with access to a vendor's console does not need to steal anything,
// they need only redirect the next statement — and an account that inherited
// the old one's verified state is exactly what they want.
func (s *Service) AddPayoutAccount(ctx context.Context, in PayoutAccount) (uuid.UUID, error) {
	iban := NormaliseIBAN(in.IBAN)
	if err := ValidateIBAN(iban); err != nil {
		return uuid.Nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "iban", Code: "invalid", Message: err.Error(),
		}}}
	}
	if in.HolderName == "" {
		return uuid.Nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "holder_name", Code: "required",
			Message: "the account holder's name must match the name on the identity check",
		}}}
	}

	var id uuid.UUID
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE payout_accounts SET status = 'retired'
			 WHERE owner_type = $1 AND owner_id = $2 AND status IN ('pending', 'verified')`,
			in.OwnerType, in.OwnerID); err != nil {
			return fmt.Errorf("kyc: retire previous account: %w", err)
		}
		return tx.QueryRow(ctx, `
			INSERT INTO payout_accounts (owner_type, owner_id, iban, bank_name, holder_name, currency, country)
			VALUES ($1, $2, $3, nullif($4, ''), $5, $6, $7)
			RETURNING id`,
			in.OwnerType, in.OwnerID, iban, in.BankName, in.HolderName,
			in.Currency, iban[:2]).Scan(&id)
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

type PayoutAccount struct {
	OwnerType  string
	OwnerID    uuid.UUID
	IBAN       string
	BankName   string
	HolderName string
	Currency   string
}

// VerifyPayoutAccount marks an account checked by a human.
//
// The name comparison is fuzzy — "Karama Kitchen L.L.C." against "KARAMA
// KITCHEN LLC" — so a person does it rather than a regex. What the code
// enforces is that the comparison happened and who says so.
func (s *Service) VerifyPayoutAccount(ctx context.Context, accountID, reviewerID uuid.UUID, approve bool) error {
	status := "verified"
	if !approve {
		status = "rejected"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE payout_accounts
		   SET status = $2, verified_by = $3, verified_at = now()
		 WHERE id = $1 AND status = 'pending'`, accountID, status, reviewerID)
	if err != nil {
		return fmt.Errorf("kyc: verify account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: this account is not awaiting verification", httpx.ErrConflict)
	}
	return nil
}

// --- cases ------------------------------------------------------------------

// OpenCase starts or returns the open case for a subject.
func (s *Service) OpenCase(ctx context.Context, subjectType string, subjectID uuid.UUID, tier Tier) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO kyc_cases (subject_type, subject_id, tier, status)
		VALUES ($1, $2, $3, 'draft')
		ON CONFLICT (subject_type, subject_id, tier)
		  WHERE status IN ('draft', 'submitted', 'in_review', 'approved')
		DO UPDATE SET updated_at = now()
		RETURNING id`, subjectType, subjectID, string(tier)).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("kyc: open case: %w", err)
	}
	return id, nil
}

// Decide approves or rejects a case.
//
// Approval carries an expiry taken from the earliest document expiry on the
// case. That is the honest date: a case backed by a trade licence expiring in
// March is not verified past March, whatever anyone sets on the case itself.
func (s *Service) Decide(ctx context.Context, caseID, reviewerID uuid.UUID, approve bool, reason string) error {
	if !approve && reason == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "a rejected case with no reason is resubmitted unchanged",
		}}}
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if !approve {
			_, err := tx.Exec(ctx, `
				UPDATE kyc_cases
				   SET status = 'rejected', rejection_reason = $3,
				       reviewed_by = $2, reviewed_at = now()
				 WHERE id = $1 AND status IN ('submitted', 'in_review')`,
				caseID, reviewerID, reason)
			return err
		}

		var earliest *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT min(expires_on) FROM kyc_documents
			 WHERE case_id = $1 AND status = 'verified' AND expires_on IS NOT NULL`,
			caseID).Scan(&earliest); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("kyc: earliest document expiry: %w", err)
		}

		tag, err := tx.Exec(ctx, `
			UPDATE kyc_cases
			   SET status = 'approved', approved_at = now(), expires_at = $3,
			       reviewed_by = $2, reviewed_at = now(), rejection_reason = NULL
			 WHERE id = $1 AND status IN ('submitted', 'in_review')`,
			caseID, reviewerID, earliest)
		if err != nil {
			return fmt.Errorf("kyc: approve case: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: this case is not awaiting review", httpx.ErrConflict)
		}
		return nil
	})
}

// --- the review queue -------------------------------------------------------

// Case is one item awaiting a decision.
type Case struct {
	ID          uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	SubjectName string
	Tier        Tier
	Status      string
	CreatedAt   time.Time
	Documents   []Document
}

// Document is what a reviewer is looking at — or rather, what they are looking
// at a *reference* to.
type Document struct {
	ID          uuid.UUID
	Kind        string
	DocumentRef string
	NumberLast4 string
	IssuedOn    *time.Time
	ExpiresOn   *time.Time
	Status      string
}

// Queue lists cases awaiting review, with their documents.
//
// Written when the admin panel was built, and its absence was the finding: this
// module shipped a `Decide` endpoint and no way to reach a case. Reviewing was
// possible only for someone who already knew the case id, which meant it was
// not possible.
//
// Documents come back with the case rather than behind a second call. A
// reviewer decides by looking at the evidence, and a screen that lists cases and
// then fetches documents per row turns one decision into two round trips and
// a spinner between them.
func (s *Service) Queue(ctx context.Context, limit int) ([]Case, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.subject_type, c.subject_id, c.tier, c.status, c.created_at,
		       coalesce(v.legal_name, u.display_name, u.phone, '')
		  FROM kyc_cases c
		  LEFT JOIN vendors v ON c.subject_type = 'vendor' AND v.id = c.subject_id
		  LEFT JOIN users u   ON c.subject_type IN ('user', 'driver') AND u.id = c.subject_id
		 WHERE c.status IN ('submitted', 'in_review')
		 ORDER BY c.created_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("kyc: queue: %w", err)
	}
	defer rows.Close()

	var out []Case
	ids := make([]uuid.UUID, 0, limit)
	for rows.Next() {
		var c Case
		if err := rows.Scan(&c.ID, &c.SubjectType, &c.SubjectID, &c.Tier,
			&c.Status, &c.CreatedAt, &c.SubjectName); err != nil {
			return nil, err
		}
		out = append(out, c)
		ids = append(ids, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	docRows, err := s.pool.Query(ctx, `
		SELECT case_id, id, kind, coalesce(document_ref, ''), coalesce(number_last4, ''),
		       issued_on, expires_on, status
		  FROM kyc_documents WHERE case_id = ANY($1)
		 ORDER BY case_id, kind`, ids)
	if err != nil {
		return nil, fmt.Errorf("kyc: queue documents: %w", err)
	}
	defer docRows.Close()

	byCase := map[uuid.UUID][]Document{}
	for docRows.Next() {
		var caseID uuid.UUID
		var d Document
		if err := docRows.Scan(&caseID, &d.ID, &d.Kind, &d.DocumentRef,
			&d.NumberLast4, &d.IssuedOn, &d.ExpiresOn, &d.Status); err != nil {
			return nil, err
		}
		byCase[caseID] = append(byCase[caseID], d)
	}
	if err := docRows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		out[i].Documents = byCase[out[i].ID]
	}
	return out, nil
}

// PendingAccount is a bank account awaiting the name check.
type PendingAccount struct {
	ID uuid.UUID
	// OwnerName is the legal name on record. The reviewer's whole job is
	// comparing it to HolderName, so both are returned and neither is compared
	// here — "Karama Kitchen L.L.C." against "KARAMA KITCHEN LLC" is not a
	// regex.
	OwnerType  string
	OwnerID    uuid.UUID
	OwnerName  string
	HolderName string
	// MaskedIBAN, never the full number. A verification screen open on a shared
	// ops laptop does not need it, and the check digits have already been
	// validated in code.
	MaskedIBAN string
	BankName   string
	Currency   string
	CreatedAt  time.Time
}

// PendingAccounts lists bank accounts awaiting verification.
func (s *Service) PendingAccounts(ctx context.Context, limit int) ([]PendingAccount, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.owner_type, a.owner_id, a.holder_name, a.iban,
		       coalesce(a.bank_name, ''), a.currency, a.created_at,
		       coalesce(v.legal_name, u.display_name, '')
		  FROM payout_accounts a
		  LEFT JOIN vendors v ON a.owner_type = 'vendor' AND v.id = a.owner_id
		  LEFT JOIN drivers d ON a.owner_type = 'driver' AND d.id = a.owner_id
		  LEFT JOIN users u   ON u.id = d.user_id
		 WHERE a.status = 'pending'
		 ORDER BY a.created_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("kyc: pending accounts: %w", err)
	}
	defer rows.Close()

	var out []PendingAccount
	for rows.Next() {
		var a PendingAccount
		var iban string
		if err := rows.Scan(&a.ID, &a.OwnerType, &a.OwnerID, &a.HolderName, &iban,
			&a.BankName, &a.Currency, &a.CreatedAt, &a.OwnerName); err != nil {
			return nil, err
		}
		a.MaskedIBAN = MaskIBAN(iban)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ExpireLapsedCases marks approved cases whose evidence has run out.
//
// Run on a tick. Belt and braces: `Eligible` already refuses an expired case,
// so this changes no payout outcome — what it changes is visibility. A vendor
// whose licence lapsed should see it in their console the next morning rather
// than discover it on the day a payout does not arrive.
func (s *Service) ExpireLapsedCases(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE kyc_cases SET status = 'expired'
		 WHERE status = 'approved' AND expires_at IS NOT NULL AND expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("kyc: expire cases: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
