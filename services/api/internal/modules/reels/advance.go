package reels

// The upload race — docs/PHASE-2-MEDIA-AND-FEED.md §1.1.
//
// Two things must happen before a reel can be processed and they are
// independent: the bytes arrive (S3 event) and the metadata arrives
// (POST /reels/{id}). Either can complete first. A fast typist on a slow
// connection finishes the caption before the upload; a client that dies right
// after PUT never sends metadata at all.
//
// So neither writer chains the next step. Both stamp their own column and call
// Advance; whichever commits second finds the preconditions satisfied and does
// the work. This is the only shape that is correct in both orderings *and*
// survives the client disappearing — the S3 event is the trigger of record
// precisely because it fires whether or not the client is still alive.

// JobKind is the pipeline job a step enqueues. The worker maps these to
// handlers; Advance never runs work itself, so it stays a pure function and
// stays testable.
type JobKind string

const (
	JobNone      JobKind = ""
	JobProbe     JobKind = "reel.probe"
	JobTranscode JobKind = "reel.transcode"
	JobModerate  JobKind = "reel.moderate"
	JobPublish   JobKind = "reel.publish"
)

// Snapshot is everything Advance is allowed to see: the reel's current state
// plus the facts that gate its next move. Loaded FOR UPDATE by the caller, so
// two concurrent Advance calls on the same reel serialise on the row.
type Snapshot struct {
	State State

	// The two independent upload preconditions.
	BytesReady    bool
	MetadataReady bool

	// Probe finished and the asset passed the §2.3 validation gate.
	ProbeOK bool

	// At least one playable rendition exists. Zero renditions with a
	// "transcode complete" signal is a provider bug, and publishing on it would
	// put an unplayable reel in the feed.
	RenditionCount int

	// Every moderation layer that was scheduled has finished.
	ModerationComplete bool
	// approve | reject | review — empty until a decision exists.
	Decision string

	// Coverage geometry resolved (§4.1). A published reel without it is
	// invisible to every geo query, which reads as a pipeline bug rather than
	// the data bug it is.
	CoverageResolved bool
}

// Step is one legal move plus the job it kicks off.
type Step struct {
	To     State
	Job    JobKind
	Reason string
}

// Advance returns the next step, or ok=false when the reel is waiting on
// something that has not happened yet.
//
// It is a pure function of the snapshot. That is what makes the race in §1.1
// tractable: correctness does not depend on which caller ran, only on what is
// true when it ran.
func Advance(s Snapshot) (Step, bool) {
	switch s.State {

	case StateDraft:
		// Gate on BOTH, not on bytes alone.
		//
		// Bytes alone would be enough to start probing, and probing is cheap.
		// But the step after it is transcoding, which is not — and a reel whose
		// author abandoned the caption form is never going to publish. Waiting
		// for both costs a few seconds of latency in the common case and saves
		// a transcode on every abandoned upload.
		if s.BytesReady && s.MetadataReady {
			return Step{To: StateUploaded, Job: JobProbe}, true
		}
		return Step{}, false

	case StateUploaded:
		return Step{To: StateProbing, Job: JobProbe}, true

	case StateProbing:
		if !s.ProbeOK {
			return Step{}, false
		}
		return Step{To: StateTranscoding, Job: JobTranscode}, true

	case StateTranscoding:
		if s.RenditionCount == 0 {
			return Step{}, false
		}
		return Step{To: StateModerating, Job: JobModerate}, true

	case StateModerating:
		if !s.ModerationComplete {
			return Step{}, false
		}
		switch s.Decision {
		case "approve":
			return Step{To: StateApproved, Job: JobPublish, Reason: "automated pass"}, true
		case "reject":
			return Step{To: StateRejected, Reason: "automated policy violation"}, true
		case "review":
			// Terminal for the pipeline. A human moves it from here; no job is
			// enqueued, because the thing that unblocks it is a person.
			return Step{To: StateReviewPending, Reason: "flagged for human review"}, true
		}
		return Step{}, false

	case StateApproved:
		if !s.CoverageResolved {
			// Deliberately not an error: coverage resolution is a job, and it
			// may not have run yet. The publish job re-enters here once it has.
			return Step{}, false
		}
		return Step{To: StatePublished}, true
	}

	return Step{}, false
}

// DecisionToState maps a moderation verdict onto the reel state it produces.
// Kept next to Advance so the two cannot drift.
func DecisionToState(decision string) (State, bool) {
	switch decision {
	case "approve":
		return StateApproved, true
	case "reject":
		return StateRejected, true
	case "review":
		return StateReviewPending, true
	}
	return "", false
}
