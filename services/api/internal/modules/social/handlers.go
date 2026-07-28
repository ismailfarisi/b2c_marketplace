// Package social owns the action kinds that belong to no vertical.
//
// `follow`, `share`, `open_menu` and `external` target vendors, branches and
// URLs — things that exist regardless of whether the platform sells laundry,
// meals or cars. Putting them in a vertical would mean deleting that vertical
// deletes the follow button; putting them in the feed would mean the feed knows
// what a vendor is, which is the leak the seam exists to prevent.
package social

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/action"
)

// Register wires the platform-level action kinds.
func Register(registry *action.Registry, pool *pgxpool.Pool) {
	branch := branchHandler{pool: pool}

	registry.Register(action.KindFollow, action.TargetVendor, followHandler{})
	registry.Register(action.KindFollow, action.TargetVendorBranch, followHandler{})
	registry.Register(action.KindShare, action.TargetVendorBranch, shareHandler{})
	registry.Register(action.KindOpenMenu, action.TargetVendorBranch, branch)
	registry.Register(action.KindExternal, action.TargetURL, externalHandler{})
}

type followHandler struct{}

func (followHandler) Resolve(_ context.Context, a action.Action, v action.Viewer) (action.Resolved, error) {
	label, labelAr := "Follow", "متابعة"
	if a.Label != "" {
		label, labelAr = a.Label, a.LabelAr
	}
	return action.Resolved{
		Label:   label,
		LabelAr: labelAr,
		// Available to anonymous viewers too. The tap prompts a sign-in, which
		// is a far better conversion moment than a login wall on the feed
		// itself — the viewer has already decided they want something.
		Available: true,
		Route:     fmt.Sprintf("/%s/%s/follow", a.TargetType, a.TargetID),
		Meta:      map[string]any{"requires_auth": !v.UserID.Valid},
	}, nil
}

type shareHandler struct{}

func (shareHandler) Resolve(_ context.Context, a action.Action, _ action.Viewer) (action.Resolved, error) {
	label, labelAr := "Share", "مشاركة"
	if a.Label != "" {
		label, labelAr = a.Label, a.LabelAr
	}
	return action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     fmt.Sprintf("/r/%s", a.ReelID),
		// The surface a shared link arrives on. Attribution treats it as
		// off-feed, because a vendor pushing their own reel to a WhatsApp group
		// is a marketing channel, not evidence that the feed works.
		Meta: map[string]any{"surface": "share_link"},
	}, nil
}

// branchHandler resolves CTAs that point at a branch.
//
// The eligibility check is the point. A branch that is closed, deactivated or
// out of range for this viewer must render as unavailable *with a reason* —
// a greyed-out button with no explanation reads as a bug, and the vendor gets
// blamed for a platform decision.
type branchHandler struct {
	pool *pgxpool.Pool
}

func (h branchHandler) Resolve(ctx context.Context, a action.Action, v action.Viewer) (action.Resolved, error) {
	label, labelAr := "View menu", "عرض القائمة"
	if a.Label != "" {
		label, labelAr = a.Label, a.LabelAr
	}

	res := action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     fmt.Sprintf("/branches/%s", a.TargetID),
	}

	var active bool
	var inArea *bool
	err := h.pool.QueryRow(ctx, `
		SELECT (b.is_active AND b.deleted_at IS NULL),
		       CASE
		           WHEN $2::float8 IS NULL OR b.service_area IS NULL THEN NULL
		           ELSE ST_Intersects(b.service_area,
		                    ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography)
		       END
		  FROM vendor_branches b
		 WHERE b.id = $1`,
		a.TargetID, nullableFloat(v.Lat), nullableFloat(v.Lng),
	).Scan(&active, &inArea)
	if err != nil {
		// An eligibility check that errors must not blank the CTA. The feed
		// renders it optimistically and the booking flow refuses properly —
		// which is the right order, because the booking flow can explain itself
		// and a feed tile cannot.
		return res, nil
	}

	switch {
	case !active:
		res.Available = false
		res.Reason = "This branch is not currently accepting orders"
	case inArea != nil && !*inArea:
		res.Available = false
		res.Reason = "Outside this vendor's delivery area"
	}
	return res, nil
}

type externalHandler struct{}

// Resolve refuses anything that is not http(s).
//
// A CTA is a link a vendor supplies and a customer taps inside our app. Letting
// `javascript:` or a custom scheme through makes the feed a delivery mechanism
// for whatever a vendor account can be persuaded to post.
func (externalHandler) Resolve(_ context.Context, a action.Action, _ action.Viewer) (action.Resolved, error) {
	label, labelAr := "Learn more", "اعرف المزيد"
	if a.Label != "" {
		label, labelAr = a.Label, a.LabelAr
	}

	parsed, err := url.Parse(strings.TrimSpace(a.TargetURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return action.Resolved{
			Label:     label,
			LabelAr:   labelAr,
			Available: false,
			Reason:    "This link is not available",
		}, nil
	}

	return action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     parsed.String(),
		Meta:      map[string]any{"external": true, "host": parsed.Host},
	}, nil
}

func nullableFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}
