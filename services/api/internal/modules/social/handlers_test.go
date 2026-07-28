package social

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/seam/action"
)

func externalAction(target string) action.Action {
	return action.Action{
		ID:         uuid.New(),
		ReelID:     uuid.New(),
		Kind:       action.KindExternal,
		TargetType: action.TargetURL,
		TargetURL:  target,
	}
}

func TestExternalHandlerRejectsNonHTTPSchemes(t *testing.T) {
	// A CTA is a link a vendor supplies and a customer taps inside our app.
	// Letting a non-http scheme through makes the feed a delivery mechanism for
	// whatever a vendor account can be persuaded to post.
	hostile := []string{
		"javascript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"file:///etc/passwd",
		"intent://scan/#Intent;scheme=zxing;end",
		"//evil.example.com/path",
		"",
		"   ",
		"not a url at all",
	}

	for _, raw := range hostile {
		res, err := externalHandler{}.Resolve(context.Background(), externalAction(raw), action.Viewer{})
		if err != nil {
			t.Fatalf("Resolve(%q) errored: %v", raw, err)
		}
		if res.Available {
			t.Errorf("Resolve(%q) rendered as available", raw)
		}
		if res.Reason == "" {
			t.Errorf("Resolve(%q) was unavailable with no reason", raw)
		}
	}
}

func TestExternalHandlerAcceptsHTTPAndHTTPS(t *testing.T) {
	for _, raw := range []string{
		"https://vendor.example.ae/menu",
		"http://vendor.example.ae/menu",
		"https://vendor.example.ae/menu?utm_source=reel",
	} {
		res, err := externalHandler{}.Resolve(context.Background(), externalAction(raw), action.Viewer{})
		if err != nil {
			t.Fatalf("Resolve(%q) errored: %v", raw, err)
		}
		if !res.Available {
			t.Errorf("Resolve(%q) was rejected: %s", raw, res.Reason)
		}
		if res.Route == "" {
			t.Errorf("Resolve(%q) produced no route", raw)
		}
	}
}

func TestFollowIsAvailableToAnonymousViewers(t *testing.T) {
	// The tap prompts a sign-in, which is a far better conversion moment than a
	// login wall on the feed itself — the viewer has already decided they want
	// something.
	res, err := followHandler{}.Resolve(context.Background(), action.Action{
		Kind: action.KindFollow, TargetType: action.TargetVendor, TargetID: uuid.New(),
	}, action.Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Available {
		t.Fatal("follow was unavailable to an anonymous viewer")
	}
	if requires, _ := res.Meta["requires_auth"].(bool); !requires {
		t.Error("an anonymous follow CTA should tell the client it needs auth")
	}
}

func TestShareMarksItsSurfaceOffFeed(t *testing.T) {
	// Attribution treats share_link as off-feed. If this metadata stops
	// flowing, vendor-driven WhatsApp traffic starts counting toward the feed's
	// own conversion number.
	res, err := shareHandler{}.Resolve(context.Background(), action.Action{
		Kind: action.KindShare, TargetType: action.TargetVendorBranch,
		TargetID: uuid.New(), ReelID: uuid.New(),
	}, action.Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Meta["surface"].(string); got != "share_link" {
		t.Fatalf("share surface = %q, want share_link", got)
	}
}

func TestAuthorSuppliedLabelsWin(t *testing.T) {
	a := action.Action{
		Kind: action.KindFollow, TargetType: action.TargetVendor, TargetID: uuid.New(),
		Label: "Follow Al Barsha Laundry", LabelAr: "تابع مغسلة البرشاء",
	}
	res, _ := followHandler{}.Resolve(context.Background(), a, action.Viewer{})
	if res.Label != a.Label || res.LabelAr != a.LabelAr {
		t.Fatalf("labels = %q/%q, want the author's", res.Label, res.LabelAr)
	}
}
