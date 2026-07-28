// Package boosts sells reach.
//
// PLAN-VIDEO-FIRST §7 calls this "a natural, high-margin unlock that Framing A
// never had: you own an attention surface with hard geo targeting", and §9
// explains why it matters more than a third revenue line — the feed pays for
// egress on every view whether it converts or not, and this is the only stream
// that earns on the views themselves.
//
// Three rules constrain everything in this package, and each of them costs
// revenue. They are here because an ad system that maximises revenue in the
// first quarter and empties the feed by the third is not a business.
//
//  1. **A boosted reel passes the same hard filters as an organic one.** No
//     column, flag or parameter can switch that off. Selling reach into feeds
//     where the advertiser cannot deliver would be the most profitable thing
//     here and would destroy the one claim the product has against TikTok
//     (§10: "everything here is available to you right now").
//
//  2. **Paid slots never come out of the exploration slice.** §3 reserves
//     10–15% of the feed for reels that have not earned their place, because
//     without it new vendors get no impressions and the supply side churns.
//     Taking that reserve for advertisers converts the supply engine into a
//     rent, and the churn shows up two quarters later as an empty feed.
//
//  3. **Sponsored engagement does not feed the ranker.** A boosted reel
//     accumulates views it did not earn. Letting those into author quality or
//     the reel's own features means money buys organic rank — first as a bug,
//     then as a product feature nobody decided to build.
package boosts

import (
	"fmt"
	"time"
)

// MicrosPerMinor is the fixed-point scale for impression pricing.
//
// A 12 AED CPM is 1200 minor per thousand impressions — 1.2 minor each. Rounded
// down to an integer fils the platform has discounted every campaign by 17%;
// rounded up, every advertiser is overcharged by 67%. Neither is a rounding
// error. So impressions accumulate in millionths of a fils and the division
// happens exactly once, when a charge is posted.
const MicrosPerMinor = 1_000_000

// PriceMicros is what one impression costs, from a CPM bid.
func PriceMicros(bidCPMMinor int) int64 {
	// bid per 1000 impressions, in micros: bid × 1e6 / 1000.
	return int64(bidCPMMinor) * (MicrosPerMinor / 1000)
}

// MinorFromMicros converts an accumulated total to fils, rounding half up.
//
// One direction only. There is deliberately no MicrosFromMinor: converting back
// would let a rounded figure re-enter the accumulator, and the whole point of
// the fixed point is that it never does.
func MinorFromMicros(micros int64) int64 {
	if micros < 0 {
		return -MinorFromMicros(-micros)
	}
	return (micros + MicrosPerMinor/2) / MicrosPerMinor
}

// --- how much of the feed is for sale ---------------------------------------

const (
	// MaxSponsoredShare is the ceiling on paid slots in a page.
	//
	// One in six, so a 20-item page carries three. The number is a product
	// decision rather than a technical one and it is set low deliberately: the
	// feed's value to an advertiser is that people watch it, and the fastest
	// way to stop them is to sell a quarter of it. It can be raised; every
	// point of it should be argued for against retention rather than against
	// a revenue target.
	MaxSponsoredShare = 6

	// FirstSponsoredPosition is the earliest slot a paid reel may occupy.
	//
	// Never position 0. The first reel is the answer to "is this feed worth
	// staying in", it is the one every session sees, and it is the one an
	// advertiser would pay most for — which is exactly why it is not for sale.
	FirstSponsoredPosition = 2

	// MinGapBetweenSponsored keeps paid slots apart. Two adjacent sponsored
	// reels read as an ad break, and an ad break is the moment a scroll ends.
	MinGapBetweenSponsored = 4
)

