package subject

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeGate struct {
	readiness Readiness
	reason    string
	err       error
	calls     int
}

func (f *fakeGate) Ready(context.Context, uuid.UUID) (Readiness, string, error) {
	f.calls++
	return f.readiness, f.reason, f.err
}

// A Provider has to exist before a gate can be registered, so this is the
// smallest thing that satisfies the registry.
type nullProvider struct{}

func (nullProvider) Facet() Facet { return Facet{Label: "Test", Order: 1} }
func (nullProvider) Project(context.Context, uuid.UUID) (TimeSignal, bool, error) {
	return TimeSignal{}, false, nil
}
func (nullProvider) Summarize(context.Context, []uuid.UUID, Viewer) (map[uuid.UUID]Summary, error) {
	return nil, nil
}

const testType Type = "test_subject"

func registryWithGate(t *testing.T, g Gate) *Registry {
	t.Helper()
	r := NewRegistry()
	r.Register(testType, nullProvider{})
	if g != nil {
		r.RegisterGate(testType, g)
	}
	return r
}

func TestATypeWithNoGatePublishes(t *testing.T) {
	// The default, and the behaviour every vertical had before gates existed.
	// A meal plan is live the moment its branch says so; there is no second
	// desk to wait for, and requiring every vertical to declare that would be
	// four implementations to express "nothing to add".
	r := registryWithGate(t, nil)

	got, _, err := r.Ready(context.Background(), testType, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if got != ReadyNow {
		t.Fatalf("an ungated type answered %v", got)
	}
}

func TestAnUnknownTypeOrEmptySubjectPublishes(t *testing.T) {
	// A reel about nothing in particular is legal. So is a subject type this
	// build has never heard of — and refusing to publish over an unrecognised
	// string would mean a server rollback took the feed down with it.
	r := registryWithGate(t, &fakeGate{readiness: ReadyNever})

	for _, tc := range []struct {
		name string
		typ  Type
		id   uuid.UUID
	}{
		{"unknown type", "no_such_thing", uuid.New()},
		{"empty type", "", uuid.New()},
		{"nil subject", testType, uuid.Nil},
	} {
		got, _, err := r.Ready(context.Background(), tc.typ, tc.id)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != ReadyNow {
			t.Errorf("%s: answered %v, want ReadyNow", tc.name, got)
		}
	}
}

func TestANilRegistryPublishes(t *testing.T) {
	// The pipeline holds a *Registry that a deployment may not have wired. The
	// permissive answer is deliberate: being too strict here stops every reel
	// in the product from reaching the feed, which is a worse failure than the
	// bug gates were added to fix.
	var r *Registry
	got, _, err := r.Ready(context.Background(), testType, uuid.New())
	if err != nil || got != ReadyNow {
		t.Fatalf("nil registry answered %v, %v", got, err)
	}
}

func TestTheGateIsConsultedAndItsAnswerReturned(t *testing.T) {
	for _, want := range []Readiness{ReadyNow, ReadyLater, ReadyNever} {
		g := &fakeGate{readiness: want, reason: "because"}
		r := registryWithGate(t, g)

		got, reason, err := r.Ready(context.Background(), testType, uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("got %v, want %v", got, want)
		}
		if reason != "because" {
			t.Errorf("reason was dropped: %q", reason)
		}
		if g.calls != 1 {
			t.Errorf("gate called %d times", g.calls)
		}
	}
}

func TestAGateErrorPropagatesRatherThanBecomingAVerdict(t *testing.T) {
	// The distinction the whole type exists for. A database blip must not
	// read as "this listing was rejected" — that would permanently fail a reel
	// that is fine — and must not read as "publish it" either, which for
	// property means advertising without a permit because Postgres hiccupped.
	//
	// The caller holds the reel and asks again.
	boom := errors.New("connection reset")
	r := registryWithGate(t, &fakeGate{readiness: ReadyLater, err: boom})

	_, _, err := r.Ready(context.Background(), testType, uuid.New())
	if !errors.Is(err, boom) {
		t.Fatalf("the error was swallowed: %v", err)
	}
}

func TestReadyNowIsTheZeroValue(t *testing.T) {
	// Load-bearing. Anything that constructs a Readiness without setting it —
	// a struct literal, a map miss, a decode — gets the permissive answer, and
	// that is the direction a wiring mistake should fail in. If ReadyNever were
	// zero, one forgotten field would stop the feed.
	var r Readiness
	if r != ReadyNow {
		t.Fatal("the zero Readiness is not ReadyNow")
	}
}

func TestRegisteringAGateTwicePanics(t *testing.T) {
	// A gate silently not applied is the exact bug this file exists to fix, so
	// an ambiguous registration must fail at boot rather than resolve by map
	// iteration order in production. Same rule as Register and
	// RegisterComposer.
	r := registryWithGate(t, &fakeGate{})
	defer func() {
		if recover() == nil {
			t.Fatal("a duplicate gate was accepted")
		}
	}()
	r.RegisterGate(testType, &fakeGate{})
}

func TestAGateForAnUnregisteredTypePanics(t *testing.T) {
	// Catches the ordering mistake — a gate wired before its provider — at
	// boot. Left to run, it would be a gate on a type the feed cannot render
	// anyway, which is a confusing pair of symptoms to debug together.
	r := NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("a gate for an unregistered type was accepted")
		}
	}()
	r.RegisterGate(testType, &fakeGate{})
}
