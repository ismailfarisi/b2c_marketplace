// Package subject defines the second half of the upstream seam: what a reel is
// *about*, as the feed is allowed to know it.
//
// The action seam (internal/seam/action) answers "what can I do here" without
// the feed learning what a car is. This one answers the two questions the reel
// overlay asks and the action seam deliberately does not:
//
//	when does this stop being true   → TimeSignal
//	what am I looking at             → Summary
//
// The same rule holds and for the same reason. The feed never branches on
// SubjectType. It carries a mode and two instants, and it carries three lines of
// text and a price it did not compose. Adding a vertical means registering a
// Provider. If it means a `switch subjectType` in the feed, the ranker, or
// either client's reel overlay, the seam has leaked — and the cost of that leak
// is higher here than on the action side, because a leak in the overlay means
// adding a vertical requires shipping a mobile release.
//
// # Why the two halves take different paths
//
// A Summary is resolved per page, like a CTA: it depends on the viewer's locale
// and currency, and nothing sorts by it.
//
// A TimeSignal is not. It is viewer-independent, and the feed must *order* by
// it — the "Ending soon" surface is `ORDER BY ends_at` over the candidate set,
// which cannot be answered by resolving ten already-chosen reels through a
// registry. So a TimeSignal is projected into `reel_time_signals` by the vertical
// that owns the subject, and the feed reads it as a plain indexed column.
//
// That makes Provider a *writer* interface for the time half and a *reader*
// interface for the summary half. The asymmetry is deliberate and is the whole
// design; see Provider.
package subject

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Type identifies what a reel points at. The values match `reels.subject_type`
// and the action seam's TargetType where they overlap — a vehicle_listing is
// the same row on both sides of the seam.
type Type string

