// Package search mirrors published reels into OpenSearch.
//
// It is not on the read path in Phase 2 — chronological + geo + one category
// filter is squarely inside what PostGIS does well, and adding a second
// datastore to the critical path before there are ranking signals is premature.
//
// It ships anyway, and that is the point. Indexing from day one means Phase 4
// flips the read path against an index that is already warm and already
// correct, instead of running a backfill under production load and discovering
// then that three months of documents were mapped wrong. It costs a day and it
// runs cold.
//
// This refines PLAN-VIDEO-FIRST §3, which called for OpenSearch "from day one".
// The better shape is: index from day one, serve from Postgres until ranking
// needs more.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Document is the indexed shape.
//
// Denormalised on purpose: an index that requires a join at query time is a
// database with worse guarantees. Everything the Phase 4 ranker will filter or
// boost on is here at write time.
type Document struct {
	ID          uuid.UUID  `json:"id"`
	AuthorType  string     `json:"author_type"`
	AuthorID    uuid.UUID  `json:"author_id"`
	AuthorName  string     `json:"author_name,omitempty"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	SubjectType *string    `json:"subject_type,omitempty"`
	SubjectID   *uuid.UUID `json:"subject_id,omitempty"`
	Caption     string     `json:"caption,omitempty"`
	// Stored separately from caption so an Arabic analyzer can be applied to
	// one and an English analyzer to the other. A single field with a single
	// analyzer serves neither language well, and this market writes in both
	// inside one sentence.
	CaptionLocale string   `json:"caption_locale,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	DurationMS    int      `json:"duration_ms"`

	// GeoJSON, which is what OpenSearch's geo_shape mapping expects. The
	// coverage polygon rather than a point: eligibility here is containment,
	// exactly as it is in the Postgres query.
	Coverage    json.RawMessage `json:"coverage,omitempty"`
	OriginPoint json.RawMessage `json:"origin_point,omitempty"`

	Views       int64     `json:"views"`
	Likes       int64     `json:"likes"`
	PublishedAt time.Time `json:"published_at"`

	// Phase 4 ranking signals.
	//
	// Indexed even though Phase 4 still ranks in Go against Postgres, and the
	// reason is the same one that justified indexing at all in Phase 2: the
	// moment retrieval moves here — which is when the candidate set stops
	// fitting a single GiST scan, not before — these fields need to have been
	// correct for months, not backfilled under load.
	//
	// Counts, never rates. The same rule as reel_features and for the same
	// reason: a stored rate is a number whose denominator has been discarded.
	Impressions         int64 `json:"impressions"`
	Clicks              int64 `json:"clicks"`
	Orders              int64 `json:"orders"`
	LifetimeImpressions int64 `json:"lifetime_impressions"`

	// The author prior, denormalised. A join at query time against a document
	// store is a database with worse guarantees.
	AuthorPublished int64 `json:"author_published"`
	AuthorOrders    int64 `json:"author_orders"`
	AuthorTakedowns int64 `json:"author_takedowns"`
}

// Indexer writes documents.
//
// An interface so the outbox consumer does not care whether OpenSearch is
// configured. In an environment without it, the consumer runs against the noop
// and the outbox still drains — a stalled outbox because an optional dependency
// is absent would block every other consumer behind it.
type Indexer interface {
	Index(ctx context.Context, doc Document) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type Client struct {
	baseURL string
	index   string
	http    *http.Client
}

func NewClient(baseURL, index string) *Client {
	return &Client{
		baseURL: baseURL,
		index:   index,
		// A short timeout because this runs behind the outbox relay: a hung
		// index call must fail and be retried, not hold the relay's worker
		// while every other event queues behind it.
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) Index(ctx context.Context, doc Document) error {
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("search: marshal document: %w", err)
	}

	// Document ID = reel ID, so the write is an upsert. At-least-once outbox
	// delivery means this call will be repeated, and an auto-generated ID would
	// turn every retry into a duplicate in the feed.
	url := fmt.Sprintf("%s/%s/_doc/%s", c.baseURL, c.index, doc.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("search: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("search: index request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("search: index returned %s", resp.Status)
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, id uuid.UUID) error {
	url := fmt.Sprintf("%s/%s/_doc/%s", c.baseURL, c.index, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("search: build delete request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("search: delete request: %w", err)
	}
	defer resp.Body.Close()

	// 404 is success: the document is absent, which is the requested state. The
	// alternative is a takedown that retries forever because the reel was never
	// indexed in the first place.
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("search: delete returned %s", resp.Status)
	}
	return nil
}

// NoopIndexer is what runs when OpenSearch is not configured.
type NoopIndexer struct{}

func (NoopIndexer) Index(context.Context, Document) error   { return nil }
func (NoopIndexer) Delete(context.Context, uuid.UUID) error { return nil }
