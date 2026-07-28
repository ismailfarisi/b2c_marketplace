package boosts

import (
	"testing"
	"time"
)

func local(hour, minute int) time.Time {
	loc := time.FixedZone("GST", 4*3600)
	return time.Date(2026, 8, 3, hour, minute, 0, 0, loc)
}

// ── pricing ──────────────────────────────────────────────────────────────────

// The bug this fixed point exists to prevent: a 12 AED CPM is 1.2 fils per
// impression. Rounded to an integer, the platform is either discounting every
// campaign by 17% or overcharging every advertiser by 67%.
func TestAThousandImpressionsCostExactlyTheCPM(t *testing.T) {
	for _, cpm := range []int{1000, 1200, 1500, 999, 1} {
		var total int64
		for i := 0; i < 1000; i++ {
			total += PriceMicros(cpm)
		}
		if got := MinorFromMicros(total); got != int64(cpm) {
			t.Errorf("1000 impressions at a %d CPM cost %d, want %d", cpm, got, cpm)
		}
	}
}

func TestAMillionImpressionsDoNotDrift(t *testing.T) {
	const cpm = 1200
	var total int64
	for i := 0; i < 1_000_000; i++ {
		total += PriceMicros(cpm)
	}
	if got, want := MinorFromMicros(total), int64(cpm*1000); got != want {
		t.Fatalf("a million impressions cost %d, want %d — drift of %d fils",
			got, want, got-want)
	}
}

func TestMicroConversionRoundsHalfUpAndIsSymmetric(t *testing.T) {
	if got := MinorFromMicros(MicrosPerMinor / 2); got != 1 {
		t.Errorf("half a fils rounds to %d, want 1", got)
	}
	if got := MinorFromMicros(-MicrosPerMinor / 2); got != -1 {
		t.Errorf("negative half rounds to %d, want -1 — a refund must be the exact "+
			"negative of its charge", got)
	}
	if got := MinorFromMicros(0); got != 0 {
		t.Errorf("got %d", got)
	}
}

// ── slots ────────────────────────────────────────────────────────────────────

// The first reel is the answer to "is this feed worth staying in". It is what
// every session sees and what an advertiser would pay most for, which is exactly
// why it is not for sale.
func TestTheFirstReelIsNeverSponsored(t *testing.T) {
	for size := 1; size <= 50; size++ {
		for _, pos := range SponsoredSlots(size) {
			if pos < FirstSponsoredPosition {
				t.Fatalf("page of %d sells position %d", size, pos)
			}
		}
	}
}

func TestSponsoredShareIsCapped(t *testing.T) {
	for size := 1; size <= 50; size++ {
		slots := SponsoredSlots(size)
		if len(slots)*MaxSponsoredShare > size {
			t.Fatalf("page of %d has %d sponsored slots — over one in %d",
				size, len(slots), MaxSponsoredShare)
		}
	}
	// The shipping page size, stated explicitly so a change to it is visible in
	// a diff rather than emergent.
	if got := len(SponsoredSlots(20)); got != 3 {
		t.Fatalf("a 20-item page has %d sponsored slots, want 3", got)
	}
}

func TestSponsoredSlotsAreNeverAdjacent(t *testing.T) {
	for size := 1; size <= 50; size++ {
		slots := SponsoredSlots(size)
		for i := 1; i < len(slots); i++ {
			if gap := slots[i] - slots[i-1]; gap < MinGapBetweenSponsored {
				t.Fatalf("page of %d puts sponsored slots %d apart — two in a row "+
					"reads as an ad break", size, gap)
			}
		}
	}
}

