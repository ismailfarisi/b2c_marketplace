package subject

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A minimum viable composition, which each test then breaks in one place.
func wellFormed() Composition {
	return Composition{
		Fields: []Field{{Key: "size", Label: "Size", Kind: FieldText, Required: true}},
		Clocks: []ClockOption{{
			Key: "open", Mode: ModeFreshness, Label: "Open listing", Default: true,
		}},
	}
}

func TestAWellFormedCompositionValidates(t *testing.T) {
	if err := wellFormed().Validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestTheFieldCapIsEnforcedRatherThanDocumented(t *testing.T) {
	c := wellFormed()
	for i := 0; i <= MaxFields; i++ {
		c.Fields = append(c.Fields, Field{
			Key: string(rune('a' + i)), Label: "x", Kind: FieldText,
		})
	}
	assertRejects(t, c, "exceeds")
}

func TestAselectWithNoOptionsIsRejected(t *testing.T) {
	// It would render as an empty dropdown: a required field the seller cannot
	// answer, discovered after they have already filmed.
	c := wellFormed()
	c.Fields = []Field{{Key: "spec", Label: "Spec", Kind: FieldSelect, Required: true}}
	assertRejects(t, c, "no options")
}

func TestAlookupWithNoSourceIsRejected(t *testing.T) {
	c := wellFormed()
	c.Fields = []Field{{Key: "car", Label: "Car", Kind: FieldLookup}}
	assertRejects(t, c, "no source")
}

func TestTwoFieldsCannotShareAKey(t *testing.T) {
	// Values is a map. A duplicate key means one answer silently overwrites the
	// other, and the field that loses is decided by JSON decode order.
	c := wellFormed()
	c.Fields = append(c.Fields, Field{Key: "size", Label: "Again", Kind: FieldText})
	assertRejects(t, c, "duplicate field key")
}

func TestACompositionNeedsAClock(t *testing.T) {
	// The premise of the whole flow is that the clock is the listing type. A
	// vertical with no clock has nothing for the overlay to draw.
	c := wellFormed()
	c.Clocks = nil
	assertRejects(t, c, "at least one clock")
}

func TestExactlyOneClockIsTheDefault(t *testing.T) {
	// Zero defaults means the client picks, and whichever it picks is a listing
	// term nobody chose. Two means the two clients may pick differently.
	c := wellFormed()
	c.Clocks[0].Default = false
	assertRejects(t, c, "exactly one default")

	c = wellFormed()
	c.Clocks = append(c.Clocks, ClockOption{
		Key: "auction", Mode: ModeCountdown, Label: "Auction", Default: true,
	})
	assertRejects(t, c, "exactly one default")
}

func TestAClockCannotUseAModeTheClientsCannotDraw(t *testing.T) {
	c := wellFormed()
	c.Clocks[0].Mode = "whenever"
	assertRejects(t, c, "unknown mode")
}

func TestAnUnknownFieldKindIsRejected(t *testing.T) {
	// The client renders a closed set. An unrecognised kind draws nothing at
	// all, so a required field would become invisible and unanswerable.
	c := wellFormed()
	c.Fields[0].Kind = "slider"
	assertRejects(t, c, "unknown field kind")
}

// ─────────────────────────────────────────────────────────────────────────────
// Registration
// ─────────────────────────────────────────────────────────────────────────────

type stubProvider struct{ facet Facet }

func (s stubProvider) Facet() Facet { return s.facet }
func (s stubProvider) Project(context.Context, uuid.UUID) (TimeSignal, bool, error) {
	return TimeSignal{}, false, nil
}
func (s stubProvider) Summarize(context.Context, []uuid.UUID, Viewer) (map[uuid.UUID]Summary, error) {
	return nil, nil
}

type stubComposer struct{ c Composition }

func (s stubComposer) Compose() Composition { return s.c }
func (s stubComposer) Draft(context.Context, DraftInput) (Drafted, error) {
	return Drafted{}, nil
}
func (s stubComposer) Publish(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func TestAComposerWithoutItsProviderIsAWiringBug(t *testing.T) {
	// A sell flow with no provider produces listings that publish into a feed
	// with no badge, no price and no countdown — a reel of a car that reads as
	// a reel of nothing. It must fail at boot, not at the first seller.
	defer func() {
		if recover() == nil {
			t.Fatal("registering a composer with no provider was allowed")
		}
	}()
	NewRegistry().RegisterComposer(TypeListing, stubComposer{c: wellFormed()})
}

func TestAMalformedComposerFailsAtBoot(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a malformed composition was registered")
		}
	}()
	r := NewRegistry()
	r.Register(TypeListing, stubProvider{})
	r.RegisterComposer(TypeListing, stubComposer{c: Composition{}})
}

func TestSellableListsOnlyWhatCanActuallyBeSold(t *testing.T) {
	// A vertical visible in the feed is not necessarily one a consumer can
	// author. A tile that opens a form nobody implemented is worse than an
	// absent one, because the seller has already filmed by the time they find
	// out.
	r := NewRegistry()
	r.Register(TypeListing, stubProvider{facet: Facet{Label: "Cars", Order: 20}})
	r.Register(TypePlan, stubProvider{facet: Facet{Label: "Meals", Order: 10}})
	r.RegisterComposer(TypeListing, stubComposer{c: wellFormed()})

	if got := len(r.Facets()); got != 2 {
		t.Fatalf("filter row shows %d verticals, want 2", got)
	}
	sellable := r.Sellable()
	if len(sellable) != 1 || sellable[0].Key != TypeListing {
		t.Fatalf("sell grid shows %+v, want just the listing", sellable)
	}
}

func TestSellableCarriesTheVerticalsOwnLabel(t *testing.T) {
	// The same rule as the filter row: the tile says "Cars", never
	// "vehicle_listing", and neither client has a mapping from one to the other.
	r := NewRegistry()
	r.Register(TypeListing, stubProvider{facet: Facet{Label: "Cars", Icon: "car"}})
	r.RegisterComposer(TypeListing, stubComposer{c: wellFormed()})

	got := r.Sellable()[0]
	if got.Label != "Cars" || got.Icon != "car" {
		t.Fatalf("got %+v", got)
	}
}

func assertRejects(t *testing.T, c Composition, want string) {
	t.Helper()
	err := c.Validate()
	if err == nil {
		t.Fatalf("expected a rejection mentioning %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not mention %q", err, want)
	}
}
