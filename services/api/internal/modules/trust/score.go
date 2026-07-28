// Package trust turns fraud evidence into a number a query can filter on.
//
// The shape is deliberately the same as the interest profiles in Phase 4:
// append-only signals, folded with exponential decay into a derived score that
// is never incremented in place. The reason is the same too — a stored score
// that is mutated by each detector cannot be recomputed, cannot be explained,
// and cannot be corrected when a detector is found to be wrong. Here the stakes
// are higher: this score decides whether a seller can trade.
package trust

import (
	"math"
	"sort"
	"time"
)

// HalfLife is how fast evidence stops counting.
//
// Thirty days. Long enough that a scammer cannot wait out a signal in a week,
// short enough that a seller who had one bad listing in March is not still
// restricted in December. Fraud behaviour is not permanent and treating it as
// permanent means the only path back is a support ticket.
const HalfLife = 30 * 24 * time.Hour

// Window bounds how far back the fold reads. Past four half-lives a signal
// contributes under 7% of its weight, so reading further is cost without
// meaning.
const Window = 120 * 24 * time.Hour

type Band string

const (
	BandOK         Band = "ok"
	BandWatch      Band = "watch"
	BandRestricted Band = "restricted"
	BandBlocked    Band = "blocked"
)

// Band thresholds. Everyone starts at 100 and loses points.
//
// `watch` is not enforced anywhere — it exists so a reviewer has a queue that
// is not "everything currently blocked", which is the queue that arrives too
// late to be useful.
const (
	WatchBelow      = 80
	RestrictedBelow = 55
	BlockedBelow    = 25
)

// Signal is one piece of evidence, as stored in fraud_signals.
type Signal struct {
	Kind      string
	Severity  int // 1–100, the detector's confidence in this instance
	State     string
	CreatedAt time.Time
}

// weights is how much each kind of evidence is worth, at severity 100 and zero
// age, expressed in points off a perfect score.
//
// This table is the fraud policy. It lives in Go rather than in the database so
// it can be retuned in a deploy rather than a migration, and it is one map
// rather than scattered constants so that the relative weights — which are the
// only thing that matters — can be read in one screen.
// Two invariants the numbers here have to satisfy, and they pull in opposite
// directions:
//
//   - one *confirmed* signal of the top tier must block a seller on its own
//   - no *open* signal of any kind may block a seller on its own
//
// Together they cap every weight below `100 - BlockedBelow` = 75, and leave the
// confirmed multiplier to do the blocking. The consequence is worth stating
// plainly because it is the whole safety property of this file: **a machine
// cannot end an account. Only a human confirming what the machine found can.**
//
// The first draft of this table had stolen_vehicle at 90, which quietly broke
// the second invariant — an unreviewed detector hit would have suspended a
// dealer outright. TestOneUnconfirmedSignalDoesNotBlock is what caught it and
// is the reason it now runs over every key in the map rather than a sample.
var weights = map[string]float64{
	// Criminality. Blocks when confirmed; restricts while merely suspected.
	"stolen_vehicle":       50,
	"vin_mismatch":         45,
	"odometer_rollback":    45,
	"advance_fee_language": 40,
	"chat_advance_fee":     40,
	"identity_mismatch":    42,

	// Strong but circumstantial.
	"vin_check_digit_failed":   35,
	"duplicate_listing":        35,
	"price_outlier_low":        30,
	"mileage_suspiciously_low": 25,
	"rapid_relist":             25,
	"message_burst":            25,
	"image_reuse":              30,

	// Weak. Individually meaningless; the point is that several together are
	// not.
	"chat_off_platform_push":  15,
	"chat_messaging_handle":   10,
	"chat_external_link":      10,
	"chat_phone":              6,
	"chat_email":              6,
	"mileage_implausible":     12,
	"price_raised_while_live": 10,
	"unverified_high_value":   15,
	"buyer_report":            20,
}

// DefaultWeight is what an unrecognised kind is worth.
//
// Non-zero on purpose. A detector shipped without a weights entry is a bug, and
// the safe direction for a signal nobody classified is "counts a little", not
// "counts nothing" — the latter makes the bug invisible.
const DefaultWeight = 20

// stateMultiplier weighs a moderator's judgement against a machine's.
//
// A dismissed signal contributes exactly zero and is still kept: "we looked and
// it was fine" has to survive, or the next detector run re-raises it and the
// same reviewer looks at it again forever.
func stateMultiplier(state string) float64 {
	switch state {
	case "confirmed":
		return 2.0
	case "dismissed":
		return 0
	default: // open
		return 1.0
	}
}

// Score folds evidence into 0–100 and a band.
//
// Additive penalties rather than a product of probabilities. The product is
// more defensible statistically and useless in practice: nobody can explain to
// a suspended seller why 0.94 × 0.88 × 0.91 crossed a line, and every appeal
// becomes a research project. Points off, with a table of what each kind costs,
// is a decision a human can review in a minute.
func Score(signals []Signal, now time.Time) (int, Band) {
	penalty := 0.0
	for _, s := range signals {
		age := now.Sub(s.CreatedAt)
		if age < 0 {
			age = 0
		}
		if age > Window {
			continue
		}

		w, ok := weights[s.Kind]
		if !ok {
			w = DefaultWeight
		}

		severity := float64(s.Severity)
		if severity <= 0 {
			severity = 50
		}
		if severity > 100 {
			severity = 100
		}

		decay := math.Exp2(-age.Hours() / HalfLife.Hours())
		penalty += w * (severity / 100) * decay * stateMultiplier(s.State)
	}

	score := int(math.Round(100 - penalty))
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score, BandFor(score)
}

func BandFor(score int) Band {
	switch {
	case score < BlockedBelow:
		return BandBlocked
	case score < RestrictedBelow:
		return BandRestricted
	case score < WatchBelow:
		return BandWatch
	default:
		return BandOK
	}
}

// Explain returns the per-kind contribution, largest first.
//
// Not diagnostics. This is what a moderator reads before suspending an account
// and what an appeal is answered with, so it has to be the same arithmetic the
// decision used rather than a reconstruction of it.
type Contribution struct {
	Kind    string
	Points  float64
	Count   int
	Latest  time.Time
	Dropped bool // outside the window, counted for nothing
}

func Explain(signals []Signal, now time.Time) []Contribution {
	byKind := map[string]*Contribution{}
	for _, s := range signals {
		c, ok := byKind[s.Kind]
		if !ok {
			c = &Contribution{Kind: s.Kind}
			byKind[s.Kind] = c
		}
		c.Count++
		if s.CreatedAt.After(c.Latest) {
			c.Latest = s.CreatedAt
		}

		age := now.Sub(s.CreatedAt)
		if age < 0 {
			age = 0
		}
		if age > Window {
			c.Dropped = true
			continue
		}
		w, ok := weights[s.Kind]
		if !ok {
			w = DefaultWeight
		}
		severity := float64(s.Severity)
		if severity <= 0 {
			severity = 50
		}
		if severity > 100 {
			severity = 100
		}
		c.Points += w * (severity / 100) *
			math.Exp2(-age.Hours()/HalfLife.Hours()) * stateMultiplier(s.State)
	}

	out := make([]Contribution, 0, len(byKind))
	for _, c := range byKind {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Points != out[j].Points {
			return out[i].Points > out[j].Points
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// Weight exposes the table for tests and for the moderator UI, which shows what
// a signal is worth *before* a reviewer confirms it.
func Weight(kind string) float64 {
	if w, ok := weights[kind]; ok {
		return w
	}
	return DefaultWeight
}
