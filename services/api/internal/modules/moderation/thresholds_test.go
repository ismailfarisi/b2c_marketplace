package moderation

import (
	"slices"
	"testing"
	"time"
)

func TestThresholdBandsAreOrdered(t *testing.T) {
	// A category whose reject line sits below its review line would auto-reject
	// content it also wanted a human to see — an ordering mistake that is easy
	// to make while tuning and produces no error.
	for category, th := range Thresholds() {
		if th.Reject < th.Review {
			t.Errorf("%s: reject %.0f is below review %.0f", category, th.Reject, th.Review)
		}
		if th.Review <= 0 || th.Reject > 100 {
			t.Errorf("%s: bands outside 0–100: %+v", category, th)
		}
	}
}

func TestGCCThresholdsAreStricterThanWesternDefaults(t *testing.T) {
	// The regression this guards: someone "cleaning up" the table to the
	// familiar ~80 auto-approve line used in US deployments. Alcohol, gambling
	// and suggestive content carry regulatory and app-store risk here that they
	// do not carry there, and the review lines encode exactly that.
	th := Thresholds()
	for _, category := range []string{"alcohol", "gambling"} {
		if th[category].Review > 35 {
			t.Errorf("%s review threshold is %.0f — too permissive for the GCC",
				category, th[category].Review)
		}
	}
	if th["suggestive"].Review > 40 {
		t.Errorf("suggestive review threshold is %.0f, want ≤40", th["suggestive"].Review)
	}
}

func TestDecideBands(t *testing.T) {
	cases := []struct {
		name  string
		label Label
		want  Verdict
	}{
		{"clean", Label{Category: "Alcohol", Confidence: 10}, VerdictApprove},
		{"alcohol at review line", Label{Category: "Alcohol", Confidence: 35}, VerdictReview},
		{"alcohol below review line", Label{Category: "Alcohol", Confidence: 34.9}, VerdictApprove},
		{"alcohol at reject line", Label{Category: "Alcohol", Confidence: 70}, VerdictReject},
		{"hate symbols are near zero-tolerance", Label{Category: "Hate Symbols", Confidence: 55}, VerdictReject},
		{"tobacco is more forgiving", Label{Category: "Tobacco", Confidence: 55}, VerdictReview},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide([]Label{tc.label}).Verdict; got != tc.want {
				t.Fatalf("Decide(%+v) = %s, want %s", tc.label, got, tc.want)
			}
		})
	}
}

func TestDecideTakesMaxPerCategoryNotSum(t *testing.T) {
	// Eight frames at 30% alcohol is a reel with a bottle in shot, not 240%
	// alcohol. Summing would reject ordinary restaurant footage.
	var labels []Label
	for i := 0; i < 8; i++ {
		labels = append(labels, Label{Category: "Alcohol", Confidence: 30, TimestampMS: i * 1000})
	}
	if got := Decide(labels).Verdict; got != VerdictApprove {
		t.Fatalf("eight low-confidence detections produced %s, want approve", got)
	}
}

func TestUnknownCategoryRoutesToHuman(t *testing.T) {
	// A category nobody has reasoned about must not auto-approve. The failure
	// mode of reviewing it is a few minutes; of approving it, a violation in
	// the feed.
	d := Decide([]Label{{Category: "Cryptocurrency Promotion", Confidence: 60}})
	if d.Verdict != VerdictReview {
		t.Fatalf("unmapped category produced %s, want review", d.Verdict)
	}
	if !slices.Contains(d.Categories, "cryptocurrency_promotion") {
		t.Fatalf("unmapped category was dropped from the reasons: %v", d.Categories)
	}
}

