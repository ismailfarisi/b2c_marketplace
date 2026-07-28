package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/modules/kyc"
	"github.com/marketplace/api/internal/modules/payouts"
)

// PayoutGate adapts kyc.Service to payouts.Eligibility.
//
// Two structurally identical types with a translation between them, which looks
// like ceremony and is not. `payouts` must not import `kyc` — it would then
// know about trade licences and Emirates IDs, and the next thing that needs a
// compliance check would import `payouts` to reuse it. The types are identical
// today because the question is simple today; the seam is what lets one of them
// grow without dragging the other with it.
type PayoutGate struct{ KYC *kyc.Service }

func (g PayoutGate) EligibleTx(
	ctx context.Context, tx pgx.Tx, subjectType string, subjectID uuid.UUID,
) (payouts.Decision, error) {
	status, err := g.KYC.EligibleTx(ctx, tx, subjectType, subjectID)
	if err != nil {
		return payouts.Decision{}, err
	}
	return payouts.Decision{Eligible: status.Eligible, Reasons: status.Reasons}, nil
}
