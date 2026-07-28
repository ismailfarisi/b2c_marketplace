package comments

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/moderation"
)

func codeOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var v *httpx.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("got %T, want *httpx.ValidationError", err)
	}
	if len(v.Fields) == 0 {
		t.Fatal("validation error with no fields")
	}
	return v.Fields[0].Code
}

func TestModerationFailureHoldsRatherThanPublishes(t *testing.T) {
	// The rule the whole package turns on. Comments arrive at a rate that makes
	// "publish while the classifier is down" equivalent to "publish everything"
	// within minutes — and unlike a bad reel, a bad comment is already under
	// someone's storefront.
	//
	// The cost is stated rather than hidden: a moderation outage means comments
	// stop appearing. That is visible and complainable-about. The other
	// direction is invisible until it is a screenshot.
	state, _ := Decide(moderation.TextResult{}, errors.New("anthropic: 503"))
	if state != StatePending {
		t.Fatalf("a moderation failure produced state %q, want pending", state)
	}
}

func TestReviewVerdictHoldsToo(t *testing.T) {
	state, _ := Decide(moderation.TextResult{Verdict: moderation.VerdictReview}, nil)
	if state != StatePending {
		t.Fatalf("a review verdict produced state %q, want pending", state)
	}
}

func TestApprovedCommentsPublish(t *testing.T) {
	state, reason := Decide(moderation.TextResult{Verdict: moderation.VerdictApprove}, nil)
	if state != StatePublished {
		t.Fatalf("an approved comment produced state %q", state)
	}
	if reason != "" {
		t.Errorf("an approved comment carried a rejection reason: %q", reason)
	}
}

func TestRejectedCommentsCarryAReason(t *testing.T) {
	// "Your comment was removed" with no reason is the single most common
	// complaint about every platform that has comments, and it turns a correct
	// decision into a grievance.
	state, reason := Decide(moderation.TextResult{
		Verdict:    moderation.VerdictReject,
		Categories: []string{"harassment"},
	}, nil)

	if state != StateRejected {
		t.Fatalf("state = %q, want rejected", state)
	}
	if reason == "" {
		t.Fatal("a rejected comment carried no reason")
	}
}

func TestRejectionReasonFallsBackToSomethingSayable(t *testing.T) {
	_, reason := Decide(moderation.TextResult{Verdict: moderation.VerdictReject}, nil)
	if reason == "" {
		t.Fatal("a rejection with no categories and no detail produced an empty reason")
	}
}

func TestLengthIsCountedInRunesNotBytes(t *testing.T) {
	// An Arabic comment is about two bytes per character in UTF-8. A byte limit
	// would silently give Arabic-speaking users half the allowance, in a product
	// whose primary market writes in Arabic.
	arabic := strings.Repeat("ش", MaxLength)
	if err := Validate(arabic); err != nil {
		t.Fatalf("a %d-character Arabic comment was rejected: %v", MaxLength, err)
	}
	if err := Validate(strings.Repeat("ش", MaxLength+1)); err == nil {
		t.Fatal("a comment over the limit was accepted")
	}
}

func TestWhitespaceOnlyCommentsAreRejected(t *testing.T) {
	for _, body := range []string{"", "   ", "\n\t  \n"} {
		if got := codeOf(t, Validate(body)); got != "required" {
			t.Errorf("Validate(%q) code = %q, want required", body, got)
		}
	}
}

func TestBurstLimitStopsAScript(t *testing.T) {
	now := time.Now()
	var recent []time.Time
	for i := 0; i < BurstLimit; i++ {
		recent = append(recent, now.Add(-time.Duration(i)*time.Second))
	}

	if got := codeOf(t, CheckRate(recent, now)); got != "rate_limited" {
		t.Fatalf("%d comments in %d seconds was allowed", BurstLimit, BurstLimit)
	}
}

func TestBurstLimitReleasesAfterItsWindow(t *testing.T) {
	now := time.Now()
	var old []time.Time
	for i := 0; i < BurstLimit; i++ {
		old = append(old, now.Add(-BurstWindow-time.Minute))
	}

	if err := CheckRate(old, now); err != nil {
		t.Fatalf("comments older than the burst window still counted: %v", err)
	}
}

func TestDailyLimitStopsADeterminedHuman(t *testing.T) {
	// The harder and more common problem. Someone working through a
	// competitor's reels one comment at a time never trips a per-minute limit.
	now := time.Now()
	var spread []time.Time
	for i := 0; i < DailyLimit; i++ {
		spread = append(spread, now.Add(-time.Duration(i)*10*time.Minute))
	}

	if got := codeOf(t, CheckRate(spread, now)); got != "rate_limited" {
		t.Fatalf("%d comments spread over a day was allowed", DailyLimit)
	}
}

func TestFutureTimestampsCannotBypassTheLimit(t *testing.T) {
	// Ignoring a negative age would let a client with a skewed or hostile clock
	// comment without limit.
	now := time.Now()
	var future []time.Time
	for i := 0; i < BurstLimit; i++ {
		future = append(future, now.Add(time.Hour))
	}

	if err := CheckRate(future, now); err == nil {
		t.Fatal("future-dated comments bypassed the rate limit")
	}
}

func TestAQuietAccountIsNotLimited(t *testing.T) {
	now := time.Now()
	recent := []time.Time{now.Add(-time.Minute), now.Add(-2 * time.Hour)}
	if err := CheckRate(recent, now); err != nil {
		t.Fatalf("two comments in two hours were rate limited: %v", err)
	}
}

func TestOnlyPublishedUndeletedCommentsAreVisible(t *testing.T) {
	cases := map[string]struct {
		state   string
		deleted bool
		want    bool
	}{
		"published":         {StatePublished, false, true},
		"published+deleted": {StatePublished, true, false},
		"pending":           {StatePending, false, false},
		"rejected":          {StateRejected, false, false},
		"removed":           {StateRemoved, false, false},
	}
	for name, c := range cases {
		if got := Visible(c.state, c.deleted); got != c.want {
			t.Errorf("%s: Visible = %v, want %v", name, got, c.want)
		}
	}
}
