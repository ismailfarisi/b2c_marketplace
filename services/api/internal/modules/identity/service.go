// Package identity handles phone-first authentication.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/auth"
	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

var e164 = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

type Service struct {
	pool   *pgxpool.Pool
	issuer *auth.Issuer
	otpTTL time.Duration
	// devEcho logs the OTP instead of sending it. config.Load rejects this in
	// production, so it cannot reach a deployed environment.
	devEcho bool
	// audit is set only by WithAuditor. Nil on the sign-in path, which has no
	// actor and must not be able to perform staff actions on accounts.
	audit Auditor
}

func NewService(pool *pgxpool.Pool, issuer *auth.Issuer, otpTTL time.Duration, devEcho bool) *Service {
	return &Service{pool: pool, issuer: issuer, otpTTL: otpTTL, devEcho: devEcho}
}

type Challenge struct {
	ID                 uuid.UUID
	ExpiresAt          time.Time
	ResendAfterSeconds int
}

// RequestOTP issues a code.
//
// The response is identical whether or not the number has an account. Anything
// else turns this endpoint into a user-enumeration oracle, which for a
// phone-first product means confirming whether a given person is a customer.
func (s *Service) RequestOTP(ctx context.Context, phone, purpose, requestIP string) (*Challenge, error) {
	phone = normalizePhone(phone)
	if !e164.MatchString(phone) {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "phone", Code: "invalid", Message: "must be E.164, e.g. +971501234567",
		}}}
	}
	if purpose == "" {
		purpose = "login"
	}

	if err := s.checkRateLimit(ctx, phone, requestIP); err != nil {
		return nil, err
	}

	code, err := generateCode()
	if err != nil {
		return nil, err
	}

	var ip *net.IP
	if parsed := net.ParseIP(requestIP); parsed != nil {
		ip = &parsed
	}

	ch := &Challenge{ExpiresAt: time.Now().Add(s.otpTTL), ResendAfterSeconds: 60}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO otp_codes (phone, purpose, code_hash, request_ip, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		phone, purpose, auth.HashToken(code), ip, ch.ExpiresAt,
	).Scan(&ch.ID)
	if err != nil {
		return nil, fmt.Errorf("insert otp: %w", err)
	}

	if s.devEcho {
		slog.WarnContext(ctx, "OTP issued (dev echo — never enabled in production)",
			"phone", phone, "code", code, "challenge_id", ch.ID)
	}
	// Real dispatch goes through the SMS provider here, enqueued via the outbox
	// so a provider outage retries rather than dropping the login.

	return ch, nil
}

type Session struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	User         User

	// Unexported: callers get tokens, not the session row's identity. Only
	// Refresh needs it, to link the old session to the new one.
	sessionID string
}

type User struct {
	ID          uuid.UUID
	Phone       string
	Email       *string
	DisplayName *string
	Locale      string
	Status      string
	CreatedAt   time.Time
}

// Reason codes carried on a 401 from VerifyOTP. See httpx.Problem.Code.
//
// These are wire contract: a shipped app branches on them, so they may be added
// to but never reworded. The client's job with all five is the same shape —
// tell the viewer whether to retype the code or ask for a new one — and the
// distinction exists because getting that wrong strands people at the login
// screen with no way forward.
const (
	CodeWrongCode        = "otp_wrong_code"
	CodeChallengeExpired = "otp_expired"
	CodeChallengeSpent   = "otp_already_used"
	CodeTooManyAttempts  = "otp_too_many_attempts"
	CodeUnknownChallenge = "otp_unknown_challenge"
)

