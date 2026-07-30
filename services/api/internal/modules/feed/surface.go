package feed

import (
	"fmt"
	"time"
)

// Surface is which feed the viewer asked for — the tab row at the top of the
// app.
//
// A surface is not a filter applied to a page the ranker already built. It
// changes candidate generation and, for two of the four, the ordering itself.
// That distinction is the whole reason this is a server concept: a client that
// filtered the ten reels it had been served could only ever sort those ten, and
// "ending soon" over ten reels the ranker chose for engagement is not ending
// soon — it is the wrong answer, arrived at quickly.
type Surface string

const (
	// SurfaceForYou is the ranked feed and the default. Everything else in this
	// file is a departure from it.
	SurfaceForYou Surface = "for_you"

	// SurfaceLiveNow is the set whose time signal is open right now — an auction
	// mid-run, an offer inside its window. Still ranked: being live earns a reel
	// candidacy, not a position.
	SurfaceLiveNow Surface = "live_now"

	// SurfaceEndingSoon is the set closing within EndingSoonHorizon, ordered by
	// how soon. The ordering is the surface; a viewer who opened this tab has
	// stated their sort key and ranking on predicted engagement instead would be
	// answering a question they did not ask.
	SurfaceEndingSoon Surface = "ending_soon"

	// SurfaceNearby is ordered by distance.
	//
	// Worth being precise about, because it is easy to assume the whole feed is
	// already this. It is not: every surface here is *eligible* — the reel's
	// coverage contains the viewer — and eligibility is not proximity. A vendor
	// who delivers city-wide is eligible from thirty kilometres away. This tab
	// is for the viewer who wants to walk there.
	SurfaceNearby Surface = "nearby"
)

// EndingSoonHorizon bounds the ending-soon surface.
//
// Six hours rather than twenty-four. The tab has to feel like a different feed
// from For you, and at a day's horizon a launch district's whole inventory
// qualifies — at which point the ordering is arbitrary and the tab is noise. Six
// hours is also roughly the span over which someone will actually change their
// evening.
const EndingSoonHorizon = 6 * time.Hour

// ParseSurface maps the query parameter, defaulting rather than failing.
//
// An unknown surface is For you and not a 400. The parameter is a *view
// preference* arriving from a client that may be older than the server: a build
// that has not learned about a tab we removed should get a feed, not an error
// screen. The same reasoning the cursor gets, for the same class of caller.
func ParseSurface(raw string) Surface {
	switch Surface(raw) {
	case SurfaceLiveNow:
		return SurfaceLiveNow
	case SurfaceEndingSoon:
		return SurfaceEndingSoon
	case SurfaceNearby:
		return SurfaceNearby
	default:
		return SurfaceForYou
	}
}

// NeedsTimeSignal reports whether this surface restricts candidates to reels
// that have one.
//
// Read by retrieve to decide whether the join is a filter or a decoration, and
// by the handler to decide whether an empty page means "nothing is live" or
// "something is wrong" — those are different messages and the viewer deserves
// the right one.
func (s Surface) NeedsTimeSignal() bool {
	return s == SurfaceLiveNow || s == SurfaceEndingSoon
}

// candidatePredicate is the SQL fragment that narrows `eligible` to this
// surface, as a boolean over a joined `ts` (reel_time_signals) row.
//
// Returned as text spliced into the query rather than passed as a parameter,
// which is safe because it is a constant chosen by a closed switch — no caller
// input reaches it — and which matters because the alternative is four
// parameters that are NULL three times out of four and a WHERE clause nobody can
// read.
func (s Surface) candidatePredicate() string {
	switch s {
	case SurfaceLiveNow:
		// Open right now. `starts_at IS NULL` counts as open because a countdown
		// may not carry one — the field is optional there and exists only to
		// give the bar a denominator.
		return `ts.ends_at > now()
		        AND (ts.starts_at IS NULL OR ts.starts_at <= now())
		        AND ts.mode IN ('countdown', 'window')`
	case SurfaceEndingSoon:
		return `ts.ends_at > now()
		        AND ts.ends_at < now() + make_interval(secs => $15)
		        AND ts.mode <> 'freshness'`
	default:
		return "true"
	}
}

