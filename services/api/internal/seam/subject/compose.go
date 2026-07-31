package subject

// Selling — the third question this seam answers.
//
// The read half of this package asks a vertical what a reel is *about*. This
// half asks the inverse: what does a seller have to tell you before a reel can
// be about one of these.
//
// It exists for the same reason and it is the harder half to keep honest. A
// filter chip that leaks costs a wrong word on a button. A *composer* that leaks
// costs the whole sell flow: the moment the app contains `if vertical == "car"
// { show mileage field }`, adding property means shipping a mobile release, a
// review cycle and a staged rollout before a single flat can be listed. The
// composer is therefore declared entirely server-side — the fields, their
// labels, their units, their validation bounds, the clock options, and the trust
// upload — and both clients render what they are handed without knowing what any
// of it means.
//
// The test is the same one the rest of the package is held to: adding a vertical
// must be a Go file and a registration line. If it is anything more, this seam
// has failed.

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// MaxFields caps how much a seller is asked for before the video goes up.
//
// Four, and the number is a product constraint rather than a technical one. The
// thing being optimised is the count of sellers who finish, and every field is a
// place to stop: a used-car flow asking for twelve attributes is how classifieds
// sites end up with a listing funnel that converts under a fifth of the people
// who start it.
//
// Enforced at registration rather than trusted, because the pressure to add
// "just one more" field is constant and comes from people who are right about
// the field being useful and wrong about what it costs.
const MaxFields = 4

// Composition is the whole sell flow for one vertical, as the client renders it.
//
// Note what is not here: any notion of screens, steps or order. The client
// decides how to lay four fields out. What the vertical decides is *what it
// needs to know*, and mixing the two would put a vertical in the business of
// designing a phone UI it cannot see.
type Composition struct {
	// Fields are asked in order, and there are at most MaxFields of them.
	Fields []Field

	// Clocks are the ways this subject can be timed — see ClockOption.
	//
	// At least one. A vertical with nothing to offer here has no time dimension
	// and should not be sellable through a flow whose whole premise is that the
	// clock is the listing type.
	Clocks []ClockOption

	// Trust is the optional upload that earns a badge, or nil.
	Trust *TrustUpload

	// Tip is the line over the viewfinder — "Walk around it once. Buyers skip
	// stills."
	//
	// The vertical's own words, because the advice is entirely vertical-specific:
	// what makes a good thirty seconds of a used car and of a plate of food have
	// nothing in common, and a generic "record a video" helps nobody.
	Tip   string
	TipAr string
}

