package reels

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/objectstore"
	"github.com/marketplace/api/internal/platform/postgres"
)

// UploadTTL is how long a presigned URL and its draft row stay valid.
//
// Two hours, matching the `expired` transition in §1.2. Long enough for a
// vendor on a bad connection to finish a 30 MB upload and come back to it;
// short enough that abandoned drafts do not accumulate.
const UploadTTL = 2 * time.Hour

// SubjectAuthorizer answers "may this user post a reel about this thing".
//
// The one interface this package gained in Phase 6, when user-authored seller
// reels arrived. Note what it is not: it is not "is this a car". A vendor-branch
// reel authorises through branch membership, which this package already knew
// how to do; a person is not a member of anything, so a user reel's authority
// has to come from owning the subject. Nothing here learns what the subject is.
//
// One implementation today (vehicles.SubjectAuthorizer). It becomes a map keyed
// on subject type when a second kind of user-authored content exists, and not
// before — a registry with one entry is a registry that has never been tested
// with two.
type SubjectAuthorizer interface {
	AuthorizeSubject(ctx context.Context, userID uuid.UUID, subjectType string, subjectID uuid.UUID) error
}

type Service struct {
	pool     *pgxpool.Pool
	repo     *Repository
	store    *objectstore.Store
	subjects SubjectAuthorizer
}

// NewService takes a SubjectAuthorizer, which may be nil.
//
// Nil disables user-authored uploads entirely rather than allowing them
// unchecked. A deployment without the vehicles vertical wired in should not be
// able to accept personal reels at all, and the safe direction for a missing
// dependency in an upload path is closed.
func NewService(pool *pgxpool.Pool, store *objectstore.Store, subjects SubjectAuthorizer) *Service {
	return &Service{pool: pool, repo: NewRepository(pool), store: store, subjects: subjects}
}

func (s *Service) Repo() *Repository { return s.repo }

type UploadRequest struct {
	UserID      uuid.UUID
	BranchID    uuid.UUID
	ContentType string
	SubjectType *string
	SubjectID   *uuid.UUID
}

type Upload struct {
	ReelID    uuid.UUID
	AssetID   uuid.UUID
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

// RequestUpload creates the draft and returns a presigned PUT.
//
// The presign happens before the transaction, not inside it. It is a signing
// operation against AWS's SDK — no network call in the common case, but it can
// reach for credentials — and holding a database transaction open across
// anything that might do IO to a third party is how one slow dependency becomes
// connection-pool exhaustion.
func (s *Service) RequestUpload(ctx context.Context, in UploadRequest) (*Upload, error) {
	if err := s.repo.AuthorizeBranch(ctx, in.UserID, in.BranchID); err != nil {
		return nil, err
	}

	presigned, err := s.store.PresignUpload(ctx, "vendor_branch", in.BranchID.String(), in.ContentType, UploadTTL)
	if err != nil {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "content_type", Code: "unsupported", Message: in.ContentType,
		}}}
	}

	var out Upload
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		reelID, assetID, err := s.repo.CreateDraft(ctx, tx, CreateDraftInput{
			AuthorType:  "vendor_branch",
			AuthorID:    in.BranchID,
			SubjectType: in.SubjectType,
			SubjectID:   in.SubjectID,
			Bucket:      s.store.Bucket(),
			S3Key:       presigned.Key,
			MimeType:    in.ContentType,
			UploadTTL:   UploadTTL,
		})
		if err != nil {
			return err
		}
		out = Upload{
			ReelID:    reelID,
			AssetID:   assetID,
			URL:       presigned.URL,
			Headers:   presigned.Headers,
			ExpiresAt: presigned.ExpiresAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UserUploadRequest is a seller reel: a person filming a thing they own.
type UserUploadRequest struct {
	UserID      uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	ContentType string
}

// RequestUserUpload is the Phase 6 authoring path.
//
// It differs from RequestUpload in exactly two places — where authority comes
// from, and what author_type is written — and in nothing else. The same
// presign, the same draft, the same state machine, the same moderation
// pipeline. That is the result Phase 2 was aiming at when it made `reels`
// polymorphic from the first migration instead of storing a branch id: adding a
// whole new class of author cost this function and an interface.
//
// One thing does change materially, and it is not code. Every one of these
// authors is `new` in author_trust, so moderation.Route sends all of it to a
// human before publish — which is correct and is a step change in review
// volume. PLAN-VIDEO-FIRST §6 calls that out as headcount rather than a
// feature; see §4 of the phase doc.
func (s *Service) RequestUserUpload(ctx context.Context, in UserUploadRequest) (*Upload, error) {
	if s.subjects == nil {
		return nil, fmt.Errorf("%w: personal reels are not enabled", httpx.ErrForbidden)
	}
	if err := s.subjects.AuthorizeSubject(ctx, in.UserID, in.SubjectType, in.SubjectID); err != nil {
		return nil, err
	}

	presigned, err := s.store.PresignUpload(ctx, "user", in.UserID.String(), in.ContentType, UploadTTL)
	if err != nil {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "content_type", Code: "unsupported", Message: in.ContentType,
		}}}
	}

	subjectType := in.SubjectType
	var out Upload
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		reelID, assetID, err := s.repo.CreateDraft(ctx, tx, CreateDraftInput{
			AuthorType:  "user",
			AuthorID:    in.UserID,
			SubjectType: &subjectType,
			SubjectID:   &in.SubjectID,
			Bucket:      s.store.Bucket(),
			S3Key:       presigned.Key,
			MimeType:    in.ContentType,
			UploadTTL:   UploadTTL,
		})
		if err != nil {
			return err
		}
		out = Upload{
			ReelID:    reelID,
			AssetID:   assetID,
			URL:       presigned.URL,
			Headers:   presigned.Headers,
			ExpiresAt: presigned.ExpiresAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SubmitMetadata is the second half of the §1.1 race.
//
// It stamps metadata_ready_at and then calls Advance, exactly as the S3 event
// handler does. Neither knows nor cares which of them ran first.
func (s *Service) SubmitMetadata(ctx context.Context, userID, reelID uuid.UUID, in MetadataInput) (*Reel, error) {
	reel, err := s.repo.Get(ctx, reelID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeAuthor(ctx, userID, reel); err != nil {
		return nil, err
	}

	if in.Visibility == "" {
		in.Visibility = "public"
	}

	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.SaveMetadata(ctx, tx, reelID, in); err != nil {
			return err
		}
		_, err := s.advance(ctx, tx, reelID, ActorAuthor(userID.String()))
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, reelID)
}

// NotifyBytesArrived is called by the S3 event consumer. The other half of the
// race.
func (s *Service) NotifyBytesArrived(ctx context.Context, assetID uuid.UUID, bytes int64) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		reelID, err := s.repo.MarkBytesReady(ctx, tx, assetID, bytes)
		if err != nil {
			return err
		}
		_, err = s.advance(ctx, tx, reelID, ActorSystem)
		return err
	})
}

