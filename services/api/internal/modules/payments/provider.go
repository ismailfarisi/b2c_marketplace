// Package payments owns gateway integration and the money state machine.
package payments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// Provider is the gateway abstraction.
//
// Kept from day one for the same reason as the transcode provider: the regional
// PSP landscape (Checkout.com, Tap, Network, Telr, Stripe) is not settled, and
// switching should be a config change rather than a rewrite of the order flow.
type Provider interface {
	Name() string
	// Authorize places a hold. It does not move money.
	Authorize(ctx context.Context, in AuthorizeInput) (AuthorizeResult, error)
	// Capture settles up to the authorized amount.
	Capture(ctx context.Context, providerRef string, amountMinor int64) (CaptureResult, error)
	Refund(ctx context.Context, providerRef string, amountMinor int64) (RefundResult, error)
}

type AuthorizeInput struct {
	OrderID     uuid.UUID
	UserID      uuid.UUID
	AmountMinor int64
	Currency    string
	// IdempotencyKey is forwarded to the gateway. Our own idempotency table
	// protects our state; this protects theirs, and both are needed — a retry
	// that gets past our layer must not create a second hold on the card.
	IdempotencyKey string
}

type AuthorizeResult struct {
	ProviderRef  string
	Status       string // requires_action | authorized | failed
	ClientSecret string
	RedirectURL  string
	FailureCode  string
}

type CaptureResult struct {
	ProviderRef string
	Status      string
	FailureCode string
}

type RefundResult struct {
	ProviderRef string
	Status      string
}

// SandboxProvider approves everything.
//
// This exists so the Phase 1 slice runs end to end without a merchant account,
// which is otherwise a weeks-long procurement dependency on proving the engine
// works. It is wired only when APP_ENV is not production — config.Load rejects
// the combination, so it cannot reach a deployed environment by mistake.
type SandboxProvider struct{}

func (SandboxProvider) Name() string { return "sandbox" }

func (SandboxProvider) Authorize(_ context.Context, in AuthorizeInput) (AuthorizeResult, error) {
	return AuthorizeResult{
		ProviderRef:  "auth_" + randomRef(),
		Status:       "authorized",
		ClientSecret: "sandbox_secret_" + in.OrderID.String()[:8],
	}, nil
}

func (SandboxProvider) Capture(_ context.Context, providerRef string, _ int64) (CaptureResult, error) {
	return CaptureResult{ProviderRef: "cap_" + randomRef(), Status: "succeeded"}, nil
}

func (SandboxProvider) Refund(_ context.Context, providerRef string, _ int64) (RefundResult, error) {
	return RefundResult{ProviderRef: "ref_" + randomRef(), Status: "succeeded"}, nil
}

func randomRef() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("fallback_%d", len(buf))
	}
	return hex.EncodeToString(buf)
}
