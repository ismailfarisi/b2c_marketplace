package promo

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/seam/servicetype"
)

func ptr[T any](v T) *T { return &v }

func TestDiscount(t *testing.T) {
	cases := []struct {
		name string
		p    Promo
		sub  int64
		want int64
	}{
		{"percent", Promo{DiscountType: TypePercent, PercentBP: 2000}, 100_000, 20_000},
		{"percent capped", Promo{DiscountType: TypePercent, PercentBP: 5000, MaxDiscountMinor: ptr[int64](10_000)}, 100_000, 10_000},
		{"fixed", Promo{DiscountType: TypeFixed, AmountMinor: 5_000}, 100_000, 5_000},
		{"fixed above subtotal", Promo{DiscountType: TypeFixed, AmountMinor: 50_000}, 30_000, 30_000},
		{"trial only", Promo{DiscountType: TypeNone}, 30_000, 0},
		{"full", Promo{DiscountType: TypePercent, PercentBP: 10000}, 30_000, 30_000},
	}
	for _, c := range cases {
		if got := c.p.Discount(c.sub); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}

func TestPlatformFunded(t *testing.T) {
	if got := (Promo{KitchenShareBP: 0}).PlatformFunded(10_000); got != 10_000 {
		t.Fatalf("platform pays all: got %d", got)
	}
	if got := (Promo{KitchenShareBP: 5000}).PlatformFunded(10_001); got != 5_001 {
		t.Fatalf("half split: got %d", got)
	}
	if got := (Promo{KitchenShareBP: 10000}).PlatformFunded(10_000); got != 0 {
		t.Fatalf("kitchen pays all: got %d", got)
	}
}

func TestTrialPrice(t *testing.T) {
	if got := TrialPrice(100_000, 7, 28); got != 25_000 {
		t.Fatalf("7 of 28: got %d", got)
	}
	if got := TrialPrice(100_000, 3, 28); got != 10_714 {
		t.Fatalf("3 of 28 rounds half up: got %d", got)
	}
	if got := TrialPrice(100_000, 30, 28); got != 100_000 {
		t.Fatalf("trial longer than cycle keeps the price: got %d", got)
	}
}

func TestApplyToQuote(t *testing.T) {
	q := servicetype.QuoteResult{Currency: "AED", SubtotalMinor: 105_000, DeliveryMinor: 1_200,
		TaxMinor: 5_000 + 57, TotalMinor: 106_200, AuthorizeMinor: 106_200}
	ApplyToQuote(&q, 21_000, 500)
	if q.TotalMinor != 85_200 || q.AuthorizeMinor != 85_200 || q.DiscountMinor != 21_000 {
		t.Fatalf("totals: %+v", q)
	}
	if q.TaxMinor != 4_000+57 {
		t.Fatalf("item VAT should be re-extracted from the discounted amount: got %d", q.TaxMinor)
	}
}

func TestSpreadSumsToWhole(t *testing.T) {
	got := Spread(10_001, []int64{30_000, 20_000, 25_000, 25_000})
	var sum int64
	for _, v := range got {
		sum += v
	}
	if sum != 10_001 || got[0] != 3_000 || got[1] != 2_000 {
		t.Fatalf("got %v", got)
	}
}

func TestLiveAt(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	p := Promo{IsActive: true, StartsAt: ptr(now.Add(time.Hour))}
	if p.LiveAt(now) {
		t.Fatal("not started")
	}
	p = Promo{IsActive: true, EndsAt: ptr(now)}
	if p.LiveAt(now) {
		t.Fatal("ended at its end time")
	}
	p = Promo{IsActive: true, UsageLimit: ptr(2), UsageCount: 2}
	if p.LiveAt(now) {
		t.Fatal("exhausted")
	}
	if !(Promo{IsActive: true}).LiveAt(now) {
		t.Fatal("open promo should be live")
	}
}

func TestCoversAll(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	if !(Promo{}).CoversAll([]uuid.UUID{a, b}) {
		t.Fatal("no kitchens listed means all kitchens")
	}
	if (Promo{BranchIDs: []uuid.UUID{a}}).CoversAll([]uuid.UUID{a, b}) {
		t.Fatal("every rotation kitchen must be covered")
	}
}

func TestNormalize(t *testing.T) {
	in, err := Normalize(Input{Code: ptr(" try7 "), Name: "Try a week", DiscountType: TypeNone,
		TrialDays: ptr(7), IsActive: true})
	if err != nil || *in.Code != "TRY7" || in.AppliesTo != ForSubscriptions || in.Audience != AudienceEveryone {
		t.Fatalf("got %+v, %v", in, err)
	}
	bad := []Input{
		{Name: "No code, not public", DiscountType: TypeFixed, AmountMinor: 100},
		{Code: ptr("X"), Name: "Short code", DiscountType: TypeFixed, AmountMinor: 100},
		{Code: ptr("NONE1"), Name: "None without trial", DiscountType: TypeNone},
		{Code: ptr("ROT1"), Name: "Trial rotation", DiscountType: TypeNone, TrialDays: ptr(3), AppliesTo: ForRotations},
		{Code: ptr("PCT1"), Name: "Zero percent", DiscountType: TypePercent},
		{Code: ptr("LONG1"), Name: "Long trial", DiscountType: TypeNone, TrialDays: ptr(30)},
		{Code: ptr("SHARE1"), Name: "Share", DiscountType: TypeFixed, AmountMinor: 1, KitchenShareBP: 12000},
	}
	for _, b := range bad {
		if _, err := Normalize(b); err == nil {
			t.Errorf("%s: expected a validation error", b.Name)
		}
	}
}

func TestRandomCode(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		c, err := randomCode(8)
		if err != nil {
			t.Fatal(err)
		}
		if len(c) != 8 || strings.Trim(c, codeAlphabet) != "" {
			t.Fatalf("code %q not 8 chars from the alphabet", c)
		}
		seen[c] = true
	}
	if len(seen) < 190 {
		t.Fatalf("only %d distinct codes in 200", len(seen))
	}
}

func TestProgramInputValidate(t *testing.T) {
	ok := ProgramInput{FriendAmountMinor: 5000, RewardAmountMinor: 5000, MonthlyCap: 5, RewardValidDays: 90}
	if err := ok.validate(); err != nil || ok.Currency != "AED" {
		t.Fatalf("valid input rejected: %v (currency %q)", err, ok.Currency)
	}
	for _, bad := range []ProgramInput{
		{FriendAmountMinor: 0, RewardAmountMinor: 5000, MonthlyCap: 5, RewardValidDays: 90},
		{FriendAmountMinor: 5000, RewardAmountMinor: 0, MonthlyCap: 5, RewardValidDays: 90},
		{FriendAmountMinor: 5000, RewardAmountMinor: 5000, MonthlyCap: 0, RewardValidDays: 90},
		{FriendAmountMinor: 5000, RewardAmountMinor: 5000, MonthlyCap: 5, RewardValidDays: 3},
	} {
		if err := bad.validate(); err == nil {
			t.Fatalf("invalid input accepted: %+v", bad)
		}
	}
}
