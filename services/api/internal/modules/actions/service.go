package actions

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/seam/action"
)

// Authorizer answers "may this user act as this reel's author". Supplied by the
// reels module rather than reimplemented here — two copies of an authorisation
// rule is one copy too many, and the one that drifts is always the one nobody
// is looking at.
type Authorizer interface {
	AuthorizeBranch(ctx context.Context, userID, branchID uuid.UUID) error
}

type Service struct {
	pool     *pgxpool.Pool
	repo     *Repository
	registry *action.Registry
	authz    Authorizer
}

func NewService(pool *pgxpool.Pool, registry *action.Registry, authz Authorizer) *Service {
	return &Service{pool: pool, repo: NewRepository(pool), registry: registry, authz: authz}
}

func (s *Service) Repo() *Repository { return s.repo }

// MaxActions bounds a reel's CTA list.
//
// Three. Not a storage limit — a product one. A reel with six buttons has no
// primary call to action, and the measurement gets worse too: attribution
// credits one action, so spreading intent across six makes every one of them
// look weak.
const MaxActions = 3

// Set replaces a reel's CTAs.
//
// Refused once the reel has been through moderation. Two reasons, and the
// second is the one that matters: a CTA swapped after approval means the
// reviewed content and the live destination are different things, which is the
// oldest trick in content moderation. And repointing a published reel's CTA
// would silently reassign its accumulated click history to a new target,
// corrupting the funnel retroactively.
func (s *Service) Set(ctx context.Context, userID, reelID uuid.UUID, inputs []Input) error {
	if len(inputs) > MaxActions {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "actions", Code: "too_many",
			Message: fmt.Sprintf("a reel carries at most %d actions", MaxActions),
		}}}
	}

	reel, err := s.repo.ReelState(ctx, reelID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, userID, reel); err != nil {
		return err
	}

	switch reel.State {
	case "draft", "uploaded", "probing", "transcoding":
	default:
		return fmt.Errorf("%w: actions are fixed once moderation has begun", httpx.ErrConflict)
	}

	if err := s.validate(inputs); err != nil {
		return err
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.repo.Replace(ctx, tx, reelID, inputs)
	})
}

// validate checks the shape of each CTA and that a handler exists for it.
//
// The handler check is the valuable half. Without it an author can attach a CTA
// nobody implemented, it stores fine, and the failure surfaces as a greyed-out
// button in the feed weeks later — at which point it looks like a feed bug.
func (s *Service) validate(inputs []Input) error {
	var fields []httpx.FieldError

	for i, in := range inputs {
		path := fmt.Sprintf("actions[%d]", i)

		if in.TargetType == action.TargetURL {
			if in.TargetURL == "" {
				fields = append(fields, httpx.FieldError{
					Field: path + ".target_url", Code: "required",
				})
			}
		} else if in.TargetID == nil {
			fields = append(fields, httpx.FieldError{
				Field: path + ".target_id", Code: "required",
			})
		}

		if !s.registry.Has(in.Kind, in.TargetType) {
			fields = append(fields, httpx.FieldError{
				Field: path + ".kind", Code: "unsupported_action",
				Message: fmt.Sprintf("%s on %s has no handler registered",
					in.Kind, in.TargetType),
			})
		}
	}

	if len(fields) > 0 {
		return &httpx.ValidationError{Fields: fields}
	}
	return nil
}

// ResolvePage resolves every CTA on a page of reels.
//
// Two round-trips for the whole page — one to read the actions, one batch
// resolve — rather than two per reel. On the feed this is the difference
// between a page costing 2 queries and 40.
func (s *Service) ResolvePage(ctx context.Context, reelIDs []uuid.UUID, viewer action.Viewer) (map[uuid.UUID][]Resolved, error) {
	stored, err := s.repo.ListForReels(ctx, reelIDs)
	if err != nil {
		return nil, err
	}

	// Flatten so the registry sees one batch, then scatter the results back.
	var flat []action.Action
	var owners []uuid.UUID
	for _, reelID := range reelIDs {
		for _, a := range stored[reelID] {
			flat = append(flat, a)
			owners = append(owners, reelID)
		}
	}
	if len(flat) == 0 {
		return map[uuid.UUID][]Resolved{}, nil
	}

	results, err := s.registry.ResolveBatch(ctx, flat, viewer)
	if err != nil {
		return nil, err
	}

	out := make(map[uuid.UUID][]Resolved, len(reelIDs))
	for i, r := range results {
		out[owners[i]] = append(out[owners[i]], Resolved{
			ID:         flat[i].ID,
			Kind:       flat[i].Kind,
			TargetType: flat[i].TargetType,
			TargetID:   flat[i].TargetID,
			Position:   flat[i].Position,
			Resolved:   r,
		})
	}
	return out, nil
}