const (
	TypeOffering Type = "service_offering"
	TypePlan     Type = "meal_plan"
	TypeListing  Type = "vehicle_listing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Time
// ─────────────────────────────────────────────────────────────────────────────

// Mode is how a client renders the passage of time on a reel. Four, because
// four is what the verticals actually need and an open-ended set would become a
// string the client switches on — which is the leak this package exists to
// prevent.
//
// The modes are distinguished by what the viewer is being told, not by which
// fields are populated:
//
//	ModeCountdown  this ends and is then gone      "02:04 left"
//	ModeWindow     this is true between two times  "1h 48m of 3h"
//	ModeFreshness  this has been true for a while  "listed 3d ago"
//	ModeCutoff     act before the next boundary    "order by 16:00"
//
// Countdown and Window carry the same fields and are still two modes: a
// countdown counts down and drains, a window counts elapsed and fills. Merging
// them would push the choice into the client as a heuristic on how far away
// EndsAt is, and that heuristic would be wrong for a three-hour auction.
type Mode string

const (
	ModeCountdown Mode = "countdown"
	ModeWindow    Mode = "window"
	ModeFreshness Mode = "freshness"
	ModeCutoff    Mode = "cutoff"
)

// TimeSignal is the time bar and the pill beside it.
//
// # It carries instants, never rendered text
//
// There is no `label string` field and there will not be one. A server-rendered
// "02:04 left" is wrong the moment it is serialised: the page may be cached, the
// client may background for twenty minutes and come back, and the same bytes are
// served to an English and an Arabic viewer whose digits differ. The server
// states when things happen; the client states how long that is from now. This
// is the same call the codebase already made for money — minor units on the
// wire, formatting at the edge.
type TimeSignal struct {
	Mode Mode

	// StartsAt is when the interval opened. Required for ModeWindow and
	// ModeFreshness; optional for ModeCountdown, where it exists only to give
	// the bar a denominator.
	StartsAt time.Time

	// EndsAt is when it closes. Required for every mode except ModeFreshness,
	// which has no end — a listing does not expire because it got old, it just
	// stops being interesting, which is a ranking concern and not this one.
	//
	// For ModeCutoff this is the *next* occurrence, already resolved to an
	// instant. See Recurring.
	EndsAt time.Time

	// Horizon is the bar's denominator when the interval itself does not supply
	// a useful one.
	//
	// A plate auction opens nine days before it closes. Drawing the bar across
	// those nine days means it does not visibly move during the two minutes
	// anyone is watching, which is worse than no bar. The vertical picks the
	// span over which urgency is legible — say the final hour — and the bar is
	// drawn across that, saturated before it.
	//
	// Required for ModeFreshness, where it is the decay span ("3 days old" is
	// fresh for property and stale for a restaurant offer, and only the vertical
	// knows which).
	Horizon time.Duration

	// Timezone is an IANA name, and only an offset would be wrong.
	//
	// "Order by 16:00" is a rule in the vendor's wall clock, not an instant the
	// vendor happened to mean once. An offset is correct until the first DST
	// boundary in a market that observes one, and this codebase has already been
	// bitten once by the neighbouring assumption — see the note at
	// internal/api/handlers.go on midnight shifting a day east of Greenwich.
	//
	// Required for ModeCutoff. Ignored otherwise.
	Timezone string

	// Recurring marks a boundary that comes back: EndsAt is the next 16:00, not
	// the last one there will ever be.
	//
	// The client needs this to decide what happens when the clock runs out. A
	// countdown that expires removes the reel; a cutoff that passes rolls to
	// tomorrow's cycle and the CTA stays live. Without the flag the client picks
	// one behaviour and it is wrong for half the feed.
	Recurring bool

	// AsOf is the server's clock when this was resolved, and it is not
	// decoration.
	//
	// Device clocks are wrong. A phone four minutes fast renders a live auction
	// as already closed and the user concludes the app is broken; a phone four
	// minutes slow lets someone tap "bid" into a closed lot and eat a 409. The
	// client computes `skew = AsOf - deviceNow` once per page and applies it to
	// every countdown on it. One field, and the entire class of bug goes away.
	AsOf time.Time
}

// Validate rejects a signal that cannot be rendered.
//
// Called by the projection writer, not by the feed. The point is that a vertical
// storing an incoherent signal fails at write time, where the stack trace names
// the vertical — rather than at read time, where the symptom is a bar that does
// not move and the blame lands on the feed.
func (t TimeSignal) Validate() error {
	switch t.Mode {
	case ModeCountdown:
		if t.EndsAt.IsZero() {
			return fmt.Errorf("subject: countdown needs ends_at")
		}
		if t.StartsAt.IsZero() && t.Horizon <= 0 {
			return fmt.Errorf("subject: countdown needs starts_at or horizon for the bar")
		}
	case ModeWindow:
		if t.StartsAt.IsZero() || t.EndsAt.IsZero() {
			return fmt.Errorf("subject: window needs both starts_at and ends_at")
		}
		if !t.EndsAt.After(t.StartsAt) {
			return fmt.Errorf("subject: window ends before it starts")
		}
	case ModeFreshness:
		if t.StartsAt.IsZero() {
			return fmt.Errorf("subject: freshness needs starts_at")
		}
		if t.Horizon <= 0 {
			return fmt.Errorf("subject: freshness needs a horizon to decay against")
		}
	case ModeCutoff:
		if t.EndsAt.IsZero() {
			return fmt.Errorf("subject: cutoff needs the next boundary as an instant")
		}
		if t.Timezone == "" {
			return fmt.Errorf("subject: cutoff needs an IANA timezone, not an offset")
		}
		if _, err := time.LoadLocation(t.Timezone); err != nil {
			return fmt.Errorf("subject: unknown timezone %q", t.Timezone)
		}
	default:
		return fmt.Errorf("subject: unknown mode %q", t.Mode)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Summary
// ─────────────────────────────────────────────────────────────────────────────

// Summary is the badge at the top of the reel and the three lines at the bottom.
//
// Every field is composed by the vertical. The feed does not know that 64,000 is
// a kilometre reading or that AED 480 is weekly, and it must not learn: the
// moment the overlay formats a mileage, "add a vertical" means "change the feed".
type Summary struct {
	// Badge is the pill over the video — "USED CAR · VERIFIED", "MEAL
	// SUBSCRIPTION". It names the *kind* of thing, which is what lets a viewer
	// parse a reel in the third of a second before deciding to scroll past.
	Badge   string
	BadgeAr string

	// Title and Line are the two text rows above the CTA: what it is, then the
	// facts that qualify it. Line is pre-joined by the vertical, separators and
	// all — splitting it into a []string would tempt the client into re-joining
	// with a middot that is wrong in RTL.
	Title   string
	TitleAr string
	Line    string
	LineAr  string

	// PriceMinor and Currency follow internal/money: minor units on the wire,
	// formatted at the edge. Zero PriceMinor with a non-empty Currency is a free
	// item; an empty Currency means this subject has no price to show, which is
	// not the same thing.
	PriceMinor int64
	Currency   string

	// PriceSuffix is the unit the price is per — "/wk", "/mo". Kept apart from
	// the number so the client can render it smaller, and so a locale that puts
	// the unit first still can.
	PriceSuffix   string
	PriceSuffixAr string
}

// ─────────────────────────────────────────────────────────────────────────────
// The seam
// ─────────────────────────────────────────────────────────────────────────────

// Viewer is re-declared rather than imported from the action seam.
//
// They are the same fields today. They are not the same concept: an action
// resolves against eligibility (can this address be delivered to), a summary
// resolves against presentation (which language, which currency). Sharing the
// struct would make every future field added for one half visible to the other,
// and the first person to read Lat on a summary provider would reasonably use it.
type Viewer struct {
	UserID uuid.NullUUID
	Locale string
	// Country selects the currency and the regulated wording — the same plan is
	// priced in AED and SAR and the disclosure differs.
	Country string
}

// Provider is what a vertical implements. One provider owns one Type, and it
// lives in the module that owns the table.
//
// The two methods are called from different places at different times, which is
// the point:
//
//   - Project is called on write — when a listing is published, an auction is
//     scheduled, a plan's cutoff changes — and by the reconcile worker. Its
//     result is stored in `reel_time_signals`, where the ranker can sort by it.
//     A provider that returns ok=false is saying this subject has no time
//     dimension, and the projection row is deleted rather than left stale.
//
//   - Summarize is called on the feed read path, once per page, batched. It must
//     be fast and it must not fail the page: a provider error costs the overlay
//     its text, not the viewer their feed.
type Provider interface {
	Project(ctx context.Context, subjectID uuid.UUID) (sig TimeSignal, ok bool, err error)
	Summarize(ctx context.Context, subjectIDs []uuid.UUID, v Viewer) (map[uuid.UUID]Summary, error)
}

// Registry binds providers to Type. Registration happens once at startup from
// app.BuildRegistries; lookups are concurrent and read-only after that.
type Registry struct {
	mu        sync.RWMutex
	providers map[Type]Provider
}

func NewRegistry() *Registry { return &Registry{providers: make(map[Type]Provider)} }

// Register panics on a duplicate, matching action.Registry. Two modules claiming
// one subject type is a wiring bug that must fail at boot rather than resolve by
// map iteration order in production.
func (r *Registry) Register(t Type, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[t]; exists {
		panic(fmt.Sprintf("subject: duplicate provider for %s", t))
	}
	r.providers[t] = p
}

// Has reports whether a type has a provider. A subject type with no provider is
// legal — a reel about nothing in particular still plays — so the feed asks
// rather than assumes.
func (r *Registry) Has(t Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[t]
	return ok
}

// Project resolves one subject's time signal for storage. Returns ok=false when
// the type has no provider or the provider says this subject is timeless.
func (r *Registry) Project(ctx context.Context, t Type, id uuid.UUID) (TimeSignal, bool, error) {
	r.mu.RLock()
	p, ok := r.providers[t]
	r.mu.RUnlock()
	if !ok {
		return TimeSignal{}, false, nil
	}
	sig, present, err := p.Project(ctx, id)
	if err != nil || !present {
		return TimeSignal{}, false, err
	}
	if err := sig.Validate(); err != nil {
		return TimeSignal{}, false, err
	}
	return sig, true, nil
}

// Ref is one (type, id) pair on a feed page.
type Ref struct {
	Type Type
	ID   uuid.UUID
}

// SummarizePage resolves the overlay text for a whole page, one call per
// provider rather than one per reel.
//
// Batching is a requirement and not an optimisation, for the same reason it is
// on the action side: a page of ten reels resolved one at a time is ten
// sequential round-trips on the hottest read path in the product.
//
// A provider that errors drops out of the result. Its reels render with a
// countdown and no caption text, which is degraded and legible; failing the page
// to protect a caption would trade the product for its subtitle.
func (r *Registry) SummarizePage(ctx context.Context, refs []Ref, v Viewer) (map[Ref]Summary, error) {
	byType := map[Type][]uuid.UUID{}
	for _, ref := range refs {
		byType[ref.Type] = append(byType[ref.Type], ref.ID)
	}

	out := make(map[Ref]Summary, len(refs))
	for t, ids := range byType {
		r.mu.RLock()
		p, ok := r.providers[t]
		r.mu.RUnlock()
		if !ok {
			continue
		}
		got, err := p.Summarize(ctx, ids, v)
		if err != nil {
			continue
		}
		for id, s := range got {
			out[Ref{Type: t, ID: id}] = s
		}
	}
	return out, nil
}
