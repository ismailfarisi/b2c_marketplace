package property

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/action"
)

// EnquiryHandler owns the (enquire_property, property_listing) CTA.
//
// A separate kind from a car enquiry, which looks like over-modelling until you
// look at what each one produces. A car enquiry opens a thread between two
// private parties. A property enquiry is a regulated lead handed to a licensed
// brokerage and logged against a permit — the advertiser has a disclosure
// obligation the car seller does not, and the platform has a record-keeping one.
// One handler doing both would mean the second's obligations quietly applying
// to the first, or the first's laxity applying to the second.
type EnquiryHandler struct {
	pool *pgxpool.Pool
}

func NewEnquiryHandler(pool *pgxpool.Pool) *EnquiryHandler { return &EnquiryHandler{pool: pool} }

// Resolve renders the enquiry CTA.
//
// The unavailable cases are the ordinary marketplace ones plus one that belongs
// only to this vertical: an advert whose permit has lapsed. It is refused rather
// than quietly served, because a tap on it would generate a lead against an
// advert the platform is not currently entitled to be running.
func (h *EnquiryHandler) Resolve(ctx context.Context, a action.Action, v action.Viewer) (action.Resolved, error) {
	label, labelAr := "Enquire", "استفسر"
	if a.Label != "" {
		label, labelAr = a.Label, a.LabelAr
	}

	res := action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     fmt.Sprintf("/properties/%s/enquire", a.TargetID),
	}

	var (
		status        string
		purpose       string
		priceMinor    int64
		currency      string
		permitExpires *time.Time
		permitNumber  string
		agentBRN      string
	)
	err := h.pool.QueryRow(ctx, `
		SELECT status, purpose, price_minor, currency, permit_expires,
		       COALESCE(permit_number, ''), COALESCE(agent_brn, '')
		  FROM property_listings WHERE id = $1`, a.TargetID,
	).Scan(&status, &purpose, &priceMinor, &currency, &permitExpires,
		&permitNumber, &agentBRN)

	if errors.Is(err, pgx.ErrNoRows) {
		res.Available = false
		res.Reason = "This advert is no longer running"
		return res, nil
	}
	if err != nil {
		return res, nil
	}

	// The permit and the agent's licence ride along so a client can display them
	// with the CTA without a second round-trip. Both are things the advert is
	// legally required to show; making the client fetch them separately is how
	// one of the two ends up missing on a screen nobody re-checked.
	res.Meta = map[string]any{
		"price_minor":   priceMinor,
		"currency":      currency,
		"permit_number": permitNumber,
		"agent_brn":     agentBRN,
		"purpose":       purpose,
	}

	switch {
	case Status(status) == StatusLetOrSold:
		res.Available = false
		if Purpose(purpose) == PurposeRent {
			res.Reason = "This property has been let"
		} else {
			res.Reason = "This property has been sold"
		}
	case Status(status) != StatusActive:
		res.Available = false
		res.Reason = "This advert is not currently running"
	case PermitExpired(permitExpires, time.Now()):
		// Stated as the advert being withdrawn rather than as a permit problem.
		// The viewer is a would-be tenant; the compliance detail is the
		// platform's and the brokerage's business, and putting it on their
		// screen would be noise about somebody else's paperwork.
		res.Available = false
		res.Reason = "This advert has been withdrawn"
	}
	return res, nil
}
