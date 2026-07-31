// Package documents handles the one upload in this product that is not a
// video.
//
// The subject seam's TrustUpload has always described a document — a mulkiya, a
// Trakheesi permit — and there has never been anywhere to put one. The sell
// flow drew the row and said "not built yet", which is honest and is still a
// promise the product makes and does not keep: a seller is told a mulkiya earns
// them Verified, and cannot supply one.
//
// # Why this is not part of `media`
//
// `media` is a video pipeline: probe, transcode ladder, moderation, publish.
// None of it applies. A document is not decoded, not transcoded, not served to
// anyone but a reviewer, and its "moderation" is a person deciding whether a
// registration card matches a listing — which is a different queue, a different
// skill and a different SLA from deciding whether a video breaks the rules.
//
// What they share is a presigned upload, and that lives in `objectstore` where
// both can reach it.
package documents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/objectstore"
)

// Accepted content types.
//
// A closed list rather than a prefix match on "image/". A seller photographing
// a document produces a JPEG or a PNG; a brokerage forwards a PDF. Everything
// else — an HEIC the reviewer's tooling cannot open, an SVG which is a script,
// a TIFF nobody can rotate — is a support ticket rather than a document.
var accepted = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"application/pdf": true,
}

// MaxBytes caps what will be accepted at confirmation.
//
// Twenty megabytes. A phone photograph of an A4 page is two to five; a
// multi-page scanned PDF can reach fifteen. Above that it is either a mistake
// or someone using the bucket as storage, and the cost of being wrong is a
// seller resizing a photograph rather than a document that cannot be reviewed.
const MaxBytes = 20 << 20

// UploadTTL is how long the presigned PUT is valid.
//
// Fifteen minutes, much shorter than a reel's: a document is one small file
// picked from a gallery, not a sixty-second video on a congested cell. A URL
// that outlives its use is a write handle on the bucket sitting in a log.
const UploadTTL = 15 * time.Minute

type Service struct {
	pool  *pgxpool.Pool
	store *objectstore.Store
}

func NewService(pool *pgxpool.Pool, store *objectstore.Store) *Service {
	return &Service{pool: pool, store: store}
}

type Document struct {
	ID          uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	Kind        string
	Status      string
	ContentType string
	ByteSize    *int64
	Reason      *string
	CreatedAt   time.Time
}

type UploadRequest struct {
	UploadedBy  uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	Kind        string
	ContentType string
}

type Upload struct {
	DocumentID uuid.UUID
	URL        string
	Headers    map[string]string
	ExpiresAt  time.Time
}

// RequestUpload mints a presigned PUT and records the row it belongs to.
//
// Ownership is *not* checked here, and that is a deliberate gap this function
// cannot close: `documents` does not know what a vehicle listing is, and asking
// it to would make it import every vertical — the thing the subject seam exists
// to prevent. The caller checks, and [OwnerCheck] is the interface it does it
// through.
func (s *Service) RequestUpload(ctx context.Context, in UploadRequest) (Upload, error) {
	if in.ContentType == "" {
		in.ContentType = "image/jpeg"
	}
	if !accepted[in.ContentType] {
		return Upload{}, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "content_type", Code: "unsupported",
			Message: "documents must be a JPEG, a PNG or a PDF",
		}}}
	}
	if in.Kind == "" || in.SubjectType == "" || in.SubjectID == uuid.Nil {
		return Upload{}, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "subject", Code: "required",
		}}}
	}

	presigned, err := s.store.PresignUpload(ctx, "documents", in.Kind, in.ContentType, UploadTTL)
	if err != nil {
		return Upload{}, fmt.Errorf("documents: presign: %w", err)
	}

	// Supersede whatever was in this slot before.
	//
	// A seller re-uploading has decided the previous one was wrong, and the
	// partial unique index would refuse the insert otherwise. Marked rejected
	// with a reason rather than deleted, because a reviewer looking at the
	// second document wants to know there was a first — that history is most of
	// what makes a doctored document detectable.
	var doc uuid.UUID
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE trust_documents
			   SET status = 'rejected',
			       reason = 'replaced by a newer upload',
			       reviewed_by = uploaded_by,
			       reviewed_at = now()
			 WHERE subject_type = $1 AND subject_id = $2 AND kind = $3
			   AND status IN ('pending', 'uploaded')`,
			in.SubjectType, in.SubjectID, in.Kind,
		); err != nil {
			return fmt.Errorf("supersede: %w", err)
		}
		return tx.QueryRow(ctx, `
			INSERT INTO trust_documents
			    (subject_type, subject_id, uploaded_by, kind, s3_key, content_type)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id`,
			in.SubjectType, in.SubjectID, in.UploadedBy, in.Kind,
			presigned.Key, in.ContentType,
		).Scan(&doc)
	})
	if err != nil {
		return Upload{}, fmt.Errorf("documents: request upload: %w", err)
	}

	return Upload{
		DocumentID: doc,
		URL:        presigned.URL,
		// Returned explicitly for the reason the reel upload returns them: a
		// presigned URL signs the headers it was created with, so a client that
		// omits Content-Type gets a signature mismatch that reads like an auth
		// failure and is not one.
		Headers:   presigned.Headers,
		ExpiresAt: presigned.ExpiresAt,
	}, nil
}

// Confirm records that the bytes arrived, after checking that they did.
//
// The size is read from the store rather than taken from the client. A client
// that says "done" is reporting on its own success, and the failure this
// catches is the ordinary one: an upload that 403'd on a signature mismatch,
// which the client sees as a completed request with an unhelpful status.
func (s *Service) Confirm(ctx context.Context, uploader, documentID uuid.UUID) (Document, error) {
	var d Document
	var key string
	err := s.pool.QueryRow(ctx, `
		SELECT id, subject_type, subject_id, kind, status, content_type, s3_key
		  FROM trust_documents
		 WHERE id = $1 AND uploaded_by = $2`, documentID, uploader,
	).Scan(&d.ID, &d.SubjectType, &d.SubjectID, &d.Kind, &d.Status, &d.ContentType, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not-found rather than forbidden for a document belonging to someone
		// else: distinguishing them would let a caller enumerate ids.
		return Document{}, httpx.ErrNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("documents: load: %w", err)
	}
	if d.Status != "pending" {
		// Idempotent. A client retrying a confirm it already made is the normal
		// consequence of a dropped response, and re-checking the store for it
		// would be a round trip to reach the same answer.
		return d, nil
	}

	size, ok, err := s.store.Size(ctx, key)
	if err != nil {
		return Document{}, fmt.Errorf("documents: size: %w", err)
	}
	if !ok || size == 0 {
		return Document{}, fmt.Errorf("%w: the upload did not arrive", httpx.ErrConflict)
	}
	if size > MaxBytes {
		return Document{}, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "file", Code: "too_large",
			Message: "documents must be under 20 MB",
		}}}
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE trust_documents SET status = 'uploaded', byte_size = $2
		 WHERE id = $1`, documentID, size); err != nil {
		return Document{}, fmt.Errorf("documents: confirm: %w", err)
	}
	d.Status = "uploaded"
	d.ByteSize = &size
	return d, nil
}