func TestNormaliseCategoryFoldsProviderSpellings(t *testing.T) {
	cases := map[string]string{
		"Explicit Nudity":  "explicit_nudity",
		"explicit_nudity":  "explicit_nudity",
		"Alcohol Use":      "alcohol",
		"alcoholic drinks": "alcohol",
		"Hate Speech":      "hate_symbols",
		"Smoking":          "tobacco",
		"Revealing Attire": "suggestive",
		"Weapon Violence":  "explicit_nudity", // sanity: must NOT match
	}
	delete(cases, "Weapon Violence")

	for raw, want := range cases {
		if got := normaliseCategory(raw); got != want {
			t.Errorf("normaliseCategory(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := normaliseCategory("Weapon Violence"); got != "violence" {
		t.Errorf("normaliseCategory(\"Weapon Violence\") = %q, want violence", got)
	}
}

func TestDecideOrdersCategoriesByConfidence(t *testing.T) {
	d := Decide([]Label{
		{Category: "Tobacco", Confidence: 60},
		{Category: "Alcohol", Confidence: 90},
		{Category: "Gambling", Confidence: 75},
	})
	want := []string{"alcohol", "gambling", "tobacco"}
	if !slices.Equal(d.Categories, want) {
		t.Fatalf("categories = %v, want %v (most confident first)", d.Categories, want)
	}
}

func TestStricterIsAMax(t *testing.T) {
	if Stricter(VerdictApprove, VerdictReview) != VerdictReview {
		t.Error("review must beat approve")
	}
	if Stricter(VerdictReject, VerdictReview) != VerdictReject {
		t.Error("reject must beat review")
	}
	if Stricter(VerdictReject, VerdictApprove) != VerdictReject {
		t.Error("a clean caption must not rescue a rejected frame")
	}
}

// --- trust tiers -------------------------------------------------------------

func TestComputeTier(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-10 * 24 * time.Hour)
	old := now.Add(-100 * 24 * time.Hour)

	cases := []struct {
		name  string
		state TrustState
		want  Tier
	}{
		{"first post", TrustState{PublishedCount: 0}, TierNew},
		{"a few posts", TrustState{PublishedCount: 3}, TierProbation},
		{"established", TrustState{PublishedCount: 40}, TierTrusted},
		{"recent strike outranks volume",
			TrustState{PublishedCount: 400, StrikeCount: 1, LastStrikeAt: &recent}, TierFlagged},
		{"aged-out strike returns to probation, not trusted",
			TrustState{PublishedCount: 400, StrikeCount: 1, LastStrikeAt: &old}, TierProbation},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComputeTier(tc.state, now); got != tc.want {
				t.Fatalf("ComputeTier(%+v) = %s, want %s", tc.state, got, tc.want)
			}
		})
	}
}

func TestRouteNeverLoosensAReject(t *testing.T) {
	// Trust buys a faster path through clean content, never immunity.
	for _, tier := range []Tier{TierNew, TierProbation, TierTrusted, TierFlagged} {
		if got := Route(VerdictReject, tier, false); got != VerdictReject {
			t.Errorf("tier %s downgraded a reject to %s", tier, got)
		}
	}
}

func TestRouteEscalatesUntrustedAuthors(t *testing.T) {
	if got := Route(VerdictApprove, TierNew, false); got != VerdictReview {
		t.Errorf("a first-ever post auto-published: %s", got)
	}
	if got := Route(VerdictApprove, TierFlagged, false); got != VerdictReview {
		t.Errorf("a flagged author auto-published: %s", got)
	}
	if got := Route(VerdictApprove, TierProbation, false); got != VerdictApprove {
		t.Errorf("probation blocked a clean pass: %s", got)
	}
	if got := Route(VerdictApprove, TierTrusted, false); got != VerdictApprove {
		t.Errorf("trusted blocked a clean pass: %s", got)
	}
}

func TestTrustedAuthorsAreSampled(t *testing.T) {
	// Without the sample, `trusted` is unfalsifiable: nothing looks at that
	// author's content again until a customer reports them.
	if got := Route(VerdictApprove, TierTrusted, true); got != VerdictReview {
		t.Fatalf("sampled trusted reel was not reviewed: %s", got)
	}
}

func TestUnknownTierFailsTowardHuman(t *testing.T) {
	if got := Route(VerdictApprove, Tier("something_new"), false); got != VerdictReview {
		t.Fatalf("unknown tier auto-published: %s", got)
	}
}
