// Package comments owns reel comments.
//
// This is the first surface in the product where a member of the public writes
// something another member of the public reads, which makes it a moderation
// problem before it is a feature. PLAN-VIDEO-FIRST §6 is explicit that GCC
// standards on nudity, alcohol, gambling and religious or political content are
// stricter than the defaults these services ship with, and that the consequence
// of under-blocking is regulatory action or app-store removal rather than a bad
// review.
//
// A comment costs a viewer two seconds and costs us a moderation call. That
// ratio is the whole reason the rules in this file exist.
package comments

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/moderation"
)

// Length bounds. 600 characters is roughly four sentences — enough for a real
// question about delivery times, short enough that the moderation prompt stays
// cheap and a comment cannot be used as a billboard.
const (
	MinLength = 1
	MaxLength = 600
)

// Rate limits, in two windows.
//
// A burst window and a sustained one, because they catch different things. The
// burst limit stops a script; the daily limit stops a determined human, who is
// the harder and more common problem — someone working through a competitor's
// reels one comment at a time never trips a per-minute limit.
const (
	BurstWindow = 5 * time.Minute
	BurstLimit  = 8

	DailyWindow = 24 * time.Hour
	DailyLimit  = 100
)

// Validate checks a comment body.
//
// Length is counted in runes, not bytes. An Arabic comment is roughly two bytes
// per character in UTF-8, so a byte limit would silently give Arabic-speaking
// users half the allowance — in a product whose primary market writes in Arabic.
func Validate(body string) error {
	trimmed := strings.TrimSpace(body)

	var fields []httpx.FieldError
	switch n := utf8.RuneCountInString(trimmed); {
	case n < MinLength:
		fields = append(fields, httpx.FieldError{
			Field: "body", Code: "required", Message: "a comment cannot be empty",
		})
	case n > MaxLength:
		fields = append(fields, httpx.FieldError{
			Field: "body", Code: "too_long",
			Message: "comments are limited to 600 characters",
		})
	}

	if len(fields) > 0 {
		return &httpx.ValidationError{Fields: fields}
	}
	return nil
}

// Normalise is what gets stored.
func Normalise(body string) string { return strings.TrimSpace(body) }

// CheckRate applies both windows to a viewer's recent comment timestamps.
//
// Pure, so the limits can be tested without a database and without waiting five
// minutes. The caller supplies the timestamps; this decides.
func CheckRate(recent []time.Time, now time.Time) error {
	var burst, daily int
	for _, t := range recent {
		age := now.Sub(t)
		if age < 0 {
			// A clock-skewed row from a client-supplied timestamp. Counted
			// against both windows rather than ignored — the alternative is a
			// rate limit that a client can bypass by sending a future time.
			burst++
			daily++
			continue
		}
		if age <= BurstWindow {
			burst++
		}
		if age <= DailyWindow {
			daily++
		}
	}

	if burst >= BurstLimit || daily >= DailyLimit {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "body", Code: "rate_limited",
			Message: "you are commenting too quickly — try again shortly",
		}}}
	}
	return nil
}

// Comment states, mirroring the CHECK constraint on reel_comments.state.
const (
	StatePending   = "pending"
	StatePublished = "published"
	StateRejected  = "rejected"
	StateRemoved   = "removed"
)

// Decide maps a moderation outcome onto a comment state.
//
// The important row is the last one. A moderation call that times out, errors,
// or is served by a stub yields **pending**, never published. That is the same
// rule the Phase 2 media pipeline applies to reels, and it matters more here:
// comments arrive at a rate that makes "publish while the moderator is down"
// equivalent to "publish everything" within minutes.
//
// The cost of failing this way is stated rather than hidden: a moderation
// outage means comments stop appearing. That is a visible, complainable-about
// degradation. The other direction is invisible until it is a screenshot.
func Decide(res moderation.TextResult, err error) (state string, reason string) {
	if err != nil {
		return StatePending, ""
	}

	switch res.Verdict {
	case moderation.VerdictApprove:
		return StatePublished, ""
	case moderation.VerdictReject:
		// The author is told which policy their comment fell foul of. "Your
		// comment was removed" with no reason is the single most common
		// complaint about every platform that has comments, and it converts a
		// correct decision into a grievance.
		return StateRejected, rejectReason(res)
	default:
		return StatePending, ""
	}
}

func rejectReason(res moderation.TextResult) string {
	if res.Reason != "" {
		return res.Reason
	}
	if len(res.Categories) > 0 {
		return strings.Join(res.Categories, ", ")
	}
	return "community guidelines"
}

// Visible reports whether a state is renderable to anyone but its author.
func Visible(state string, deleted bool) bool {
	return state == StatePublished && !deleted
}