// ForSubject lists what has been uploaded against one subject.
//
// Used by the sell flow to render the trust row's current state, so a seller
// who uploaded a mulkiya yesterday is not asked for it again.
func (s *Service) ForSubject(ctx context.Context, subjectType string, subjectID uuid.UUID) ([]Document, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, subject_type, subject_id, kind, status, content_type,
		       byte_size, reason, created_at
		  FROM trust_documents
		 WHERE subject_type = $1 AND subject_id = $2
		 ORDER BY created_at DESC`, subjectType, subjectID)
	if err != nil {
		return nil, fmt.Errorf("documents: for subject: %w", err)
	}
	defer rows.Close()

	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.SubjectType, &d.SubjectID, &d.Kind, &d.Status,
			&d.ContentType, &d.ByteSize, &d.Reason, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("documents: scan: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Queue is the review list, oldest first.
func (s *Service) Queue(ctx context.Context, limit int) ([]Document, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, subject_type, subject_id, kind, status, content_type,
		       byte_size, reason, created_at
		  FROM trust_documents
		 WHERE status = 'uploaded'
		 ORDER BY created_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("documents: queue: %w", err)
	}
	defer rows.Close()

	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.SubjectType, &d.SubjectID, &d.Kind, &d.Status,
			&d.ContentType, &d.ByteSize, &d.Reason, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("documents: scan queue: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Decide approves or rejects. A rejection needs a reason, enforced by the table
// as well as here — a document refused with no explanation is a seller who
// uploads the same photograph again.
func (s *Service) Decide(ctx context.Context, reviewer, documentID uuid.UUID, approve bool, reason string) error {
	if !approve && reason == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "say why, so the seller can fix it",
		}}}
	}
	status := "approved"
	var why *string
	if !approve {
		status = "rejected"
		why = &reason
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE trust_documents
		   SET status = $2, reason = $3, reviewed_by = $4, reviewed_at = now()
		 WHERE id = $1 AND status = 'uploaded'`,
		documentID, status, why, reviewer)
	if err != nil {
		return fmt.Errorf("documents: decide: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either it does not exist or somebody else has already judged it.
		// Refusing rather than overwriting: two reviewers reaching opposite
		// conclusions should be a conflict a human resolves, not a race the
		// later write wins.
		return httpx.ErrConflict
	}
	return nil
}

// ReadURL is a short-lived link for a reviewer.
//
// Sixty seconds, and never handed to a buyer. A mulkiya carries a chassis
// number, an owner's name and an address; the whole reason it is collected is
// that it is sensitive enough to prove something.
func (s *Service) ReadURL(ctx context.Context, documentID uuid.UUID) (string, error) {
	var key string
	err := s.pool.QueryRow(ctx,
		`SELECT s3_key FROM trust_documents WHERE id = $1`, documentID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("documents: read url: %w", err)
	}
	return s.store.PresignDownload(ctx, key, time.Minute)
}
