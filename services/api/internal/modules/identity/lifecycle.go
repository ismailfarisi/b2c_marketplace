package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Account lifecycle — the customer mutations the console left unbuilt.
//
// All three were deferred with the note that no safe service method existed and
// a half-working form is worse than none. These are the methods.
//
// They live in `identity` because it owns `users`, and they take an actor and a
// reason for the same purpose everything else in the console does. What is
// different here is how much of each one is a *refusal*: the interesting logic
// is the list of situations in which the platform declines to do what it was
// asked, because every one of those is a way to corrupt something that cannot
// be corrected afterwards.

// Auditor is the one method these need from admin.Service.
type Auditor interface {
	Record(ctx context.Context, tx pgx.Tx, a admin.Action) error
}

// WithAuditor returns a copy that records what it does.
//
// Same shape as payouts, and the same reason: the sign-in path constructs this
// service with no actor and must not be able to perform staff actions at all.
func (s *Service) WithAuditor(a Auditor) *Service {
	clone := *s
	clone.audit = a
	return &clone
}

// ── phone change ────────────────────────────────────────────────────────────

// ChangePhoneInput moves an account to a new number.
type ChangePhoneInput struct {
	UserID  uuid.UUID
	Phone   string
	ActorID uuid.UUID
	Reason  string
}

