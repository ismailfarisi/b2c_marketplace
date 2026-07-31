package subject

import (
	"context"

	"github.com/google/uuid"
)

// Readiness is the subject's answer to "should a reel about me be in the feed
// yet".
//
// # The bug this exists for
//
// A seller films a car, answers four questions and taps Publish. The listing
// goes to `pending_review`; the reel goes through probe, transcode and
// moderation and reaches `published` — because nothing in the media pipeline
// ever asked the listing whether it was ready. The reel appears in the feed
// with its CTA correctly greyed out and a reason underneath, which is the
// action seam doing its job, and which is also the wrong outcome: a viewer is
// being shown a car they cannot enquire about, and the seller's one impression
// budget is being spent on it.
//
// The two review queues are genuinely independent — a moderator judges the
// *video*, a permit desk judges the *advert* — and neither should wait on the
// other. What must wait is the publish.
//
// # Why three values rather than a boolean
//
// Because "not yet" and "never" need opposite handling, and getting them
// confused produces the two worst outcomes available. Treating a pending
// listing as a permanent refusal strands a reel that would have been fine in
// ten minutes; treating a rejected one as pending retries it every sweep until
// the row is deleted.
type Readiness int

const (
	// ReadyNow — publish.
	ReadyNow Readiness = iota

	// ReadyLater — the subject is mid-review, or scheduled, or otherwise
	// legitimately not live yet. The reel holds at `approved` and the sweep
	// asks again. Nothing is recorded as a failure, because nothing has failed.
	ReadyLater

	// ReadyNever — rejected, removed, sold, expired. The reel is failed with a
	// reason the seller can read on the Me screen, rather than sitting in a
	// state that looks like progress forever.
	ReadyNever
)

// Gate is optional, in exactly the way Composer is.
//
// A vertical that does not implement it publishes on the media pipeline's
// judgement alone, which is the behaviour every vertical had before this and
// is correct for anything with no separate review of its own — a vendor's meal
// plan is live the moment the branch says so.
//
// Deliberately not folded into Provider. Provider is about *rendering* a
// subject and every vertical needs all of it; this is about a lifecycle only
// some verticals have, and widening Provider would force four implementations
// where two are meaningful.
type Gate interface {
	// Ready reports whether a reel about this subject may publish, and a
	// sentence for the seller when it may not.
	//
	// The reason is shown to the *author*, never to a viewer, so it should say
	// what they must do: "your permit is still being checked" rather than
	// "not_ready".
	Ready(ctx context.Context, subjectID uuid.UUID) (Readiness, string, error)
}

// RegisterGate attaches a publish gate to an already-registered subject type.
//
// Panics on a duplicate or an unknown type, matching Register and
// RegisterComposer: a gate silently not applied is a reel published against an
// unreviewed advert, which is the exact failure this whole file exists to
// prevent, and it must not be discoverable only in production.
func (r *Registry) RegisterGate(t Type, g Gate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[t]; !ok {
		panic("subject: gate for " + string(t) + " registered before its provider")
	}
	if r.gates == nil {
		r.gates = make(map[Type]Gate)
	}
	if _, exists := r.gates[t]; exists {
		panic("subject: duplicate gate for " + string(t))
	}
	r.gates[t] = g
}

// Ready asks the type's gate, if it has one.
//
// A type with no gate, an empty type, or a nil registry all answer [ReadyNow].
// That is the permissive default on purpose: this is called from the publish
// path, and a wiring mistake here should not stop every reel in the product
// from reaching the feed. The failure mode of being too permissive is the bug
// that already existed; the failure mode of being too strict is an outage.
func (r *Registry) Ready(ctx context.Context, t Type, id uuid.UUID) (Readiness, string, error) {
	if r == nil || t == "" || id == uuid.Nil {
		return ReadyNow, "", nil
	}
	r.mu.RLock()
	g, ok := r.gates[t]
	r.mu.RUnlock()
	if !ok {
		return ReadyNow, "", nil
	}
	return g.Ready(ctx, id)
}