// SponsoredSlots returns the positions in a page that may be sold, in order.
//
// Computed from the page size rather than fixed, so a client asking for five
// items gets at most one sponsored slot rather than the same three a twenty-item
// page gets.
func SponsoredSlots(pageSize int) []int {
	if pageSize <= FirstSponsoredPosition {
		return nil
	}
	max := pageSize / MaxSponsoredShare
	if max == 0 {
		return nil
	}

	slots := make([]int, 0, max)
	for pos := FirstSponsoredPosition; pos < pageSize && len(slots) < max; pos += MinGapBetweenSponsored {
		slots = append(slots, pos)
	}
	return slots
}

// --- the auction ------------------------------------------------------------

// Bid is one campaign competing for a slot.
type Bid struct {
	CampaignID string
	ReelID     string
	AuthorKey  string
	BidCPMinor int
	// Quality is the predicted engagement of this reel for this viewer,
	// normalised to roughly [0, 1] by the ranker.
	Quality float64
	// PriceMicros is what this impression will cost if confirmed. Carried on
	// the bid so a mid-flight bid change cannot reprice a served impression.
	PriceMicros int64
}

// QualityFloor is the predicted-engagement level below which a bid does not
// compete at any price.
//
// A pure highest-bid auction fills the feed with the worst content from the
// richest advertiser. That is not a hypothetical failure — it is what happens by
// default, because the advertisers most willing to overpay are the ones whose
// content nobody watches voluntarily. The floor is the cheapest possible defence
// and it costs the revenue of exactly the campaigns that would do the damage.
const QualityFloor = 0.15

// EffectiveCPM is what a bid is actually worth to the platform.
//
// bid × quality. The multiplication is the whole mechanism: an advertiser with
// watchable content pays less per impression than one without, which is the
// incentive the product needs to exist. Selling on price alone would make the
// feed worse every time it made more money.
func EffectiveCPM(b Bid) float64 {
	if b.Quality < QualityFloor {
		return 0
	}
	return float64(b.BidCPMinor) * b.Quality
}

// Auction picks winners for a set of slots.
//
// Second-price is deliberately *not* used. It is the right mechanism when
// bidders are sophisticated and repeated, and it is the wrong one at this scale:
// a restaurant owner buying 200 AED of reach cannot reason about truthful
// bidding, and "you bid 12 and paid 9" reads as a bug to them. First price with
// a published floor is explicable, which matters more here than the theoretical
// efficiency.
//
// One campaign per page, one author per page. A viewer who sees the same
// sponsored reel three times in one scroll has been shown one advertiser three
// times, and the reaction to that is not more clicks.
func Auction(bids []Bid, slots int) []Bid {
	if slots <= 0 || len(bids) == 0 {
		return nil
	}

	ranked := make([]Bid, 0, len(bids))
	for _, b := range bids {
		if EffectiveCPM(b) <= 0 {
			continue
		}
		ranked = append(ranked, b)
	}

	// Insertion sort by effective CPM, descending. The candidate set is a
	// handful of campaigns in a launch district; reaching for sort.Slice here
	// would allocate a closure per feed request to order six items.
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && EffectiveCPM(ranked[j]) > EffectiveCPM(ranked[j-1]); j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}

	winners := make([]Bid, 0, slots)
	seenCampaign := map[string]bool{}
	seenAuthor := map[string]bool{}
	for _, b := range ranked {
		if len(winners) == slots {
			break
		}
		if seenCampaign[b.CampaignID] || (b.AuthorKey != "" && seenAuthor[b.AuthorKey]) {
			continue
		}
		seenCampaign[b.CampaignID] = true
		seenAuthor[b.AuthorKey] = true
		winners = append(winners, b)
	}
	return winners
}

// --- pacing -----------------------------------------------------------------

// BurstMinutes is how much of the day's budget is available at midnight.
//
// Without it a campaign created at 00:01 with a linear pacing curve can spend
// nothing until 00:02, which makes the first serve of every day fail for no
// reason a person would recognise. An hour's worth is enough to start and small
// enough not to matter.
const BurstMinutes = 60