// eligibleOrder is how the capped eligible set is chosen, not how the page is
// ordered.
//
// It matters because `eligible` is bounded by eligibleCap, and a cap is a
// silent filter: whatever the ORDER BY puts last is what the cap discards. Newest
// first is right for a ranked feed and wrong for Ending soon — in a city with
// more than eligibleCap live reels it would keep the most recently published and
// throw away the ones closing in ten minutes, which are the only ones the tab
// exists to show. The bug would never appear in a launch district and would
// arrive fully formed on the day the catalogue crossed the cap.
func (s Surface) eligibleOrder() string {
	if s == SurfaceEndingSoon {
		return "ts.ends_at ASC"
	}
	return "r.published_at DESC"
}

// freshOrder is the same decision one level down.
//
// The `fresh` source re-caps the eligible set at freshQuota, so it is a second
// silent filter with the same failure mode, and fixing only the outer one would
// leave the bug intact with a smaller threshold. Unqualified because it reads
// from the `eligible` CTE's own column list rather than from `reel_time_signals`.
func (s Surface) freshOrder() string {
	if s == SurfaceEndingSoon {
		return "ends_at ASC"
	}
	return "published_at DESC"
}

// overrideScore replaces the ranker's score for the surfaces whose ordering the
// viewer chose.
//
// Implemented as a score override rather than as a re-sort of the finished page,
// and that is the load-bearing decision in this file. Everything downstream of
// scoring — the exploration partition, the per-author cap, the interleave, the
// spread — operates on score order and is worth keeping. Re-sorting the built
// page by ends_at would undo the diversification and hand back a tab showing
// five consecutive reels from whichever vendor has the most lots closing, which
// is exactly the failure PLAN-VIDEO-FIRST §3 calls out.
//
// So the surface says what "best" means and the existing machinery runs
// unchanged.
func (s Surface) overrideScore(it *Item, now time.Time) {
	switch s {
	case SurfaceEndingSoon:
		if it.TimeSignal == nil || it.TimeSignal.EndsAt.IsZero() {
			// Retrieval already excluded these. Scored to the floor rather than
			// dropped, because a candidate that reaches here without a signal is
			// a bug in the join and a silently shorter page is the worst way to
			// find out about it.
			it.Score = -1
			return
		}
		remaining := it.TimeSignal.EndsAt.Sub(now).Seconds()
		if remaining < 1 {
			remaining = 1
		}
		// Reciprocal rather than negated seconds: it compresses the difference
		// between "in five hours" and "in four", which nobody perceives, while
		// keeping the difference between "in two minutes" and "in twenty",
		// which is the entire point of the tab.
		it.Score = 1 / remaining
	case SurfaceNearby:
		if it.DistanceKM == nil {
			// A reel with no origin point. Scored neutral-low rather than
			// promoted or discarded — the same call the ranker makes for
			// unknown distance, because demoting content for a
			// data-collection failure makes a pipeline bug look like a
			// ranking decision.
			it.Score = 0
			return
		}
		it.Score = -*it.DistanceKM
	}
}

// reservesExploration reports whether this surface holds slots back for reels
// below the impression floor.
//
// For you does. The two ordered surfaces do not, and turning it off there is not
// a cost-cut — it is that exploration is a *ranking* device and these tabs are
// not ranked. Reserving a slot on Ending soon means placing a reel that closes
// in five hours above one closing in ten minutes, which breaks the only promise
// the tab makes.
//
// Nothing is lost. Exploration exists so a vendor with no engagement history can
// still be seen, and on a surface ordered purely by time-to-close or by distance
// a brand-new vendor already competes on equal terms — there is no score for
// their missing history to lose on.
func (s Surface) reservesExploration() bool {
	return s == SurfaceForYou || s == SurfaceLiveNow
}

// EmptyReason explains a page with nothing on it, per surface.
//
// Not cosmetic. "Nothing is live right now" is a true statement about a working
// product; the generic empty state reads as a broken one, and the difference
// decides whether the viewer tries the next tab or closes the app.
func (s Surface) EmptyReason() string {
	switch s {
	case SurfaceLiveNow:
		return "nothing_live"
	case SurfaceEndingSoon:
		return "nothing_closing"
	case SurfaceNearby:
		return "nothing_near"
	default:
		return "nothing_eligible"
	}
}

func (s Surface) String() string { return string(s) }

// Valid is used by tests and by the admin feed inspector, which lets staff
// reproduce a viewer's page on a named surface and must reject a typo rather
// than silently inspect For you.
func (s Surface) Valid() error {
	switch s {
	case SurfaceForYou, SurfaceLiveNow, SurfaceEndingSoon, SurfaceNearby:
		return nil
	default:
		return fmt.Errorf("feed: unknown surface %q", s)
	}
}