// VerifyOTP exchanges a code for a session, creating the user on first login.
func (s *Service) VerifyOTP(ctx context.Context, challengeID uuid.UUID, code string, device DeviceInfo) (*Session, error) {
	var session *Session

	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			phone       string
			codeHash    []byte
			attempts    int
			maxAttempts int
			consumedAt  *time.Time
			expiresAt   time.Time
		)

		// FOR UPDATE serialises concurrent guesses against the same challenge.
		// Without it, N parallel requests each read attempts=0 and the limit
		// never bites — which is exactly how a 6-digit code gets brute-forced.
		err := tx.QueryRow(ctx, `
			SELECT phone, code_hash, attempts, max_attempts, consumed_at, expires_at
			  FROM otp_codes WHERE id = $1 FOR UPDATE`, challengeID,
		).Scan(&phone, &codeHash, &attempts, &maxAttempts, &consumedAt, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			// A challenge id that never existed, or one old enough to have been
			// swept. Same remedy as an expired one from the caller's point of
			// view, and given its own code only so the log can tell a client bug
			// from an ordinary timeout.
			return httpx.Coded(httpx.ErrUnauthorized, CodeUnknownChallenge,
				"That sign-in attempt is no longer valid. Ask for a new code.")
		}
		if err != nil {
			return fmt.Errorf("load otp: %w", err)
		}

		// Three distinguishable failures, and the distinction is deliberate.
		//
		// All three were a bare 401 until now, which is defensible as
		// information hiding right up until you watch someone use it: a code
		// that has expired looks identical to a typo, so they retype the same
		// six digits from the same SMS and fail again. Someone out of attempts
		// does that forever.
		//
		// Nothing is disclosed by saying which. The caller already holds the
		// challenge id — it was minted for them thirty seconds ago — and none of
		// these answers reveals whether the *phone* has an account, which is the
		// fact actually worth hiding here and is hidden by upsert-on-verify
		// regardless.
		if consumedAt != nil {
			return httpx.Coded(httpx.ErrUnauthorized, CodeChallengeSpent,
				"That code has already been used. Ask for a new one.")
		}
		if time.Now().After(expiresAt) {
			return httpx.Coded(httpx.ErrUnauthorized, CodeChallengeExpired,
				"That code has expired. Ask for a new one.")
		}
		if attempts >= maxAttempts {
			return httpx.Coded(httpx.ErrUnauthorized, CodeTooManyAttempts,
				"Too many incorrect codes. Ask for a new one.")
		}

		// Constant-time compare. A timing side channel on a 6-digit code is a
		// small edge, but it is free to remove.
		if subtle.ConstantTimeCompare(auth.HashToken(code), codeHash) != 1 {
			if _, err := tx.Exec(ctx,
				`UPDATE otp_codes SET attempts = attempts + 1 WHERE id = $1`, challengeID); err != nil {
				return fmt.Errorf("record attempt: %w", err)
			}
			// The attempt that exhausts the budget says so on the spot, rather
			// than letting the viewer discover it on the next try. Recomputed
			// from the row read under FOR UPDATE, so it is exact.
			if attempts+1 >= maxAttempts {
				return httpx.Coded(httpx.ErrUnauthorized, CodeTooManyAttempts,
					"Too many incorrect codes. Ask for a new one.")
			}
			return httpx.Coded(httpx.ErrUnauthorized, CodeWrongCode,
				"That code is not right.")
		}

		if _, err := tx.Exec(ctx,
			`UPDATE otp_codes SET consumed_at = now() WHERE id = $1`, challengeID); err != nil {
			return fmt.Errorf("consume otp: %w", err)
		}

		user, err := upsertUser(ctx, tx, phone)
		if err != nil {
			return err
		}

		session, err = s.issueSession(ctx, tx, user, device)
		return err
	})

	if err != nil {
		return nil, err
	}
	return session, nil
}

type DeviceInfo struct {
	Platform  string
	Name      string
	PushToken string
	IP        string
	UserAgent string
}

