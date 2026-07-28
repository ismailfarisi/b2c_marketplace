package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/seam/action"
)

type okHandler struct{}

func (okHandler) Resolve(context.Context, action.Action, action.Viewer) (action.Resolved, error) {
	return action.Resolved{Available: true}, nil
}

func registryWithBooking() *action.Registry {
	r := action.NewRegistry()
	r.Register(action.KindBookService, action.TargetOffering, okHandler{})
	r.Register(action.KindExternal, action.TargetURL, okHandler{})
	return r
}

func serviceUnderTest() *Service {
	return &Service{registry: registryWithBooking()}
}

func fieldCodes(t *testing.T, err error) []string {
	t.Helper()
	if err == nil {
		return nil
	}
	var v *httpx.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("got %T, want *httpx.ValidationError", err)
	}
	codes := make([]string, 0, len(v.Fields))
	for _, f := range v.Fields {
		codes = append(codes, f.Code)
	}
	return codes
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestUnregisteredActionKindIsRejectedAtAuthoringTime(t *testing.T) {
	// Without this check, an author attaches a CTA nobody implemented, it
	// stores cleanly, and the failure surfaces weeks later as a greyed-out
	// button in the feed — which everyone reads as a feed bug.
	id := uuid.New()
	err := serviceUnderTest().validate([]Input{{
		Kind:       action.KindSubscribePlan, // no handler registered
		TargetType: action.TargetPlan,
		TargetID:   &id,
	}})

	if !contains(fieldCodes(t, err), "unsupported_action") {
		t.Fatalf("an unhandled action kind was accepted: %v", err)
	}
}

func TestRegisteredActionKindIsAccepted(t *testing.T) {
	id := uuid.New()
	if err := serviceUnderTest().validate([]Input{{
		Kind:       action.KindBookService,
		TargetType: action.TargetOffering,
		TargetID:   &id,
	}}); err != nil {
		t.Fatalf("a registered action was rejected: %v", err)
	}
}

func TestTargetIDIsRequiredExceptForURLs(t *testing.T) {
	err := serviceUnderTest().validate([]Input{{
		Kind:       action.KindBookService,
		TargetType: action.TargetOffering,
		// no TargetID
	}})
	if !contains(fieldCodes(t, err), "required") {
		t.Fatalf("a CTA with no target was accepted: %v", err)
	}
}

func TestURLTargetRequiresAURLNotAnID(t *testing.T) {
	err := serviceUnderTest().validate([]Input{{
		Kind:       action.KindExternal,
		TargetType: action.TargetURL,
		// no TargetURL
	}})
	if !contains(fieldCodes(t, err), "required") {
		t.Fatalf("a url CTA with no url was accepted: %v", err)
	}

	if err := serviceUnderTest().validate([]Input{{
		Kind:       action.KindExternal,
		TargetType: action.TargetURL,
		TargetURL:  "https://vendor.example.ae",
	}}); err != nil {
		t.Fatalf("a valid url CTA was rejected: %v", err)
	}
}

func TestValidationReportsEveryProblemAtOnce(t *testing.T) {
	err := serviceUnderTest().validate([]Input{
		{Kind: action.KindBookService, TargetType: action.TargetOffering}, // no id
		{Kind: action.KindFollow, TargetType: action.TargetVendor},        // no handler, no id
	})
	codes := fieldCodes(t, err)
	if !contains(codes, "required") || !contains(codes, "unsupported_action") {
		t.Fatalf("expected both problems, got %v", codes)
	}
}

func TestRegistryHasReportsRegistration(t *testing.T) {
	r := registryWithBooking()
	if !r.Has(action.KindBookService, action.TargetOffering) {
		t.Error("registered pair reported as absent")
	}
	if r.Has(action.KindBookService, action.TargetListing) {
		t.Error("unregistered pair reported as present")
	}
	if r.Has(action.KindFollow, action.TargetOffering) {
		t.Error("wrong kind reported as present")
	}
}

func TestMaxActionsIsThree(t *testing.T) {
	// A product limit, not a storage one: a reel with six buttons has no
	// primary call to action, and attribution credits one action — so spreading
	// intent across six makes every one of them look weak.
	if MaxActions != 3 {
		t.Fatalf("MaxActions = %d, want 3", MaxActions)
	}
}