// ChangePhone moves an account to a new number and forces re-verification.
//
// The phone is the login credential here, so this is closer to a password reset
// than to editing a profile field. Three consequences follow and all three are
// implemented rather than described:
//
// `phone_verified_at` is cleared, so the new number has to prove itself before
// it can be trusted. Every session is revoked, because the person holding the
// old device should not keep a live token to an account that now answers to a
// different number. And a number already in use is refused rather than merged —
// merging is a separate decision with separate consequences, and doing it
// silently as a side effect of a phone change is how two people end up sharing
// an order history.
func (s *Service) ChangePhone(ctx context.Context, in ChangePhoneInput) error {
	if err := requireReason(in.Reason,
		"changing a login credential is a security event, and the reason is what "+
			"distinguishes a support request from an account takeover"); err != nil {
		return err
	}

	phone := strings.TrimSpace(in.Phone)
	if !looksLikePhone(phone) {
		return field("phone", "invalid",
			"a phone number in international format is required, e.g. +9715XXXXXXXX")
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var oldPhone string
		var status string
		var merged *uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT phone, status::text, merged_into_user_id
			  FROM users WHERE id = $1 FOR UPDATE`, in.UserID).
			Scan(&oldPhone, &status, &merged); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load user: %w", err)
		}
		if merged != nil {
			return fmt.Errorf("%w: this account has been merged into another one — "+
				"change the number on the surviving account", httpx.ErrConflict)
		}
		if oldPhone == phone {
			return nil // idempotent
		}

		var taken uuid.UUID
		err := tx.QueryRow(ctx,
			`SELECT id FROM users WHERE phone = $1 AND id <> $2`, phone, in.UserID).Scan(&taken)
		if err == nil {
			return fmt.Errorf("%w: %s already belongs to another account. If they are the "+
				"same person, merge the accounts — a phone change must not quietly join two "+
				"order histories", httpx.ErrConflict, phone)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check phone: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE users
			   SET phone = $2, phone_verified_at = NULL, updated_at = now()
			 WHERE id = $1`, in.UserID, phone); err != nil {
			return fmt.Errorf("change phone: %w", err)
		}

		// Every session, not just the current one. The old device keeps a valid
		// token otherwise, on an account that now answers to a number its holder
		// may not control.
		if _, err := tx.Exec(ctx,
			`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
			in.UserID); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}

		return s.record(ctx, tx, in.ActorID, "user.change_phone", in.UserID, in.Reason,
			// The old number is recorded. It is the only way to answer "what was
			// this account before" during a takeover investigation, and the
			// audit log is already the most restricted table here.
			map[string]any{"phone": oldPhone},
			map[string]any{"phone": phone, "sessions_revoked": true})
	})
}

// ── merge ───────────────────────────────────────────────────────────────────

// MergeInput folds a duplicate account into a surviving one.
type MergeInput struct {
	// DuplicateID stops existing. SurvivorID keeps going.
	DuplicateID uuid.UUID
	SurvivorID  uuid.UUID
	ActorID     uuid.UUID
	Reason      string
}

// MergeResult reports what moved and what deliberately did not.
type MergeResult struct {
	AddressesMoved  int
	FavouritesMoved int
	SearchesMoved   int
	// OrdersLeft and InvoicesLeft stay on the duplicate. Reported so the
	// operator sees the cost of the merge rather than discovering it when the
	// customer asks where their receipts went.
	OrdersLeft   int
	InvoicesLeft int
}

// Merge links a duplicate to a survivor and moves what can safely move.
//
// It does **not** repoint orders, payments, invoices or ledger references, and
// that is the whole design rather than an unfinished part of it. A tax invoice
// names a buyer; an invoice whose buyer changed after issue is a different
// document from the one the customer holds and the one the tax authority has.
// The ledger is append-only. Payments carry the identity the gateway authorised
// against.
//
// What moves is what names no financial document and is plainly the person's
// rather than the account's: addresses, favourites, saved searches and
// notification preferences.
//
// The duplicate is then frozen — status suspended, sessions revoked, merge
// recorded — so nobody can sign in to a history that is no longer the whole
// story.
func (s *Service) Merge(ctx context.Context, in MergeInput) (*MergeResult, error) {
	if err := requireReason(in.Reason,
		"a merge is irreversible and the reason is the only record of why these two "+
			"accounts were judged to be one person"); err != nil {
		return nil, err
	}
	if in.DuplicateID == in.SurvivorID {
		return nil, field("duplicate_id", "same", "an account cannot be merged into itself")
	}

	result := &MergeResult{}
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		dup, err := lockForMerge(ctx, tx, in.DuplicateID, "duplicate")
		if err != nil {
			return err
		}
		if _, err := lockForMerge(ctx, tx, in.SurvivorID, "survivor"); err != nil {
			return err
		}

		// Refusals that protect money. A duplicate with an order still in
		// flight has a driver on the way to an address that is about to move,
		// and a duplicate holding a wallet balance would have it stranded on a
		// frozen account.
		var openOrders int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM orders
			 WHERE user_id = $1 AND status NOT IN ('completed', 'cancelled')`,
			in.DuplicateID).Scan(&openOrders); err != nil {
			return fmt.Errorf("count open orders: %w", err)
		}
		if openOrders > 0 {
			return fmt.Errorf("%w: this account has %d order(s) still in progress. "+
				"Merging now would freeze an account a driver is currently delivering to — "+
				"wait for them to finish or cancel them first", httpx.ErrConflict, openOrders)
		}

		var activeSubs int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM subscriptions
			 WHERE user_id = $1 AND status = 'active'`, in.DuplicateID).Scan(&activeSubs); err != nil {
			return fmt.Errorf("count subscriptions: %w", err)
		}
		if activeSubs > 0 {
			return fmt.Errorf("%w: this account has %d active subscription(s), which bill "+
				"against it. Cancel or transfer them before merging", httpx.ErrConflict, activeSubs)
		}

		// What moves. Each of these names no document and is plainly the
		// person's rather than the account's.
		//
		// ON CONFLICT DO NOTHING throughout: a duplicate account usually has the
		// same home address saved, and a merge that fails on a unique index
		// because the customer is consistent would be absurd.
		if err := tx.QueryRow(ctx, `
			WITH moved AS (
				UPDATE addresses SET user_id = $2, is_default = false
				 WHERE user_id = $1 RETURNING 1)
			SELECT count(*) FROM moved`, in.DuplicateID, in.SurvivorID).
			Scan(&result.AddressesMoved); err != nil {
			return fmt.Errorf("move addresses: %w", err)
		}

		if err := tx.QueryRow(ctx, `
			WITH moved AS (
				INSERT INTO listing_favorites (user_id, listing_id, created_at)
				SELECT $2, listing_id, created_at FROM listing_favorites WHERE user_id = $1
				ON CONFLICT DO NOTHING
				RETURNING 1)
			SELECT count(*) FROM moved`, in.DuplicateID, in.SurvivorID).
			Scan(&result.FavouritesMoved); err != nil {
			return fmt.Errorf("move favourites: %w", err)
		}

		if err := tx.QueryRow(ctx, `
			WITH moved AS (
				UPDATE saved_searches SET user_id = $2 WHERE user_id = $1 RETURNING 1)
			SELECT count(*) FROM moved`, in.DuplicateID, in.SurvivorID).
			Scan(&result.SearchesMoved); err != nil {
			return fmt.Errorf("move saved searches: %w", err)
		}

		// What stays, counted so the operator sees the cost.
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM orders WHERE user_id = $1`, in.DuplicateID).
			Scan(&result.OrdersLeft); err != nil {
			return fmt.Errorf("count orders: %w", err)
		}
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM invoices WHERE buyer_user_id = $1`, in.DuplicateID).
			Scan(&result.InvoicesLeft); err != nil {
			return fmt.Errorf("count invoices: %w", err)
		}

		// The database enforces all of this with a trigger, and that trigger is
		// the thing actually keeping the invariant. Checking it here as well is
		// not redundancy for its own sake: a raw plpgsql RAISE surfaces as a
		// 500 with no usable message, and an operator told "internal server
		// error" when they have merged the wrong way round will try again.
		var inbound int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM users WHERE merged_into_user_id = $1`, in.DuplicateID).
			Scan(&inbound); err != nil {
			return fmt.Errorf("check inbound merges: %w", err)
		}
		if inbound > 0 {
			return fmt.Errorf("%w: %d account(s) have already been merged into this one, so "+
				"it is a survivor rather than a duplicate. Merging it now would hide those "+
				"accounts behind two hops", httpx.ErrConflict, inbound)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE users
			   SET merged_into_user_id = $2, merged_at = now(),
			       status = 'suspended', updated_at = now()
			 WHERE id = $1`, in.DuplicateID, in.SurvivorID); err != nil {
			return fmt.Errorf("mark merged: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
			in.DuplicateID); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}

		return s.record(ctx, tx, in.ActorID, "user.merge", in.DuplicateID, in.Reason,
			map[string]any{"phone": dup.phone, "status": dup.status},
			map[string]any{
				"merged_into":      in.SurvivorID,
				"addresses_moved":  result.AddressesMoved,
				"favourites_moved": result.FavouritesMoved,
				"searches_moved":   result.SearchesMoved,
				"orders_left":      result.OrdersLeft,
				"invoices_left":    result.InvoicesLeft,
			})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ── erasure ─────────────────────────────────────────────────────────────────

// EraseInput pseudonymises an account under a data request.
type EraseInput struct {
	UserID  uuid.UUID
	ActorID uuid.UUID
	Reason  string
}

// Erase removes everything that identifies the person and keeps the rest.
//
// Not a delete, and the distinction is the point. The platform is required to
// retain tax invoices for years; the ledger is append-only by design. A request
// to be forgotten cannot remove those and a system that pretended to would be
// lying to the customer and breaking the law in the same operation.
//
// What it does instead: the name, email and phone are replaced, addresses and
// device tokens are deleted, and the account is closed. The financial record
// stays, attached to a subject nobody can resolve to a human — which is what
// "erased" can honestly mean for a business that has to keep books.
//
// The phone is replaced rather than nulled because it is NOT NULL and is the
// login key. A deterministic placeholder derived from the user id keeps it
// unique without being a number anyone can be reached on.
func (s *Service) Erase(ctx context.Context, in EraseInput) error {
	if err := requireReason(in.Reason,
		"an erasure is irreversible — the reason is the record that it was requested "+
			"rather than performed by mistake"); err != nil {
		return err
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		var erased *string
		if err := tx.QueryRow(ctx, `
			SELECT status::text, erased_at::text FROM users WHERE id = $1 FOR UPDATE`,
			in.UserID).Scan(&status, &erased); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load user: %w", err)
		}
		if erased != nil {
			return nil // idempotent
		}

		var openOrders int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM orders
			 WHERE user_id = $1 AND status NOT IN ('completed', 'cancelled')`,
			in.UserID).Scan(&openOrders); err != nil {
			return fmt.Errorf("count open orders: %w", err)
		}
		if openOrders > 0 {
			return fmt.Errorf("%w: %d order(s) are still in progress. Erasing the address "+
				"and phone of a customer a driver is on the way to would strand the delivery",
				httpx.ErrConflict, openOrders)
		}

		// Money owed *to* the customer blocks erasure, because paying it
		// afterwards is impossible: there would be nobody to pay.
		// Returned, never swallowed. The first version of this caught the error
		// and carried on, which is invalid inside a transaction: a failed
		// statement aborts it, and every statement after fails with 25P02 while
		// the real cause is three errors upstream. A check that cannot run is a
		// check that has not run, and erasure is not the place to guess.
		var refundable int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM disputes
			 WHERE raised_by_type = 'user' AND raised_by_id = $1
			   AND state NOT IN ('resolved', 'rejected', 'withdrawn')`,
			in.UserID).Scan(&refundable); err != nil {
			return fmt.Errorf("count open disputes: %w", err)
		}
		if refundable > 0 {
			return fmt.Errorf("%w: %d open dispute(s) may owe this customer money, and "+
				"there would be nobody left to pay. Resolve them first",
				httpx.ErrConflict, refundable)
		}

		placeholder := erasedPhone(in.UserID)
		if _, err := tx.Exec(ctx, `
			UPDATE users
			   SET phone = $2, phone_verified_at = NULL,
			       email = NULL, email_verified_at = NULL,
			       display_name = NULL,
			       status = 'deleted', erased_at = now(),
			       deleted_at = coalesce(deleted_at, now()),
			       updated_at = now()
			 WHERE id = $1`, in.UserID, placeholder); err != nil {
			return fmt.Errorf("erase user: %w", err)
		}

		for _, stmt := range []string{
			`DELETE FROM addresses WHERE user_id = $1`,
			`DELETE FROM saved_searches WHERE user_id = $1`,
			`DELETE FROM listing_favorites WHERE user_id = $1`,
			`DELETE FROM viewer_interests WHERE user_id = $1`,
			`UPDATE sessions SET revoked_at = now(), push_token = NULL
			  WHERE user_id = $1 AND revoked_at IS NULL`,
		} {
			if _, err := tx.Exec(ctx, stmt, in.UserID); err != nil {
				return fmt.Errorf("erase related data: %w", err)
			}
		}

		return s.record(ctx, tx, in.ActorID, "user.erase", in.UserID, in.Reason,
			// Deliberately empty. Recording what was erased in the audit log
			// would put the erased data in the audit log, which defeats the
			// operation — the record is that it happened, by whom, and why.
			map[string]any{"redacted": true},
			map[string]any{"erased": true, "financial_records": "retained"})
	})
}

// ── helpers ─────────────────────────────────────────────────────────────────

type mergeSubject struct {
	phone  string
	status string
}

func lockForMerge(ctx context.Context, tx pgx.Tx, id uuid.UUID, role string) (mergeSubject, error) {
	var m mergeSubject
	var merged *uuid.UUID
	var erased *string
	err := tx.QueryRow(ctx, `
		SELECT phone, status::text, merged_into_user_id, erased_at::text
		  FROM users WHERE id = $1 FOR UPDATE`, id).
		Scan(&m.phone, &m.status, &merged, &erased)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, fmt.Errorf("%w: no %s account with that id", httpx.ErrNotFound, role)
	}
	if err != nil {
		return m, fmt.Errorf("load %s: %w", role, err)
	}
	if merged != nil {
		return m, fmt.Errorf("%w: the %s has already been merged into another account",
			httpx.ErrConflict, role)
	}
	if erased != nil {
		return m, fmt.Errorf("%w: the %s has been erased under a data request and cannot "+
			"be merged — there is nothing left to identify it with", httpx.ErrConflict, role)
	}
	return m, nil
}

func (s *Service) record(ctx context.Context, tx pgx.Tx, actorID uuid.UUID,
	action string, subjectID uuid.UUID, reason string, before, after map[string]any) error {

	if s.audit == nil {
		return fmt.Errorf("%w: this service was built without an auditor and cannot perform "+
			"staff actions on accounts", httpx.ErrConflict)
	}
	return s.audit.Record(ctx, tx, admin.Action{
		ActorID:     actorID,
		Action:      action,
		SubjectType: "user",
		SubjectID:   uuid.NullUUID{UUID: subjectID, Valid: true},
		Reason:      reason,
		Before:      before,
		After:       after,
	})
}

func requireReason(reason, why string) error {
	if strings.TrimSpace(reason) == "" {
		return field("reason", "required", why)
	}
	return nil
}

func field(name, code, message string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{
		Field: name, Code: code, Message: message,
	}}}
}

// erasedPhone builds a unique, non-routable stand-in for a deleted number.
//
// `phone` is NOT NULL, UNIQUE and the login key, so an erasure cannot simply
// clear it — it has to put something there that no two erasures collide on and
// nobody can be reached on.
//
// The prefix is +999, which the ITU reserves and never assigns to a country, so
// the result is well-formed E.164 that cannot route anywhere. The first version
// of this used +000000 followed by hex from the uuid and was rejected by the
// `users_phone_e164` check constraint on two counts: E.164 forbids a leading
// zero after the +, and a phone number has no letters in it. The constraint
// caught it, which is the constraint doing its job.
//
// Twelve digits of the uuid follow. Not cryptographic — collision resistance
// here only needs to beat "two erasures on the same day", and a uuid's first
// six bytes give ~2.8e14 values.
func erasedPhone(id uuid.UUID) string {
	var b strings.Builder
	b.WriteString("+999")
	for _, c := range id.String() {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		} else if c >= 'a' && c <= 'f' {
			// Fold hex letters into digits rather than dropping them, so two
			// uuids differing only in their letters do not collide.
			b.WriteRune('0' + (c-'a')%10)
		}
		if b.Len() >= 15 {
			break
		}
	}
	return b.String()
}

// looksLikePhone is a shape check, not a validity check.
//
// Deliberately weak: the authoritative test is whether an OTP arrives, and this
// only exists to catch a pasted name or a local-format number before it becomes
// a login credential nobody can use.
func looksLikePhone(p string) bool {
	if !strings.HasPrefix(p, "+") || len(p) < 8 || len(p) > 20 {
		return false
	}
	for _, r := range p[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