// Resolved is a CTA as the client receives it: the identity it must send back
// on click, plus whatever the owning vertical decided about it.
type Resolved struct {
	ID         uuid.UUID
	Kind       action.Kind
	TargetType action.TargetType
	TargetID   uuid.UUID
	Position   int
	Resolved   action.Resolved
}

// ClickRequest is a CTA press as reported by a client.
type ClickRequest struct {
	ActionID  uuid.UUID
	UserID    uuid.NullUUID
	ViewerKey string
	SessionID string
	Surface   string
	WatchedMS int
	Viewer    action.Viewer
}

// Click records the press and returns where to send the user.
//
// Recording and routing are one call on purpose. If the client routed itself
// and reported the click separately, every dropped analytics request would be a
// conversion the server never saw — and those drops are not random, they
// correlate with exactly the bad-network conditions where conversion is already
// lowest. Making the route depend on the record means the number cannot quietly
// under-count.
func (s *Service) Click(ctx context.Context, in ClickRequest) (*Resolved, error) {
	stored, err := s.repo.Get(ctx, in.ActionID)
	if err != nil {
		return nil, err
	}

	if in.ViewerKey == "" {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "viewer_key", Code: "required",
			Message: "a viewer identity is required to attribute this click",
		}}}
	}

	resolved, err := s.registry.Resolve(ctx, stored, in.Viewer)
	if err != nil {
		return nil, err
	}

	// The click is recorded even when the CTA resolves unavailable. A viewer
	// pressing "Book" on a fully-booked branch is demand, and it is the most
	// actionable demand signal the vendor will ever get — dropping it because
	// the outcome failed would hide a capacity problem behind a conversion one.
	clickID, err := s.repo.RecordClick(ctx, ClickInput{
		ActionID:  stored.ID,
		ReelID:    stored.ReelID,
		UserID:    in.UserID,
		ViewerKey: in.ViewerKey,
		SessionID: in.SessionID,
		Surface:   in.Surface,
		WatchedMS: in.WatchedMS,
	})
	if err != nil {
		return nil, err
	}
	_ = clickID

	return &Resolved{
		ID:         stored.ID,
		Kind:       stored.Kind,
		TargetType: stored.TargetType,
		TargetID:   stored.TargetID,
		Position:   stored.Position,
		Resolved:   resolved,
	}, nil
}

func (s *Service) authorize(ctx context.Context, userID uuid.UUID, reel ReelState) error {
	switch reel.AuthorType {
	case "vendor_branch":
		return s.authz.AuthorizeBranch(ctx, userID, reel.AuthorID)
	case "user":
		if reel.AuthorID != userID {
			return httpx.ErrForbidden
		}
		return nil
	}
	return fmt.Errorf("actions: unknown author type %q", reel.AuthorType)
}

// --- social ------------------------------------------------------------------

func (s *Service) Follow(ctx context.Context, userID uuid.UUID, followeeType string, followeeID uuid.UUID) error {
	switch followeeType {
	case "vendor", "vendor_branch", "user":
	default:
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "followee_type", Code: "invalid",
		}}}
	}
	return s.repo.Follow(ctx, userID, followeeType, followeeID)
}

func (s *Service) Unfollow(ctx context.Context, userID uuid.UUID, followeeType string, followeeID uuid.UUID) error {
	return s.repo.Unfollow(ctx, userID, followeeType, followeeID)
}

func (s *Service) Save(ctx context.Context, userID, reelID uuid.UUID) error {
	return s.repo.Save(ctx, userID, reelID)
}

func (s *Service) Unsave(ctx context.Context, userID, reelID uuid.UUID) error {
	return s.repo.Unsave(ctx, userID, reelID)
}

func (s *Service) Saved(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	return s.repo.SavedReelIDs(ctx, userID, limit)
}