func TestASmallPageSellsLittleOrNothing(t *testing.T) {
	if got := SponsoredSlots(5); len(got) != 0 {
		t.Fatalf("a 5-item page sells %v — one in six of five is none", got)
	}
	if got := SponsoredSlots(2); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

// ── the auction ──────────────────────────────────────────────────────────────

func bid(id string, cpm int, quality float64) Bid {
	return Bid{CampaignID: id, ReelID: id, AuthorKey: "author-" + id,
		BidCPMinor: cpm, Quality: quality, PriceMicros: PriceMicros(cpm)}
}

// A pure highest-bid auction fills the feed with the worst content from the
// richest advertiser. That is the default outcome, not an edge case — the
// advertisers most willing to overpay are the ones whose content nobody watches
// voluntarily.
func TestMoneyDoesNotBeatRelevanceOutright(t *testing.T) {
	rich := bid("rich", 5000, 0.2) // pays four times as much
	good := bid("good", 1200, 0.9) // four and a half times the engagement

	winners := Auction([]Bid{rich, good}, 1)
	if len(winners) != 1 || winners[0].CampaignID != "good" {
		t.Fatalf("winner is %+v — a feed that sells on price alone gets worse every "+
			"time it makes more money", winners)
	}
}

func TestBelowTheQualityFloorNoPriceWins(t *testing.T) {
	junk := bid("junk", 100_000, QualityFloor-0.01)
	winners := Auction([]Bid{junk}, 3)
	if len(winners) != 0 {
		t.Fatalf("a bid below the quality floor won at %d CPM", junk.BidCPMinor)
	}
}

func TestOneCampaignAndOneAuthorPerPage(t *testing.T) {
	same := []Bid{
		bid("a", 2000, 0.9),
		bid("a", 2000, 0.9),
		{CampaignID: "b", ReelID: "b", AuthorKey: "author-a", BidCPMinor: 1900, Quality: 0.9},
		bid("c", 1000, 0.5),
	}
	winners := Auction(same, 3)

	seenCampaign := map[string]bool{}
	seenAuthor := map[string]bool{}
	for _, w := range winners {
		if seenCampaign[w.CampaignID] {
			t.Fatalf("campaign %s won twice", w.CampaignID)
		}
		if seenAuthor[w.AuthorKey] {
			t.Fatalf("author %s won twice — three sponsored reels from one advertiser "+
				"in one scroll is one advertiser three times", w.AuthorKey)
		}
		seenCampaign[w.CampaignID] = true
		seenAuthor[w.AuthorKey] = true
	}
	if len(winners) != 2 {
		t.Fatalf("got %d winners, want 2 (campaigns a and c)", len(winners))
	}
}

func TestAuctionFillsAtMostTheSlotsAvailable(t *testing.T) {
	bids := []Bid{bid("a", 2000, 0.9), bid("b", 1900, 0.8), bid("c", 1800, 0.7)}
	if got := len(Auction(bids, 2)); got != 2 {
		t.Fatalf("got %d winners for 2 slots", got)
	}
	if got := len(Auction(bids, 0)); got != 0 {
		t.Fatalf("got %d winners for 0 slots", got)
	}
	if got := len(Auction(nil, 3)); got != 0 {
		t.Fatalf("got %d winners with no bidders", got)
	}
}

func TestWinnersAreOrderedByWhatTheyAreWorth(t *testing.T) {
	bids := []Bid{bid("low", 1000, 0.3), bid("high", 1000, 0.9), bid("mid", 1000, 0.6)}
	winners := Auction(bids, 3)
	want := []string{"high", "mid", "low"}
	for i, w := range want {
		if winners[i].CampaignID != w {
			t.Fatalf("position %d is %s, want %s", i, winners[i].CampaignID, w)
		}
	}
}

// ── pacing ───────────────────────────────────────────────────────────────────

// The failure every ad platform ships first: a campaign spends its whole daily
// budget between 08:00 and 09:00, and the advertiser sees a day's spend against
// a morning's reach.
func TestADailyBudgetCannotBeSpentInTheMorning(t *testing.T) {
	const cap = 100 * MicrosPerMinor

	morning := PacedAllowance(cap, local(9, 0))
	if morning >= cap {
		t.Fatal("the whole day's budget is available at 09:00")
	}
	if morning > cap*45/100 {
		t.Errorf("%d%% of the budget is available at 09:00", morning*100/cap)
	}

	// And it must all be available by the end of the day, or the campaign
	// under-delivers every single day and the advertiser is refunded budget
	// they wanted spent.
	if got := PacedAllowance(cap, local(23, 59)); got != cap {
		t.Errorf("only %d of %d is available at 23:59", got, cap)
	}
}

func TestPacingIsMonotonic(t *testing.T) {
	const cap = 100 * MicrosPerMinor
	last := int64(-1)
	for h := 0; h < 24; h++ {
		for m := 0; m < 60; m += 5 {
			got := PacedAllowance(cap, local(h, m))
			if got < last {
				t.Fatalf("allowance fell at %02d:%02d", h, m)
			}
			if got > cap {
				t.Fatalf("allowance exceeded the cap at %02d:%02d", h, m)
			}
			last = got
		}
	}
}

// A campaign created at 00:01 with strictly linear pacing can spend nothing,
// which makes the first serve of every day fail for no reason a person would
// recognise.
func TestSomethingIsSpendableAtMidnight(t *testing.T) {
	const cap = 100 * MicrosPerMinor
	if got := PacedAllowance(cap, local(0, 1)); got <= 0 {
		t.Fatal("nothing is spendable at 00:01")
	}
}

// Reservations are the difference between this and a spend counter. Impressions
// are charged seconds after they are served, and a campaign one impression from
// its cap can be served five hundred times in that gap.
func TestReservationsCountAgainstTheCap(t *testing.T) {
	const cap = 100 * MicrosPerMinor
	at := local(23, 59)

	price := PriceMicros(1200)
	spent := cap - price

	if !CanServe(cap, spent, 0, price, at) {
		t.Fatal("the last impression must be servable")
	}
	if CanServe(cap, spent, price, price, at) {
		t.Fatal("an impression already reserved must block the next one — without " +
			"this the cap is advisory")
	}
}

func TestHeadroomNeverGoesNegative(t *testing.T) {
	const cap = 100 * MicrosPerMinor
	if got := Headroom(cap, cap*2, cap, local(12, 0)); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

// ── campaign validation ──────────────────────────────────────────────────────

func TestABidBelowTheFloorIsRejected(t *testing.T) {
	c := Campaign{BidCPMinor: 900, FundedMinor: 20000, DailyCapMinor: 5000,
		StartsOn: local(0, 0)}
	if err := c.Validate(1200); err == nil {
		t.Fatal("a bid below the published floor must be rejected")
	}
	c.BidCPMinor = 1200
	if err := c.Validate(1200); err != nil {
		t.Fatalf("a bid at the floor must be accepted: %v", err)
	}
}

// A campaign funded with 200 AED and a daily cap of 500 is not a campaign, it
// is one day of spending with a misleading label. The advertiser meant "no
// daily limit"; rejecting it makes them say so.
func TestADailyCapAboveTheFundingIsRejected(t *testing.T) {
	c := Campaign{BidCPMinor: 1200, FundedMinor: 20000, DailyCapMinor: 50000,
		StartsOn: local(0, 0)}
	if err := c.Validate(1200); err == nil {
		t.Fatal("a daily cap above total funding must be rejected")
	}
}

// A cap that cannot afford one impression never serves and never errors: the
// campaign sits active, spends nothing, and the advertiser waits for reach that
// is arithmetically impossible.
func TestACapTooSmallForOneImpressionIsRejected(t *testing.T) {
	c := Campaign{BidCPMinor: 1200, FundedMinor: 20000, DailyCapMinor: 1,
		StartsOn: local(0, 0)}
	if err := c.Validate(1200); err == nil {
		t.Fatal("a one-fils daily cap at a 12 AED CPM must be rejected")
	}
	// Two fils affords a 1.2-fils impression.
	c.DailyCapMinor = 2
	if err := c.Validate(1200); err != nil {
		t.Fatalf("a cap that affords an impression must be accepted: %v", err)
	}
}

func TestUnfundedCampaignsAreRejected(t *testing.T) {
	c := Campaign{BidCPMinor: 1200, FundedMinor: 0, DailyCapMinor: 5000, StartsOn: local(0, 0)}
	if err := c.Validate(1200); err == nil {
		t.Fatal("prepaid means prepaid — there is no credit line in this system")
	}
}

func TestACampaignCannotEndBeforeItStarts(t *testing.T) {
	c := Campaign{BidCPMinor: 1200, FundedMinor: 20000, DailyCapMinor: 5000,
		StartsOn: local(0, 0).AddDate(0, 0, 5), EndsOn: local(0, 0)}
	if err := c.Validate(1200); err == nil {
		t.Fatal("expected a date error")
	}
}
