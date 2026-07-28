package moderation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/reels"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Service is the moderator-facing half: the review queue and the decision that
// empties it.
type Service struct {
	pool     *pgxpool.Pool
	repo     *Repository
	reels    *reels.Service
	reelRepo *reels.Repository
}

func NewService(pool *pgxpool.Pool, repo *Repository, reelSvc *reels.Service) *Service {
	return &Service{pool: pool, repo: repo, reels: reelSvc, reelRepo: reelSvc.Repo()}
}

// requireModerator checks platform staff membership on every privileged call.
//
// Checked against the table on each request rather than carried as a JWT claim:
// removing someone's moderation rights has to take effect now, and a claim
// stays valid until the access token expires — which, at a 15-minute TTL, is
// fifteen minutes of a revoked moderator still approving content.
// RequireStaff is the exported form, for callers outside this package that
// guard a privileged surface — the platform funnel, which is the number that
// decides whether to keep funding the feed.
func (s *Service) RequireStaff(ctx context.Context, userID uuid.UUID) error {
	return s.requireModerator(ctx, userID)
}

func (s *Service) requireModerator(ctx context.Context, userID uuid.UUID) error {
	var ok bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM platform_admins WHERE user_id = $1)`,
		userID).Scan(&ok); err != nil {
		return fmt.Errorf("moderation: authorize: %w", err)
	}
	if !ok {
		return httpx.ErrForbidden
	}
	return nil
}

func (s *Service) Queue(ctx context.Context, userID uuid.UUID, limit int) ([]QueueItem, error) {
	if err := s.requireModerator(ctx, userID); err != nil {
		return nil, err
	}
	return s.repo.Queue(ctx, limit)
}

// Decide records a moderator's ruling and moves the reel.
//
// One transaction covering the decision, the strike, the state change and the
// outbox event. A decision that commits without its state change leaves a reel
// in the queue that a moderator has already ruled on — which the next moderator
// rules on again, and the author receives two contradictory notifications.
func (s *Service) Decide(ctx context.Context, moderatorID, reelID uuid.UUID, verdict Verdict, notes string) error {
	if err := s.requireModerator(ctx, moderatorID); err != nil {
		return err
	}
	if verdict != VerdictApprove && verdict != VerdictReject {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "decision", Code: "invalid",
			Message: "a moderator decides approve or reject; review is where it already is",
		}}}
	}

	reel, err := s.reelRepo.Get(ctx, reelID)
	if err != nil {
		return err
	}
	if reel.State != reels.StateReviewPending {
		return fmt.Errorf("%w: reel is %s, not awaiting review", httpx.ErrConflict, reel.State)
	}

	target := reels.StateApproved
	if verdict == VerdictReject {
		target = reels.StateRejected
	}

	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.SaveDecision(ctx, tx, reelID, Decision{Verdict: verdict},
			"moderator", &moderatorID, notes); err != nil {
			return err
		}
		if err := s.repo.ResolveReports(ctx, tx, reelID, verdict == VerdictReject, moderatorID); err != nil {
			return err
		}
		if verdict == VerdictReject {
			if err := s.repo.RecordStrike(ctx, tx, reel.AuthorType, reel.AuthorID, time.Now()); err != nil {
				return err
			}
			if err := s.reelRepo.RecordFailure(ctx, tx, reelID, "policy_violation", notes); err != nil {
				return err
			}
		}
		return s.reelRepo.Transition(ctx, tx, reelID, reels.StateReviewPending, target,
			reels.ActorModerator(moderatorID.String()), notes)
	})
	if err != nil {
		return err
	}

	// Approval only reaches `approved`. Publication is a separate step because
	// it still needs coverage resolved, and folding the two together would let
	// a moderator's click publish a reel with no audience.
	return nil
}

// Takedown removes an already-published reel.
//
// Kept distinct from Decide because it is a different act with a different
// audit trail: Decide rules on content that was never visible, Takedown removes
// content that people have already seen.
func (s *Service) Takedown(ctx context.Context, moderatorID, reelID uuid.UUID, reason string) error {
	if err := s.requireModerator(ctx, moderatorID); err != nil {
		return err
	}
	reel, err := s.reelRepo.Get(ctx, reelID)
	if err != nil {
		return err
	}
	if reel.State == reels.StateRemoved {
		return nil
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.SaveDecision(ctx, tx, reelID, Decision{Verdict: VerdictReject},
			"moderator", &moderatorID, reason); err != nil {
			return err
		}
		if err := s.repo.ResolveReports(ctx, tx, reelID, true, moderatorID); err != nil {
			return err
		}
		if err := s.repo.RecordStrike(ctx, tx, reel.AuthorType, reel.AuthorID, time.Now()); err != nil {
			return err
		}
		if err := s.reelRepo.Transition(ctx, tx, reelID, reel.State, reels.StateRemoved,
			reels.ActorModerator(moderatorID.String()), reason); err != nil {
			return err
		}
		// The search index mirrors published reels and must learn about this.
		// Through the outbox rather than a direct call, so a takedown is not
		// blocked by OpenSearch being unreachable.
		return s.reelRepo.InsertOutbox(ctx, tx, reelID, "reel.removed", map[string]any{
			"reel_id": reelID,
			"reason":  reason,
		})
	})
}