// ConfirmBytes asks the object store directly whether the upload landed.
//
// # Why this exists alongside the S3 event
//
// It does not replace [NotifyBytesArrived] and must not be read as doing so.
// The event remains the trigger of record for exactly the reason §1.1 gives: it
// fires whether or not the client that uploaded is still alive, which is the
// only thing that works for a vendor console tab that gets closed mid-upload.
//
// What the event cannot do is respond to a client that *is* still alive and has
// just finished. On the app's sell path the seller taps Publish milliseconds
// after the last byte, and making them wait on a bucket notification adds a
// delivery hop to the one moment in the flow they are watching a spinner. A HEAD
// against the key answers the same question immediately.
//
// It is also, today, the only thing that answers it at all: this deployment has
// no S3 event consumer wired — `NotifyBytesArrived` has no caller — so without
// this a reel created from the app would sit in `draft` forever with its bytes
// sitting in the bucket beside it.
//
// # It stamps and stops
//
// It deliberately does not call Advance, and the first version of it did — which
// stranded every reel it touched.
//
// Advance moves a reel as far as its preconditions allow and *returns the job to
// enqueue*. The API process has no pipeline: the worker's media sweep is what
// runs probe, transcode and moderate, and it finds work by calling Advance
// itself and acting on the job that comes back. So a reel advanced here goes
// draft → uploaded → probing inside the request, the JobProbe that would have
// started the work is discarded with the return value, and the sweep then finds
// it sitting in `probing` where Advance correctly reports there is nothing to do
// until a probe that will never run has run. The reel stops forever, one state
// past the point anything can rescue it.
//
// Stamping only is both simpler and exactly what the S3 event path is for: the
// sweep picks the reel up on its next pass and drives it from `draft` with the
// job in hand. The cost is the sweep interval, which the live screen is already
// polling through.
//
// Idempotent and safe to race with the event — MarkBytesReady coalesces
// `bytes_ready_at`, so whichever commits second is a no-op.
//
// Returns ok=false when the object is not there yet. That is a normal answer —
// the client may still be uploading — and the caller reports it as "still
// uploading" rather than as a failure.
func (s *Service) ConfirmBytes(ctx context.Context, reelID uuid.UUID) (bool, error) {
	asset, err := s.repo.Asset(ctx, reelID)
	if err != nil {
		return false, err
	}
	if asset.BytesReady {
		return true, nil
	}
	if s.store == nil {
		return false, nil
	}

	size, present, err := s.store.Size(ctx, asset.S3Key)
	if err != nil {
		return false, err
	}
	// A zero-byte object is an upload that started and died. Treating it as
	// arrived would send an empty file to ffprobe, and the seller would get a
	// processing failure instead of a retry prompt.
	if !present || size <= 0 {
		return false, nil
	}

	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := s.repo.MarkBytesReady(ctx, tx, asset.ID, size)
		return err
	})
	return err == nil, err
}

