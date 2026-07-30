package auth

import (
	"net/http"
	"strings"

	"github.com/marketplace/api/internal/httpx"
)

// Authenticated rejects requests without a valid access token.
func Authenticated(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := parse(issuer, r)
			if !ok {
				httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
				return
			}
			userID, err := claims.UserID()
			if err != nil {
				httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
				return
			}
			ctx := httpx.WithUserID(r.Context(), userID)
			// Best-effort, and deliberately not fatal. A token whose `sid` does
			// not parse is still a validly signed token for a real user, and
			// refusing it would sign everyone out the moment the claim's shape
			// changed. Only logout reads this, and it has its own answer for
			// the claim being absent.
			if sessionID, err := claims.SessionUUID(); err == nil {
				ctx = httpx.WithSessionID(ctx, sessionID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Optional attaches the user when a valid token is present and otherwise
// proceeds anonymously.
//
// The feed needs this: it must serve an unauthenticated browser and a signed-in
// user from the same endpoint, personalising only when it can. Requiring auth
// would put a login wall in front of the product's front door.
func Optional(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if claims, ok := parse(issuer, r); ok {
				if userID, err := claims.UserID(); err == nil {
					r = r.WithContext(httpx.WithUserID(r.Context(), userID))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func parse(issuer *Issuer, r *http.Request) (*Claims, bool) {
	header := r.Header.Get("Authorization")
	scheme, raw, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || raw == "" {
		return nil, false
	}
	claims, err := issuer.ParseAccessToken(raw)
	if err != nil {
		return nil, false
	}
	return claims, true
}
