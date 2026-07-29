package auth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
)

// Impersonation — PLAN-ADMIN-CONSOLE §3.7, "view as, never act as".
//
// Ops will ask for this, because *"the vendor says the button is missing"* is
// otherwise unanswerable. It is also the single most dangerous feature in an
// admin console, so the read-only half has to be a **property of the system**
// rather than a rule people follow.
//
// The guarantee is made in one place: `ReadOnlyWhenImpersonating` sits on the
// whole authenticated router and rejects every unsafe HTTP method when the token
// carries the view claim. Not per-handler, because a per-handler check protects
// exactly the handlers somebody remembered — and the failure mode is a new
// endpoint added next quarter that nobody thought about, which is a mutation
// performed as a customer with no name attached.
//
// A view token also cannot be refreshed and cannot be exchanged. It expires and
// the agent asks again, which is what makes the audit trail a list of decisions
// rather than one decision and a long silence.

// ViewTTL is how long a view-as session lasts.
//
// Fifteen minutes: long enough to reproduce what a customer is describing while
// they are still on the phone, short enough that an agent who wanders off does
// not leave a live customer session open on an unlocked screen. It is not
// configurable, because the only reason to raise it is convenience and the only
// thing it buys is exposure.
const ViewTTL = 15 * time.Minute

// IssueViewToken mints a read-only token that acts as somebody else.
//
// Both identities are in the claims. The subject is the customer, because every
// query downstream should return exactly what they would see — a token that
// impersonated at the presentation layer and queried as staff would show a
// support agent things the customer cannot, which defeats the entire purpose.
// The actor is carried alongside so that logs, rate limits and anything else
// looking at the request can name the person actually behind it.
func (i *Issuer) IssueViewToken(subjectID, actorID uuid.UUID, locale string) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subjectID.String(),
			Issuer:    i.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ViewTTL)),
			ID:        uuid.NewString(),
		},
		// No SessionID. A view token is deliberately not a session: it cannot be
		// refreshed, it cannot be revoked individually because it does not
		// outlive its own expiry, and it must never appear in the customer's own
		// list of signed-in devices as though they logged in.
		Locale:  locale,
		ViewAs:  true,
		ActorID: actorID.String(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.signingKey)
	if err != nil {
		return "", fmt.Errorf("auth: sign view token: %w", err)
	}
	return signed, nil
}

// ReadOnlyWhenImpersonating is the structural half of the guarantee.
//
// Mounted once, on the whole authenticated surface. Any method that is not
// safe by definition is refused outright when the caller is impersonating —
// which means a handler cannot be written that mutates under a view token, no
// matter what it does, because the request never reaches it.
//
// GET, HEAD and OPTIONS only. Not a list of "safe endpoints", because such a
// list is a thing people add to.
func ReadOnlyWhenImpersonating(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := parse(issuer, r)
			if !ok || !claims.ViewAs {
				next.ServeHTTP(w, r)
				return
			}

			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				httpx.WriteProblem(w, r, fmt.Errorf(
					"%w: this is a view-as session and cannot change anything. If something "+
						"needs doing on the customer's behalf, do it from the console with "+
						"your own name on it", httpx.ErrForbidden))
				return
			}

			// The actor rides along so that anything logging this request names
			// the person actually behind it rather than the customer whose
			// identity it is wearing.
			if actor, err := uuid.Parse(claims.ActorID); err == nil {
				r = r.WithContext(httpx.WithImpersonator(r.Context(), actor))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RejectImpersonation blocks view tokens from a subtree outright.
//
// Used on `/admin`. The permission check would refuse anyway — the token's
// subject is a customer, who has no grant — but relying on that means the
// console's safety depends on the customer never being staff. A support agent
// viewing as a colleague would otherwise reach the console *as them*, and the
// permission model would happily agree.
func RejectImpersonation(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if claims, ok := parse(issuer, r); ok && claims.ViewAs {
				httpx.WriteProblem(w, r, fmt.Errorf(
					"%w: the console cannot be used from a view-as session. Sign in as "+
						"yourself", httpx.ErrForbidden))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