// Validate is called at registration, not at request time.
//
// A malformed composition is a wiring bug and it must fail at boot. Discovering
// it on the first seller who taps the tile means discovering it in production,
// at the exact moment the product is trying to acquire supply.
func (c Composition) Validate() error {
	if len(c.Fields) == 0 {
		return fmt.Errorf("subject: a composition needs at least one field")
	}
	if len(c.Fields) > MaxFields {
		return fmt.Errorf("subject: %d fields exceeds the %d-field cap", len(c.Fields), MaxFields)
	}
	seen := map[string]bool{}
	for _, f := range c.Fields {
		if f.Key == "" {
			return fmt.Errorf("subject: a field needs a key")
		}
		if seen[f.Key] {
			return fmt.Errorf("subject: duplicate field key %q", f.Key)
		}
		seen[f.Key] = true
		if f.Label == "" {
			return fmt.Errorf("subject: field %q needs a label", f.Key)
		}
		if err := f.Kind.valid(); err != nil {
			return fmt.Errorf("subject: field %q: %w", f.Key, err)
		}
		if f.Kind == FieldSelect && len(f.Options) == 0 {
			return fmt.Errorf("subject: select field %q has no options", f.Key)
		}
		if f.Kind == FieldLookup && f.Source == "" {
			return fmt.Errorf("subject: lookup field %q has no source", f.Key)
		}
	}

	if len(c.Clocks) == 0 {
		return fmt.Errorf("subject: a composition needs at least one clock option")
	}
	defaults := 0
	clockKeys := map[string]bool{}
	for _, k := range c.Clocks {
		if k.Key == "" {
			return fmt.Errorf("subject: a clock option needs a key")
		}
		if clockKeys[k.Key] {
			return fmt.Errorf("subject: duplicate clock key %q", k.Key)
		}
		clockKeys[k.Key] = true
		if k.Label == "" {
			return fmt.Errorf("subject: clock %q needs a label", k.Key)
		}
		switch k.Mode {
		case ModeCountdown, ModeWindow, ModeFreshness, ModeCutoff:
		default:
			return fmt.Errorf("subject: clock %q has unknown mode %q", k.Key, k.Mode)
		}
		if k.Default {
			defaults++
		}
	}
	// Exactly one, not at-most-one. Zero means the client picks, and whichever
	// it picks is a listing term nobody chose — which is the same objection
	// vehicles.Draft.Validate raises against defaulting regional spec.
	if defaults != 1 {
		return fmt.Errorf("subject: expected exactly one default clock, got %d", defaults)
	}

	if t := c.Trust; t != nil {
		if t.Key == "" {
			return fmt.Errorf("subject: the trust upload needs a key")
		}
		if t.Label == "" {
			return fmt.Errorf("subject: trust upload %q needs a label", t.Key)
		}
		if ref := t.Reference; ref != nil {
			if ref.Key == "" {
				return fmt.Errorf("subject: the trust reference needs a key")
			}
			// Checked against the field keys, not only against itself: the
			// reference posts back through the same Values map, so a collision
			// would have one silently overwrite the other. That is the kind of
			// bug that shows up as a permit number stored in the price column.
			if seen[ref.Key] {
				return fmt.Errorf("subject: trust reference %q collides with a field key", ref.Key)
			}
			if ref.Label == "" {
				return fmt.Errorf("subject: trust reference %q needs a label", ref.Key)
			}
			if err := ref.Kind.valid(); err != nil {
				return fmt.Errorf("subject: trust reference %q: %w", ref.Key, err)
			}
			// A typeahead against a catalogue makes no sense for the identifier
			// of a document the seller is holding, and a select would mean the
			// platform already knows every permit number ever issued.
			if ref.Kind == FieldSelect || ref.Kind == FieldLookup {
				return fmt.Errorf("subject: trust reference %q cannot be a %s", ref.Key, ref.Kind)
			}
		}
	}
	return nil
}

// FieldKind is how the client draws an input. A closed set on purpose: an
// open-ended "type" string is a switch statement in the app waiting to happen,
// and the first unrecognised value would render as nothing at all.
type FieldKind string

const (
	FieldText    FieldKind = "text"
	FieldInteger FieldKind = "integer"
	// FieldMoney is minor units on the wire and a major-unit keypad on screen.
	// Distinct from FieldInteger because the client has to divide by 100 in one
	// case and not the other, and getting that wrong prices a car at 1,890 AED.
	FieldMoney  FieldKind = "money"
	FieldSelect FieldKind = "select"
	// FieldLookup is a typeahead against a catalogue the vertical serves. The
	// client GETs Source with ?q= and posts back the chosen option's value; it
	// never learns what the rows are.
	FieldLookup FieldKind = "lookup"
)

func (k FieldKind) valid() error {
	switch k {
	case FieldText, FieldInteger, FieldMoney, FieldSelect, FieldLookup:
		return nil
	}
	return fmt.Errorf("unknown field kind %q", k)
}

// Field is one input.
//
// Every string on it is the vertical's. The client renders Label, places Unit
// beside the box, shows Hint under it when the seller stalls, and enforces Min
// and Max locally so a bad value is caught before the round trip — but the
// server re-checks all of it, because a client-side bound is a courtesy and
// never a control.
type Field struct {
	// Key is what the answer is posted back under. An opaque wire value; no
	// client should be rendering it or branching on it.
	Key string

	Label   string
	LabelAr string

	Kind     FieldKind
	Required bool

	// Unit sits beside the input — "km", "AED". Apart from Label so a locale
	// that puts it on the other side still can.
	Unit   string
	UnitAr string

	// Hint is the sub-line. Where a vertical explains a term the seller may not
	// know, which in this market is most of them.
	Hint   string
	HintAr string

	Placeholder string

	// Options populate FieldSelect. Nil otherwise.
	Options []Option

	// Source is the path a FieldLookup queries — "/v1/vehicles/models". Relative
	// to the API root and served by the vertical that declared it.
	Source string

	// Min and Max bound FieldInteger and FieldMoney. Nil means unbounded, which
	// is rarely what a vertical means and usually what it forgot.
	Min *int64
	Max *int64
}

// Option is one choice in a select, or one row a lookup returned.
type Option struct {
	// Value is posted back verbatim.
	Value   string
	Label   string
	LabelAr string
}

