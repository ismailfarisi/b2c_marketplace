// Package reels owns the lifecycle of a piece of video content: upload,
// processing, moderation, publication and takedown.
//
// The state machine lives here and nowhere else. A CHECK constraint cannot
// express a graph, so the graph is one Go value that every job, handler and
// admin action goes through. The alternative — each job updating `state`
// directly with its own idea of what was legal — is how a reel ends up
// published without having been moderated, and that failure is invisible until
// someone screenshots it.
package reels

import (
	"fmt"
	"slices"
)

type State string

const (
	StateDraft         State = "draft"
	StateUploaded      State = "uploaded"
	StateProbing       State = "probing"
	StateTranscoding   State = "transcoding"
	StateModerating    State = "moderating"
	StateReviewPending State = "review_pending"
	StateApproved      State = "approved"
	StatePublished     State = "published"
	StateRejected      State = "rejected"
	StateRemoved       State = "removed"
	StateFailed        State = "failed"
	StateExpired       State = "expired"
)

// transitions is the whole graph. Read it as "from → the complete set of legal
// nexts".
//
// Two properties are deliberate:
//
//   - `failed` is reachable from every processing state, and returns only to
//     the state it failed in. A retry re-enters the pipeline where it left off
//     rather than restarting from upload, because re-transcoding to recover
//     from a moderation timeout costs real money.
//   - `published → removed` is a one-way door. A takedown keeps the row (audit,
//     regulator, the author's own history) and drops it from the feed. There is
//     no `removed → published`; reinstating means an explicit new decision that
//     goes through review again.
var transitions = map[State][]State{
	StateDraft:         {StateUploaded, StateExpired, StateFailed, StateRemoved},
	StateUploaded:      {StateProbing, StateFailed, StateRemoved},
	StateProbing:       {StateTranscoding, StateRejected, StateFailed, StateRemoved},
	StateTranscoding:   {StateModerating, StateFailed, StateRemoved},
	StateModerating:    {StateApproved, StateReviewPending, StateRejected, StateFailed, StateRemoved},
	StateReviewPending: {StateApproved, StateRejected, StateRemoved},
	StateApproved:      {StatePublished, StateRejected, StateFailed, StateRemoved},
	StatePublished:     {StateRemoved},
	StateRejected:      {StateRemoved},
	StateRemoved:       {},
	StateExpired:       {},

	// From `failed`, back to whichever processing state is being retried. The
	// job that owns the retry decides which; this only says the moves are legal.
	StateFailed: {StateUploaded, StateProbing, StateTranscoding, StateModerating, StateRemoved},
}

// terminal states never transition again. Kept separate from the map so a
// sweeper can ask the question directly instead of inferring it from an empty
// slice.
var terminal = []State{StateRemoved, StateExpired}

func (s State) Valid() bool {
	_, ok := transitions[s]
	return ok
}

func (s State) Terminal() bool { return slices.Contains(terminal, s) }

// Processing reports whether the pipeline still owes this reel work. Used by the
// stuck-job sweeper: anything in a processing state that has not moved in an
// hour is a job that died without recording a failure.
func (s State) Processing() bool {
	switch s {
	case StateUploaded, StateProbing, StateTranscoding, StateModerating:
		return true
	}
	return false
}

// ErrIllegalTransition carries both ends so the log line is actionable without
// having to find the caller.
type ErrIllegalTransition struct {
	From, To State
}

func (e ErrIllegalTransition) Error() string {
	return fmt.Sprintf("reels: illegal transition %s → %s", e.From, e.To)
}

// CanTransition is the single authority. Every write to reels.state goes
// through it.
func CanTransition(from, to State) error {
	allowed, known := transitions[from]
	if !known {
		return fmt.Errorf("reels: unknown state %q", from)
	}
	if !to.Valid() {
		return fmt.Errorf("reels: unknown state %q", to)
	}
	// A no-op transition is not an error — jobs are at-least-once, and a retry
	// that finds the work already done should succeed quietly rather than page
	// someone.
	if from == to {
		return nil
	}
	if !slices.Contains(allowed, to) {
		return ErrIllegalTransition{From: from, To: to}
	}
	return nil
}

// Actor identifies who caused a transition, for reel_state_history.
type Actor struct {
	Type string // system | moderator | author
	ID   *string
}

var (
	ActorSystem = Actor{Type: "system"}
)

func ActorModerator(id string) Actor { return Actor{Type: "moderator", ID: &id} }
func ActorAuthor(id string) Actor    { return Actor{Type: "author", ID: &id} }