// PacedAllowance is how much of a daily cap may have been spent by now.
//
// Linear across the local day. Not because demand is flat — it is emphatically
// not, iftar is two hours — but because the alternative is a demand curve this
// system does not have the data to estimate, and a wrong curve paces worse than
// no curve. A campaign that wants iftar buys iftar through `hours_of_day`
// targeting, which is a statement of intent rather than a guess.
//
// The failure this prevents is the one every ad platform ships first: a
// campaign spends its entire daily budget between 08:00 and 09:00 against
// whoever happens to be scrolling, and the advertiser sees a day's spend and
// a morning's reach.
func PacedAllowance(capMicros int64, local time.Time) int64 {
	elapsed := int64(local.Hour()*60+local.Minute()) + BurstMinutes
	const dayMinutes = 24 * 60
	if elapsed >= dayMinutes {
		return capMicros
	}
	return capMicros * elapsed / dayMinutes
}

// Headroom is what a campaign may still commit right now.
//
// Reservations count against it, which is the difference between this and a
// spend counter. Impressions are charged on confirmation seconds after they are
// served, and without holding the gap a campaign one impression from its cap can
// be served five hundred times inside it.
func Headroom(capMicros, spentMicros, reservedMicros int64, local time.Time) int64 {
	allowed := PacedAllowance(capMicros, local)
	committed := spentMicros + reservedMicros
	if committed >= allowed {
		return 0
	}
	return allowed - committed
}

// CanServe reports whether one more impression fits.
func CanServe(capMicros, spentMicros, reservedMicros, priceMicros int64, local time.Time) bool {
	return Headroom(capMicros, spentMicros, reservedMicros, local) >= priceMicros
}

// ReservationTTL is how long a served-but-unconfirmed impression holds budget.
//
// Two minutes. A client that fetched a page and has not reported a view within
// that has almost certainly discarded it — a backgrounded app, a killed tab, a
// prefetch the user never scrolled to. Holding the budget longer starves the
// campaign; releasing it sooner double-serves.
const ReservationTTL = 2 * time.Minute

// --- validation -------------------------------------------------------------

// Campaign is what an advertiser submits.
type Campaign struct {
	BidCPMinor    int
	FundedMinor   int64
	DailyCapMinor int64
	StartsOn      time.Time
	EndsOn        time.Time
}

// Validate checks a campaign against the rate card floor and its own budget.
//
// The daily-cap-against-funding check is the one that matters. A campaign funded
// with 200 AED and a daily cap of 500 is not a campaign, it is a single day of
// spending with a misleading label — and the advertiser who set it did not mean
// that, they meant "no daily limit". Rejecting it makes them say which.
func (c Campaign) Validate(floorCPMinor int) error {
	if c.BidCPMinor < floorCPMinor {
		return fmt.Errorf("boosts: a bid of %d is below the %d floor for this placement",
			c.BidCPMinor, floorCPMinor)
	}
	if c.FundedMinor <= 0 {
		return fmt.Errorf("boosts: a campaign must be funded before it runs")
	}
	if c.DailyCapMinor <= 0 {
		return fmt.Errorf("boosts: a daily cap is required")
	}
	if c.DailyCapMinor > c.FundedMinor {
		return fmt.Errorf("boosts: a daily cap of %d against %d of funding is one day of "+
			"spending, not a daily cap", c.DailyCapMinor, c.FundedMinor)
	}
	if !c.EndsOn.IsZero() && c.EndsOn.Before(c.StartsOn) {
		return fmt.Errorf("boosts: the campaign ends before it starts")
	}
	// A daily cap below one impression's price never serves and never errors:
	// the campaign sits active, spends nothing, and the advertiser waits.
	if PriceMicros(c.BidCPMinor) > c.DailyCapMinor*MicrosPerMinor {
		return fmt.Errorf("boosts: a daily cap of %d cannot afford a single impression at "+
			"a %d CPM", c.DailyCapMinor, c.BidCPMinor)
	}
	return nil
}