// ClockOption is one answer to "how long should it run".
//
// This is the type the mock calls "the clock IS the listing type", and the
// naming is deliberate: a seller is not choosing a decoration, they are choosing
// between an open listing, an auction and a flash window, and those are
// different products with different Modes. Folding them into a duration picker
// would be exactly the merge TimeSignal's Mode comment refuses.
type ClockOption struct {
	Key string

	Mode Mode

	Label   string
	LabelAr string

	// Detail is the consequence, in the seller's terms — "Freshness badge ages:
	// today → 3d → 2w", "Countdown overlay, hold-to-bid". A seller choosing
	// between listing types is choosing between overlays their buyers will see,
	// and this is the only place they find that out before publishing.
	Detail   string
	DetailAr string

	// NeedsWindow asks the seller for two instants rather than accepting a
	// duration. Only a flash window needs it: "ends in 48h" is a duration, but
	// "every night 22:00 to 02:00" is a pair of wall-clock times, and collapsing
	// them would make the second unexpressible.
	NeedsWindow bool

	// Default preselects this option. Exactly one per composition.
	Default bool
}

// TrustUpload is the optional document that earns a badge.
//
// Optional in the strong sense: a seller who skips it publishes normally. The
// listing is worth less — Verified is the most valuable word on the overlay in a
// market where an honest listing and a scam look identical — and that is the
// whole incentive. Requiring it would trade most of the supply for the badge.
type TrustUpload struct {
	Key string

	// Label is the ask, phrased as what it buys rather than what it costs: "Add
	// mulkiya to earn Verified", not "Upload registration card".
	Label   string
	LabelAr string

	// Earns names the badge. The client renders it as the reward, and it must
	// match the badge the vertical's Summarize actually produces — promising
	// Verified and shipping nothing is worse than not asking.
	Earns   string
	EarnsAr string

	// Accept is the MIME allow-list, as the client should constrain its picker.
	Accept []string

	// Reference is an identifier that travels with the document, when the trust
	// artefact has one.
	//
	// # Why this is in the trust slot rather than being a fifth field
	//
	// Property needs a Trakheesi or DARI permit number, and it is not optional:
	// a listing cannot go live without it, so nothing in this vertical can be
	// published without asking. But MaxFields is four and property's four are
	// all load-bearing, so on the face of it the cap and the requirement are in
	// conflict.
	//
	// They are not, because a permit number is not an attribute of the
	// property. It is the identifier *of the document*, in exactly the way a
	// mulkiya has a chassis number on it. Putting it in the trust slot says
	// what it is; putting it in Fields would say a permit is a fact about a
	// flat, which is how you end up with a "permit" column on a table that
	// describes buildings.
	//
	// It also keeps the promise the cap encodes. The seller still answers four
	// questions about the thing they are selling; the trust row is one more
	// interaction, in the place the client already draws one, and the client
	// already knows to render that row differently.
	//
	// Nil for a vertical whose trust artefact is just a document — the mulkiya
	// has no number this product needs.
	Reference *Field
}

// DraftInput is the seller's answers, as posted.
//
// Values arrives as strings keyed by Field.Key, and that is deliberate. A typed
// union here would have to enumerate every kind this seam might ever grow, and
// the vertical is going to parse and range-check the value anyway — vehicles
// already owns a Draft.Validate that produces field-level issues. Two layers of
// parsing means two places for the bounds to disagree.
type DraftInput struct {
	SellerID uuid.UUID

	// Values is field key → the raw string the seller typed or chose. Money
	// arrives in minor units, matching internal/money.
	Values map[string]string

	// Clock is the chosen ClockOption.Key.
	Clock string
	// WindowStart and WindowEnd are set only for a clock with NeedsWindow.
	WindowStart time.Time
	WindowEnd   time.Time

	// Where the thing is. Not a Field, because every vertical needs it and a
	// map key would let one of them forget — and a subject with no point is
	// invisible to every geo query, which reads as a search bug rather than the
	// data bug it is.
	Lat, Lng float64
	// Region and Place are the human names the seller picked — "Dubai" and "Al
	// Quoz 3". Carried alongside the point rather than reverse-geocoded from
	// it, because the seller's own word for where they are is the one buyers
	// search with, and a geocoder returns the administrative name instead.
	//
	// Two fields rather than one joined string, because the separator is a
	// presentation decision and the client is the only thing that knows which
	// direction the text runs.
	Region string
	Place  string

	Locale  string
	Country string
}

