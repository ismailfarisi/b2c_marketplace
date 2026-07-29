package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
)

// The read-only guarantee is the whole feature, so it gets a test that fails
// loudly if anyone widens it.
//
// Written against the middleware rather than a handler on purpose: the point of
// making it middleware was that no handler can opt out, and a test that went
// through one handler would prove only that one handler is safe.
func TestViewTokenCannotMutate(t *testing.T) {
	issuer := NewIssuer("test-signing-key-not-used-anywhere", time.Minute, time.Hour)
	subject, actor := uuid.New(), uuid.New()

	token, err := issuer.IssueViewToken(subject, actor, "ar")
	if err != nil {
		t.Fatalf("issue view token: %v", err)
	}

	reached := false
	handler := ReadOnlyWhenImpersonating(issuer)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		}))

	// Every method a router can route. Listed exhaustively rather than as
	// "the unsafe ones", so adding a method to the safe list is a visible edit
	// to this test rather than a silent widening.
	for _, tc := range []struct {
		method  string
		allowed bool
	}{
		{http.MethodGet, true},
		{http.MethodHead, true},
		{http.MethodOptions, true},
		{http.MethodPost, false},
		{http.MethodPut, false},
		{http.MethodPatch, false},
		{http.MethodDelete, false},
		{http.MethodConnect, false},
		{http.MethodTrace, false},
	} {
		t.Run(tc.method, func(t *testing.T) {
			reached = false
			req := httptest.NewRequest(tc.method, "/anything", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if tc.allowed && !reached {
				t.Errorf("%s was blocked; safe methods must pass through", tc.method)
			}
			if !tc.allowed {
				if reached {
					t.Errorf("%s reached the handler — a view session must not be able "+
						"to mutate anything, ever", tc.method)
				}
				if rec.Code != http.StatusForbidden {
					t.Errorf("%s returned %d, want 403", tc.method, rec.Code)
				}
			}
		})
	}
}

// A normal token is untouched. The guard must be invisible to everyone who is
// not impersonating — if it were not, the safest way to avoid it would be to
// stop mounting it.
func TestNormalTokenIsUnaffected(t *testing.T) {
	issuer := NewIssuer("test-signing-key-not-used-anywhere", time.Minute, time.Hour)
	token, err := issuer.IssueAccessToken(uuid.New(), uuid.New(), "en")
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	reached := false
	handler := ReadOnlyWhenImpersonating(issuer)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { reached = true }))

	req := httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !reached {
		t.Error("a normal POST was blocked; only view-as sessions are read-only")
	}
}

// The impersonator rides in the context *alongside* the user, never instead of
// it. This is the regression test for a real bug: `ctxKeyImpersonator` was
// declared with no value, so Go repeated the line above and it became the same
// key as `ctxKeyUserID` — meaning every read during a view-as session ran as the
// staff member rather than the customer, which is precisely the disclosure the
// feature exists to prevent.
func TestImpersonatorDoesNotDisplaceTheUser(t *testing.T) {
	user, actor := uuid.New(), uuid.New()

	ctx := httpx.WithUserID(t.Context(), user)
	ctx = httpx.WithImpersonator(ctx, actor)

	got, ok := httpx.UserIDFromContext(ctx)
	if !ok || got != user {
		t.Fatalf("user id became %v, want %v — the impersonator overwrote the subject", got, user)
	}
	imp, ok := httpx.ImpersonatorFromContext(ctx)
	if !ok || imp != actor {
		t.Fatalf("impersonator = %v, %v; want %v", imp, ok, actor)
	}
	if got == imp {
		t.Fatal("the user and the impersonator resolved to the same value")
	}
}

// A view token carries both identities and no session. The absent session is
// deliberate: it is what stops the token being refreshed or appearing in the
// customer's own list of signed-in devices.
func TestViewTokenClaims(t *testing.T) {
	issuer := NewIssuer("test-signing-key-not-used-anywhere", time.Minute, time.Hour)
	subject, actor := uuid.New(), uuid.New()

	token, err := issuer.IssueViewToken(subject, actor, "ar")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := issuer.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !claims.ViewAs {
		t.Error("the view claim is missing; the guard keys on it and would let this through")
	}
	if claims.Subject != subject.String() {
		t.Errorf("subject = %s, want the customer %s", claims.Subject, subject)
	}
	if claims.ActorID != actor.String() {
		t.Errorf("actor = %s, want the staff member %s", claims.ActorID, actor)
	}
	if claims.SessionID != "" {
		t.Error("a view token must carry no session — it must not be refreshable")
	}

	life := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if life != ViewTTL {
		t.Errorf("lifetime = %s, want %s", life, ViewTTL)
	}
}
