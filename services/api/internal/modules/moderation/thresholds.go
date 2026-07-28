// Package moderation is the gate between "a reel exists" and "a reel is
// visible".
//
// Two things in here are market-specific and get quietly wrong by default:
// the confidence thresholds (§3.2) and the fact that Arabic text moderation
// needs a language model rather than a keyword classifier (§3.3). Both are
// documented at the point of decision rather than in a wiki nobody reads.
package moderation

import (
	"sort"
	"strings"
)

// Verdict is the outcome of any single layer, and of the combination of all of
// them.
type Verdict string

const (
	VerdictApprove Verdict = "approve"
	VerdictReview  Verdict = "review"
	VerdictReject  Verdict = "reject"
)

// severity orders verdicts so combining layers is a max, not an if-chain. The
// strictest layer wins — a clean caption never rescues a rejected frame.
func (v Verdict) severity() int {
	switch v {
	case VerdictReject:
		return 2
	case VerdictReview:
		return 1
	default:
		return 0
	}
}

// Stricter returns whichever of the two verdicts is more restrictive.
func Stricter(a, b Verdict) Verdict {
	if b.severity() > a.severity() {
		return b
	}
	return a
}

// Label is one detection from any provider, normalised. Rekognition returns
// confidence 0–100; a provider that reports 0–1 is converted at its adapter,
// not here, so this package has exactly one scale to reason about.
type Label struct {
	Category   string
	Confidence float64
	// Frame offset for video labels; zero for poster and text.
	TimestampMS int
}

// Threshold is the band structure for one category.
//
// Reject ≥ Review by construction. Anything below Review auto-approves — there
// is no fourth band, because a category with no auto-approve floor sends every
// reel to a human and the queue stops being worked within a week.
type Threshold struct {
	Reject float64
	Review float64
}

// Thresholds are the GCC values from §3.2.
//
// The reason these are not the vendor defaults: a typical US configuration
// auto-approves anything below ~80 across most categories. In the UAE and KSA,
// alcohol, gambling and suggestive content carry regulatory and app-store risk
// that they do not carry in the US, so the *review* thresholds for those
// categories sit roughly half as high as a Western deployment would set them.
// Getting this wrong does not produce a subtle quality regression; it produces
// a store removal.
//
// Treat these as a starting point to tune against the review queue's overturn
// rate, not as settled values. If reviewers approve >90% of what reaches them,
// the review thresholds are too tight and moderator hours are being burned; if
// <60%, the reject threshold is too loose and content that should have been
// blocked is reaching humans instead.
func Thresholds() map[string]Threshold {
	return map[string]Threshold{
		"explicit_nudity": {Reject: 60, Review: 25},
		"suggestive":      {Reject: 80, Review: 40},
		"alcohol":         {Reject: 70, Review: 35},
		"gambling":        {Reject: 70, Review: 35},
		"tobacco":         {Reject: 85, Review: 50},
		"violence":        {Reject: 70, Review: 35},
		"hate_symbols":    {Reject: 50, Review: 20},
	}
}

// defaultThreshold applies to any category a provider invents that we have not
// mapped.
//
// It routes to a human rather than auto-approving. An unmapped category is by
// definition one nobody has reasoned about, and the failure mode of
// auto-approving it is a policy violation in the feed; the failure mode of
// reviewing it is a few minutes of someone's time.
var defaultThreshold = Threshold{Reject: 90, Review: 50}

// Decision is the combined outcome plus the reasons behind it.
type Decision struct {
	Verdict Verdict
	// Categories that crossed at least the review line, most confident first.
	// This is what the author is shown, so its order is the order of blame.
	Categories []string
	Detail     string
}

// Decide applies the threshold table to a set of labels.
//
// Note what it does *not* do: it does not average, sum or otherwise combine
// confidences within a category. Rekognition's confidence is already a
// per-detection probability, and a reel with eight frames at 30% alcohol is not
// 240% alcohol — it is a reel with an unremarkable bottle in shot. Taking the
// max per category is the only aggregation that keeps the number meaning what
// the provider says it means.
func Decide(labels []Label) Decision {
	table := Thresholds()

	peak := make(map[string]float64, len(labels))
	for _, l := range labels {
		c := normaliseCategory(l.Category)
		if c == "" {
			continue
		}
		if l.Confidence > peak[c] {
			peak[c] = l.Confidence
		}
	}

	verdict := VerdictApprove
	type hit struct {
		category   string
		confidence float64
	}
	var hits []hit

	for category, confidence := range peak {
		t, known := table[category]
		if !known {
			t = defaultThreshold
		}

		switch {
		case confidence >= t.Reject:
			verdict = Stricter(verdict, VerdictReject)
			hits = append(hits, hit{category, confidence})
		case confidence >= t.Review:
			verdict = Stricter(verdict, VerdictReview)
			hits = append(hits, hit{category, confidence})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].confidence != hits[j].confidence {
			return hits[i].confidence > hits[j].confidence
		}
		// Deterministic tie-break. Without it the categories list reorders
		// between identical runs and every diff of a moderation record is noise.
		return hits[i].category < hits[j].category
	})

	d := Decision{Verdict: verdict}
	for _, h := range hits {
		d.Categories = append(d.Categories, h.category)
	}
	return d
}

// normaliseCategory folds provider spellings onto our vocabulary.
//
// Rekognition returns "Explicit Nudity", "Alcohol Use", and hierarchical
// parents like "Rude Gestures"; Claude returns whatever the schema's enum says.
// Doing this once here means the threshold table has one spelling of each
// concept, and a provider swap is an adapter change rather than a table
// rewrite.
func normaliseCategory(raw string) string {
	c := strings.ToLower(strings.TrimSpace(raw))
	c = strings.ReplaceAll(c, "-", " ")
	c = strings.ReplaceAll(c, "_", " ")
	c = strings.Join(strings.Fields(c), " ")
	if c == "" {
		return ""
	}

	switch {
	case strings.Contains(c, "nudity"), strings.Contains(c, "explicit sexual"),
		strings.Contains(c, "graphic sexual"):
		return "explicit_nudity"
	case strings.Contains(c, "suggestive"), strings.Contains(c, "revealing"),
		strings.Contains(c, "swimwear"), strings.Contains(c, "underwear"):
		return "suggestive"
	case strings.Contains(c, "alcohol"), strings.Contains(c, "drinking"),
		strings.Contains(c, "alcoholic"):
		return "alcohol"
	case strings.Contains(c, "gambling"), strings.Contains(c, "betting"),
		strings.Contains(c, "casino"):
		return "gambling"
	case strings.Contains(c, "tobacco"), strings.Contains(c, "smoking"),
		strings.Contains(c, "vaping"):
		return "tobacco"
	case strings.Contains(c, "violence"), strings.Contains(c, "weapon"),
		strings.Contains(c, "gore"), strings.Contains(c, "blood"):
		return "violence"
	case strings.Contains(c, "hate"), strings.Contains(c, "extremis"):
		return "hate_symbols"
	}

	// Unrecognised categories keep their normalised name and fall through to
	// defaultThreshold. Silently dropping them would make a provider adding a
	// category equivalent to disabling it.
	return strings.ReplaceAll(c, " ", "_")
}
