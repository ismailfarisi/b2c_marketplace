// Package auth issues and verifies credentials.
//
// Access tokens are short-lived JWTs verified locally — the feed will make a
// request per scroll page and a database round-trip per request to check a
// session is not affordable. Refresh tokens are opaque random strings stored as
// hashes, checked against the database on every use, and rotated on each
// refresh.
//
// That split is deliberate: the JWT buys speed and accepts a revocation window
// bounded by its TTL; the refresh token buys revocability and accepts the
// round-trip because it happens once every 15 minutes rather than once per
// request.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrTokenInvalid = errors.New("auth: token invalid")
	ErrTokenExpired = errors.New("auth: token expired")
)

type Claims struct {
	jwt.RegisteredClaims
	SessionID string `json:"sid"`
	Locale    string `json:"loc"`
	// ViewAs marks a read-only impersonation token (§3.7). Absent on every
	// normal token, so an old token cannot accidentally be one.
	ViewAs bool `json:"va,omitempty"`
	// ActorID is the staff member behind a view-as token. The Subject stays the
	// customer, so every query returns exactly what they would see.
	ActorID string `json:"act,omitempty"`
}

type Issuer struct {
	signingKey []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

func NewIssuer(signingKey string, accessTTL, refreshTTL time.Duration) *Issuer {
	return &Issuer{
		signingKey: []byte(signingKey),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		issuer:     "marketplace",
	}
}

func (i *Issuer) AccessTTL() time.Duration  { return i.accessTTL }
func (i *Issuer) RefreshTTL() time.Duration { return i.refreshTTL }

func (i *Issuer) IssueAccessToken(userID, sessionID uuid.UUID, locale string) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    i.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.accessTTL)),
			ID:        uuid.NewString(),
		},
		SessionID: sessionID.String(),
		Locale:    locale,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.signingKey)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

// ParseAccessToken verifies signature, expiry and issuer.
//
// The algorithm is pinned with WithValidMethods. Without it, a token whose
// header says alg:none — or alg:RS256 with the HMAC key used as a public key —
// is accepted, which is the single most exploited JWT flaw there is.
func (i *Issuer) ParseAccessToken(raw string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims,
		func(t *jwt.Token) (any, error) { return i.signingKey, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(i.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %s", ErrTokenInvalid, err)
	}
	return claims, nil
}

func (c *Claims) UserID() (uuid.UUID, error) {
	return uuid.Parse(c.Subject)
}

func (c *Claims) SessionUUID() (uuid.UUID, error) {
	return uuid.Parse(c.SessionID)
}

// NewOpaqueToken returns a refresh token and its hash.
//
// The plaintext is returned once and never stored. A database dump must not be
// a set of working credentials — the same reasoning that applies to passwords
// applies to a token that can mint access tokens for a month.
func NewOpaqueToken() (plaintext string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	plaintext = base64.RawURLEncoding.EncodeToString(buf)
	return plaintext, HashToken(plaintext), nil
}

// HashToken is SHA-256, not bcrypt.
//
// Deliberate: these are 256 bits of machine-generated entropy, not
// user-chosen secrets. There is no dictionary to attack, so a slow KDF buys
// nothing and costs a hash on the hot path of every refresh.
func HashToken(plaintext string) []byte {
	sum := sha256.Sum256([]byte(plaintext))
	return sum[:]
}
