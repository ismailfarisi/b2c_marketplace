-- Documents a seller uploads to prove something about a subject.
--
-- The mulkiya behind "Add mulkiya to earn Verified", the Trakheesi permit
-- behind a property advert, and whatever the next vertical asks for. Until now
-- the sell flow drew that row and said "not built yet", which is at least
-- honest and is still a promise the product makes and does not keep.
--
-- # Why one table rather than a column per vertical
--
-- Because the seam already says these are the same thing. `TrustUpload` is one
-- optional slot on a Composition, declared by the vertical, and every vertical
-- gets exactly one. A `mulkiya_key` column on vehicle_listings and a
-- `permit_scan_key` on property_listings would be the same table twice with the
-- review workflow written twice, and the third vertical would write it a third
-- time.
--
-- Keyed on (subject_type, subject_id) rather than by a foreign key, matching
-- how `reels` refers to its subject. There is no referential integrity to a
-- polymorphic target and that is accepted for the same reason it is accepted
-- there: the alternative is this module importing every vertical.

CREATE TABLE trust_documents (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    -- What it is about. subject_type is not constrained to a list here, unlike
    -- reels.subject_type: a document is not rendered anywhere a wrong value
    -- could mislead a buyer, and the CHECK on reels exists to protect the feed.
    subject_type text NOT NULL,
    subject_id   uuid NOT NULL,

    -- Who uploaded it. Not derived from the subject, because ownership of a
    -- subject can change — a dealership takes over a private listing — and the
    -- person who submitted a document is a fact about the past.
    uploaded_by  uuid NOT NULL REFERENCES users(id),

    -- The vertical's own key for the slot: 'mulkiya', 'permit'. Matches
    -- TrustUpload.Key, and is the vertical's vocabulary rather than this
    -- table's — which is why it is text and not an enum.
    kind         text NOT NULL,

    s3_key       text NOT NULL,
    content_type text NOT NULL,
    byte_size    bigint,

    -- pending  — presigned, bytes not confirmed. Most rows die here: a seller
    --            who opens the picker and backs out leaves one behind.
    -- uploaded — bytes are in the store and their size is known.
    -- approved / rejected — a person has looked.
    status       text NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'uploaded', 'approved', 'rejected')),

    -- Set on rejection and shown to the uploader. A document refused with no
    -- reason is a seller who uploads the same photograph again.
    reason       text,
    reviewed_by  uuid REFERENCES users(id),
    reviewed_at  timestamptz,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT trust_documents_reviewed_has_reviewer CHECK (
        status NOT IN ('approved', 'rejected') OR reviewed_by IS NOT NULL
    ),
    CONSTRAINT trust_documents_rejected_has_reason CHECK (
        status <> 'rejected' OR (reason IS NOT NULL AND reason <> '')
    )
);

-- One live document per slot per subject.
--
-- Partial on the two states that count, so a seller can replace a rejected
-- mulkiya without deleting the record of it having been rejected — which is
-- exactly the history a reviewer wants when the second one looks doctored.
CREATE UNIQUE INDEX trust_documents_one_live
    ON trust_documents (subject_type, subject_id, kind)
    WHERE status IN ('pending', 'uploaded', 'approved');

CREATE INDEX trust_documents_subject
    ON trust_documents (subject_type, subject_id);

-- The review queue: oldest first, which is the only fair order for a queue
-- somebody is waiting in.
CREATE INDEX trust_documents_queue
    ON trust_documents (created_at)
    WHERE status = 'uploaded';

CREATE TRIGGER trust_documents_updated_at
    BEFORE UPDATE ON trust_documents
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