// Drafted is what a vertical hands back: the row it created, and the CTA the
// reel about it should carry.
type Drafted struct {
	SubjectID uuid.UUID

	// CTA names the action seam pair to attach to the reel.
	//
	// Returned by the vertical rather than chosen by the sell handler, because
	// only the vertical knows which of its actions is the primary one — and
	// hard-coding "inquire_listing" in the handler would be the same leak in a
	// different file.
	//
	// Plain strings rather than action.Kind and action.TargetType: this package
	// deliberately does not import the action seam. They answer different
	// questions about the same reel and coupling them would make every field
	// added to one visible to the other. The api layer converts.
	CTAKind       string
	CTATargetType string
}

// Composer is what a vertical implements to be sellable from the app.
//
// A separate, optional interface from Provider rather than three more methods on
// it. The two are genuinely independent capabilities: a vertical can be visible
// in the feed without being sellable by a consumer — a restaurant's midnight
// offer is authored by the restaurant through a vendor console, not by someone
// pointing a phone at it — and forcing every Provider to implement a sell flow
// would produce stub composers that exist to satisfy a compiler.
//
// The three methods run at three different moments:
//
//   - Compose is static. Called per request but derived from constants; it
//     describes the vertical, not this seller.
//
//   - Draft runs when the seller finishes the form, *before* the video finishes
//     uploading. It creates the subject row in whatever pre-live state the
//     vertical calls a draft, and returns the CTA. It must not publish.
//
//   - Publish runs when the seller taps Publish. It moves the subject to its
//     live-or-pending state. Separate from Draft because the reel's bytes are
//     still arriving in between, and a subject that went live while its video
//     was still transcoding would be a listing with an unplayable reel.
//
// Publish takes the seller as well as the subject. The caller has already
// checked that this user authored the reel, and passing the id anyway lets the
// vertical re-check ownership of the *subject* — which is a different fact, and
// the one that decides whether a listing may go to review.
type Composer interface {
	Compose() Composition
	Draft(ctx context.Context, in DraftInput) (Drafted, error)
	Publish(ctx context.Context, sellerID, subjectID uuid.UUID) error
}

// RegisterComposer attaches a sell flow to an already-registered subject type.
//
// Separate call from Register so that a vertical that is visible but not
// consumer-sellable simply does not make it, and so the composition is validated
// at boot. Panics for the same reason Register does: two modules claiming one
// sell flow, or a malformed one, is a wiring bug that must not resolve by map
// iteration order in production.
func (r *Registry) RegisterComposer(t Type, c Composer) {
	if err := c.Compose().Validate(); err != nil {
		panic(fmt.Sprintf("subject: composer for %s is malformed: %v", t, err))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[t]; !ok {
		// A sell flow with no Provider would produce listings that publish into a
		// feed with no badge, no price and no countdown — a reel of a car that
		// reads as a reel of nothing. Caught here rather than discovered by the
		// first seller.
		panic(fmt.Sprintf("subject: composer for %s registered before its provider", t))
	}
	if _, ok := r.composers[t]; ok {
		panic(fmt.Sprintf("subject: duplicate composer for %s", t))
	}
	r.composers[t] = c
}

// Composer returns the sell flow for a type, if it has one.
func (r *Registry) Composer(t Type) (Composer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.composers[t]
	return c, ok
}

// Sellable lists the verticals a seller can open the camera on.
//
// The Sell tab's tile grid is built from exactly this, the same way the filter
// row is built from Facets. A vertical with a Provider but no Composer is
// filtered out and does not get a tile — a tile that opens a form nobody
// implemented is worse than an absent one, because the seller has already
// filmed by the time they find out.
func (r *Registry) Sellable() []Facet {
	r.mu.RLock()
	out := make([]Facet, 0, len(r.composers))
	for t := range r.composers {
		p, ok := r.providers[t]
		if !ok {
			continue
		}
		f := p.Facet()
		f.Key = t
		out = append(out, f)
	}
	r.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Clock finds a named option within a composition.
func (c Composition) Clock(key string) (ClockOption, bool) {
	for _, k := range c.Clocks {
		if k.Key == key {
			return k, true
		}
	}
	return ClockOption{}, false
}

// DefaultClock is the preselected option. Validate guarantees there is one.
func (c Composition) DefaultClock() ClockOption {
	for _, k := range c.Clocks {
		if k.Default {
			return k
		}
	}
	return ClockOption{}
}
