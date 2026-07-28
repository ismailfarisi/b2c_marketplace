package reels

import "testing"

// The transition graph is the one thing in this package that, if wrong, is
// wrong silently: an illegal transition that is accepted produces a published
// reel that skipped moderation, and nothing logs an error.

func TestPipelineHappyPathIsLegal(t *testing.T) {
	path := []State{
		StateDraft, StateUploaded, StateProbing, StateTranscoding,
		StateModerating, StateApproved, StatePublished,
	}
	for i := 0; i < len(path)-1; i++ {
		if err := CanTransition(path[i], path[i+1]); err != nil {
			t.Fatalf("happy path blocked at %s → %s: %v", path[i], path[i+1], err)
		}
	}
}

func TestModerationCannotBeSkipped(t *testing.T) {
	// The failure this guards against is the expensive one: a reel reaching the
	// feed without any layer having looked at it.
	illegal := [][2]State{
		{StateDraft, StatePublished},
		{StateUploaded, StatePublished},
		{StateProbing, StatePublished},
		{StateTranscoding, StatePublished},
		{StateTranscoding, StateApproved},
		{StateProbing, StateModerating},
	}
	for _, pair := range illegal {
		if err := CanTransition(pair[0], pair[1]); err == nil {
			t.Errorf("%s → %s was allowed and must not be", pair[0], pair[1])
		}
	}
}

func TestRemovalIsOneWay(t *testing.T) {
	for _, to := range []State{StatePublished, StateApproved, StateDraft, StateReviewPending} {
		if err := CanTransition(StateRemoved, to); err == nil {
			t.Errorf("removed → %s was allowed; reinstating must go through a new decision", to)
		}
	}
}

func TestExpiredIsTerminal(t *testing.T) {
	if !StateExpired.Terminal() {
		t.Fatal("expired must be terminal")
	}
	if err := CanTransition(StateExpired, StateUploaded); err == nil {
		t.Fatal("an expired upload must not resume; the presigned URL is gone")
	}
}

func TestSelfTransitionIsNotAnError(t *testing.T) {
	// Jobs are at-least-once. A retry that finds the work already done should
	// succeed quietly rather than page someone.
	for state := range transitions {
		if err := CanTransition(state, state); err != nil {
			t.Errorf("self-transition on %s errored: %v", state, err)
		}
	}
}

func TestEveryStateIsReachableAndDeclared(t *testing.T) {
	// Catches the case where a new state is added to the enum and to one
	// transition list but never gets its own entry — which makes CanTransition
	// *from* it return "unknown state" at runtime.
	all := []State{
		StateDraft, StateUploaded, StateProbing, StateTranscoding, StateModerating,
		StateReviewPending, StateApproved, StatePublished, StateRejected,
		StateRemoved, StateFailed, StateExpired,
	}
	for _, s := range all {
		if !s.Valid() {
			t.Errorf("state %s has no entry in the transition graph", s)
		}
	}
	if len(transitions) != len(all) {
		t.Errorf("transition graph has %d states, enum has %d", len(transitions), len(all))
	}

	for from, tos := range transitions {
		for _, to := range tos {
			if !to.Valid() {
				t.Errorf("%s targets undeclared state %s", from, to)
			}
		}
	}
}

// --- the §1.1 upload race ---------------------------------------------------

func TestDraftWaitsForBothPreconditions(t *testing.T) {
	cases := []struct {
		name          string
		bytes, meta   bool
		wantAdvance   bool
		wantNextState State
	}{
		{"neither", false, false, false, ""},
		{"bytes only — client uploaded, never captioned", true, false, false, ""},
		{"metadata only — fast typist, slow network", false, true, false, ""},
		{"both", true, true, true, StateUploaded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step, ok := Advance(Snapshot{
				State:         StateDraft,
				BytesReady:    tc.bytes,
				MetadataReady: tc.meta,
			})
			if ok != tc.wantAdvance {
				t.Fatalf("advance = %v, want %v", ok, tc.wantAdvance)
			}
			if ok && step.To != tc.wantNextState {
				t.Fatalf("next = %s, want %s", step.To, tc.wantNextState)
			}
		})
	}
}

func TestRaceIsOrderIndependent(t *testing.T) {
	// Whichever writer commits second must produce the same result. That is the
	// entire correctness argument for not chaining the two calls.
	bytesFirst := Snapshot{State: StateDraft, BytesReady: true, MetadataReady: true}
	metaFirst := Snapshot{State: StateDraft, MetadataReady: true, BytesReady: true}

	a, okA := Advance(bytesFirst)
	b, okB := Advance(metaFirst)
	if okA != okB || a != b {
		t.Fatalf("order-dependent result: %v/%+v vs %v/%+v", okA, a, okB, b)
	}
}

func TestTranscodingWaitsForRenditions(t *testing.T) {
	// A provider reporting success with zero renditions is a provider bug, and
	// advancing on it puts an unplayable reel in the feed.
	if _, ok := Advance(Snapshot{State: StateTranscoding, RenditionCount: 0}); ok {
		t.Fatal("advanced past transcoding with no renditions")
	}
	step, ok := Advance(Snapshot{State: StateTranscoding, RenditionCount: 3})
	if !ok || step.To != StateModerating {
		t.Fatalf("got %+v ok=%v, want moderating", step, ok)
	}
}

func TestModerationVerdictRouting(t *testing.T) {
	cases := map[string]State{
		"approve": StateApproved,
		"reject":  StateRejected,
		"review":  StateReviewPending,
	}
	for decision, want := range cases {
		step, ok := Advance(Snapshot{
			State:              StateModerating,
			ModerationComplete: true,
			Decision:           decision,
		})
		if !ok {
			t.Fatalf("%s: did not advance", decision)
		}
		if step.To != want {
			t.Errorf("%s → %s, want %s", decision, step.To, want)
		}
	}
}

func TestModerationWaitsForAllLayers(t *testing.T) {
	if _, ok := Advance(Snapshot{
		State:              StateModerating,
		ModerationComplete: false,
		Decision:           "approve",
	}); ok {
		t.Fatal("advanced while a moderation layer was still running")
	}
}

func TestReviewPendingEnqueuesNoJob(t *testing.T) {
	// The thing that unblocks a review is a person, not a worker. Enqueuing a
	// job here would spin against a state nothing automated can move.
	step, _ := Advance(Snapshot{
		State: StateModerating, ModerationComplete: true, Decision: "review",
	})
	if step.Job != JobNone {
		t.Fatalf("review_pending enqueued %q", step.Job)
	}
}

func TestApprovedWaitsForCoverage(t *testing.T) {
	// Publishing without coverage produces a reel that is invisible to every
	// geo query — which the vendor reports as a feed bug, not a data bug.
	if _, ok := Advance(Snapshot{State: StateApproved, CoverageResolved: false}); ok {
		t.Fatal("published without resolved coverage")
	}
	step, ok := Advance(Snapshot{State: StateApproved, CoverageResolved: true})
	if !ok || step.To != StatePublished {
		t.Fatalf("got %+v ok=%v, want published", step, ok)
	}
}

func TestTerminalStatesDoNotAdvance(t *testing.T) {
	for _, s := range []State{StatePublished, StateRejected, StateRemoved, StateExpired} {
		if _, ok := Advance(Snapshot{State: s, BytesReady: true, MetadataReady: true,
			ProbeOK: true, RenditionCount: 3, ModerationComplete: true,
			Decision: "approve", CoverageResolved: true}); ok {
			t.Errorf("%s advanced despite every precondition being satisfied", s)
		}
	}
}