// Advance moves a reel as far as its preconditions allow, inside one
// transaction, and returns the job to enqueue.
//
// It loops rather than taking a single step: when the S3 event lands last, the
// reel legitimately goes draft → uploaded → probing in one call, and stopping
// after one step would leave it waiting for a trigger that has already fired.
func (s *Service) Advance(ctx context.Context, reelID uuid.UUID, actor Actor) (JobKind, error) {
	var job JobKind
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		job, err = s.advance(ctx, tx, reelID, actor)
		return err
	})
	return job, err
}

func (s *Service) advance(ctx context.Context, tx pgx.Tx, reelID uuid.UUID, actor Actor) (JobKind, error) {
	job := JobNone

	// Bounded rather than `for {}`. The transition graph is acyclic in the
	// forward direction so this terminates, but a bug that made it cyclic would
	// otherwise spin holding a row lock — and a live-lock inside a transaction
	// is far harder to diagnose than a loop that stops.
	for range len(transitions) {
		snap, err := s.repo.LoadSnapshot(ctx, tx, reelID)
		if err != nil {
			return JobNone, err
		}

		step, ok := Advance(snap)
		if !ok {
			break
		}
		if err := s.repo.Transition(ctx, tx, reelID, snap.State, step.To, actor, step.Reason); err != nil {
			return JobNone, err
		}
		if step.Job != JobNone {
			job = step.Job
		}
		if step.To.Terminal() || step.To == StatePublished ||
			step.To == StateReviewPending || step.To == StateRejected {
			break
		}
	}
	return job, nil
}

func (s *Service) Get(ctx context.Context, userID, reelID uuid.UUID) (*Reel, error) {
	reel, err := s.repo.Get(ctx, reelID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeAuthor(ctx, userID, reel); err != nil {
		return nil, err
	}
	return reel, nil
}

// Remove is the author-initiated takedown.
//
// It keeps the row. A hard delete would take the moderation history, the state
// history and the reason a reel was rejected with it — which is precisely the
// evidence needed when the author disputes the decision or a regulator asks.
func (s *Service) Remove(ctx context.Context, userID, reelID uuid.UUID) error {
	reel, err := s.repo.Get(ctx, reelID)
	if err != nil {
		return err
	}
	if err := s.authorizeAuthor(ctx, userID, reel); err != nil {
		return err
	}
	if reel.State == StateRemoved {
		return nil
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.Transition(ctx, tx, reelID, reel.State, StateRemoved,
			ActorAuthor(userID.String()), "removed by author"); err != nil {
			return err
		}
		// The feed reads Postgres and the index mirrors it; both must learn
		// about the takedown, and the outbox is what guarantees the second one
		// does even if the indexer is down right now.
		return s.repo.InsertOutbox(ctx, tx, reelID, "reel.removed", map[string]any{
			"reel_id": reelID,
		})
	})
}

func (s *Service) Report(ctx context.Context, reporterID, reelID uuid.UUID, reason, detail string) error {
	if reason == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
		}}}
	}
	// Confirm the reel exists so a scanner cannot use this endpoint to probe
	// which reel IDs are real.
	if _, err := s.repo.Get(ctx, reelID); err != nil {
		return err
	}
	return s.repo.InsertReport(ctx, reelID, reporterID, reason, detail)
}

// authorizeAuthor checks that this user may act on this reel.
//
// Vendor-branch authorship resolves through membership; user authorship (Phase
// 6) is direct identity. Both are handled here so no caller has to know which
// kind of author it is holding.
func (s *Service) authorizeAuthor(ctx context.Context, userID uuid.UUID, reel *Reel) error {
	switch reel.AuthorType {
	case "vendor_branch":
		return s.repo.AuthorizeBranch(ctx, userID, reel.AuthorID)
	case "user":
		if reel.AuthorID != userID {
			return httpx.ErrForbidden
		}
		return nil
	}
	return fmt.Errorf("reels: unknown author type %q", reel.AuthorType)
}