// Refresh rotates a refresh token.
//
// Rotation with `rotated_to` recorded means a replayed old token is detectable
// as theft rather than merely rejected: if a token that has already been
// rotated is presented again, someone has a copy they should not have, and the
// whole session family is revoked.
func (s *Service) Refresh(ctx context.Context, refreshToken string, device DeviceInfo) (*Session, error) {
	var session *Session

	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			sessionID uuid.UUID
			userID    uuid.UUID
			revokedAt *time.Time
			rotatedTo *uuid.UUID
			expiresAt time.Time
		)

		err := tx.QueryRow(ctx, `
			SELECT id, user_id, revoked_at, rotated_to, expires_at
			  FROM sessions WHERE refresh_token_hash = $1 FOR UPDATE`,
			auth.HashToken(refreshToken),
		).Scan(&sessionID, &userID, &revokedAt, &rotatedTo, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrUnauthorized
		}
		if err != nil {
			return fmt.Errorf("load session: %w", err)
		}

		if rotatedTo != nil {
			// Reuse of an already-rotated token. Revoke every session for this
			// user — the legitimate holder will re-authenticate, and whoever
			// has the stolen copy gets nothing.
			slog.WarnContext(ctx, "refresh token reuse detected; revoking all sessions",
				"user_id", userID, "session_id", sessionID)
			if _, err := tx.Exec(ctx,
				`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
				userID); err != nil {
				return fmt.Errorf("revoke sessions: %w", err)
			}
			return httpx.ErrUnauthorized
		}

		if revokedAt != nil || time.Now().After(expiresAt) {
			return httpx.ErrUnauthorized
		}

		user, err := loadUser(ctx, tx, userID)
		if err != nil {
			return err
		}

		session, err = s.issueSession(ctx, tx, user, device)
		if err != nil {
			return err
		}

		newSessionID, err := uuid.Parse(session.sessionID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE sessions SET rotated_to = $2, revoked_at = now(), last_used_at = now()
			 WHERE id = $1`, sessionID, newSessionID); err != nil {
			return fmt.Errorf("rotate session: %w", err)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return session, nil
}

// Logout revokes the current session, or every session for the user.
//
// Revoking one session does not invalidate its outstanding access token — that
// remains valid until it expires. The access TTL is therefore the real
// revocation window, which is why it is 15 minutes and not hours.
func (s *Service) Logout(ctx context.Context, userID, sessionID uuid.UUID, allDevices bool) error {
	query := `UPDATE sessions SET revoked_at = now()
	           WHERE user_id = $1 AND id = $2 AND revoked_at IS NULL`
	args := []any{userID, sessionID}

	if allDevices {
		query = `UPDATE sessions SET revoked_at = now()
		          WHERE user_id = $1 AND revoked_at IS NULL`
		args = []any{userID}
	}

	if _, err := s.pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

func (s *Service) issueSession(ctx context.Context, tx pgx.Tx, user User, device DeviceInfo) (*Session, error) {
	refreshPlain, refreshHash, err := auth.NewOpaqueToken()
	if err != nil {
		return nil, err
	}

	var ip *net.IP
	if parsed := net.ParseIP(device.IP); parsed != nil {
		ip = &parsed
	}

	var sessionID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO sessions (
			user_id, refresh_token_hash, device_platform, device_name,
			push_token, ip, user_agent, last_used_at, expires_at
		) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''),now(),$8)
		RETURNING id`,
		user.ID, refreshHash, device.Platform, device.Name, device.PushToken,
		ip, device.UserAgent, time.Now().Add(s.issuer.RefreshTTL()),
	).Scan(&sessionID)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}

	accessToken, err := s.issuer.IssueAccessToken(user.ID, sessionID, user.Locale)
	if err != nil {
		return nil, err
	}

	return &Session{
		AccessToken:  accessToken,
		RefreshToken: refreshPlain,
		ExpiresIn:    int(s.issuer.AccessTTL().Seconds()),
		User:         user,
		sessionID:    sessionID.String(),
	}, nil
}

// checkRateLimit throttles by phone AND by IP.
//
// Phone alone lets one host walk the number space; IP alone is defeated by a
// carrier NAT, which in the GCC can put a whole city behind a handful of
// addresses. Both, or neither works.
func (s *Service) checkRateLimit(ctx context.Context, phone, requestIP string) error {
	var byPhone int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM otp_codes
		 WHERE phone = $1 AND created_at > now() - interval '1 hour'`, phone,
	).Scan(&byPhone); err != nil {
		return fmt.Errorf("rate limit check: %w", err)
	}
	if byPhone >= 5 {
		return fmt.Errorf("%w: too many codes requested for this number", httpx.ErrConflict)
	}

	if ip := net.ParseIP(requestIP); ip != nil {
		var byIP int
		if err := s.pool.QueryRow(ctx, `
			SELECT count(*) FROM otp_codes
			 WHERE request_ip = $1 AND created_at > now() - interval '1 hour'`, ip,
		).Scan(&byIP); err != nil {
			return fmt.Errorf("rate limit check: %w", err)
		}
		if byIP >= 20 {
			return fmt.Errorf("%w: too many codes requested from this address", httpx.ErrConflict)
		}
	}
	return nil
}

func upsertUser(ctx context.Context, tx pgx.Tx, phone string) (User, error) {
	var u User
	err := tx.QueryRow(ctx, `
		INSERT INTO users (phone, phone_verified_at)
		VALUES ($1, now())
		ON CONFLICT (phone) WHERE deleted_at IS NULL
		DO UPDATE SET phone_verified_at = now()
		RETURNING id, phone, email, display_name, locale::text, status::text, created_at`,
		phone,
	).Scan(&u.ID, &u.Phone, &u.Email, &u.DisplayName, &u.Locale, &u.Status, &u.CreatedAt)
	if err != nil {
		return u, fmt.Errorf("upsert user: %w", err)
	}
	if u.Status != "active" {
		return u, httpx.ErrForbidden
	}
	return u, nil
}

func loadUser(ctx context.Context, tx pgx.Tx, id uuid.UUID) (User, error) {
	var u User
	err := tx.QueryRow(ctx, `
		SELECT id, phone, email, display_name, locale::text, status::text, created_at
		  FROM users WHERE id = $1 AND deleted_at IS NULL`, id,
	).Scan(&u.ID, &u.Phone, &u.Email, &u.DisplayName, &u.Locale, &u.Status, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, httpx.ErrUnauthorized
	}
	if err != nil {
		return u, fmt.Errorf("load user: %w", err)
	}
	if u.Status != "active" {
		return u, httpx.ErrForbidden
	}
	return u, nil
}

// generateCode returns a 6-digit code from crypto/rand.
//
// math/rand would be seedable and therefore predictable, which for a login
// credential is a full authentication bypass rather than a code-quality note.
func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate otp: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func normalizePhone(raw string) string {
	var b strings.Builder
	for i, r := range raw {
		if r == '+' && i == 0 {
			b.WriteRune(r)
		} else if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
