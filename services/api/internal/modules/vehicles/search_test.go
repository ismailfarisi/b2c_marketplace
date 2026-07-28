package vehicles

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBuildBindsEveryFilterValue(t *testing.T) {
	// The one property that matters. Every client-controllable value must reach
	// Postgres as a bind parameter — nothing a buyer types may appear in the
	// query text. The sort is the only client input that selects SQL, and it
	// does so through a closed map, never by concatenation.
	f := Filter{
		MakeIDs:       []uuid.UUID{uuid.New()},
		Text:          "'; DROP TABLE vehicle_listings; --",
		SellerKind:    "dealer'; --",
		RegionalSpecs: []string{"gcc"},
		Sort:          Sort("price_asc; DROP TABLE users"),
		HasGeo:        true,
		Lat:           25.2, Lng: 55.3,
	}
	sql, args := f.Build()

	if strings.Contains(sql, "DROP TABLE") {
		t.Fatalf("client input reached the query text:\n%s", sql)
	}
	if len(args) == 0 {
		t.Fatal("expected bind parameters")
	}
	// An unrecognised sort falls back rather than being interpolated.
	if !strings.Contains(sql, orderBy[SortNewest]) {
		t.Fatal("an unknown sort must fall back to newest")
	}
}

func TestPlaceholdersAreDenseAndOrdered(t *testing.T) {
	// $1..$n with no gaps and no reuse. A hand-rolled builder that skips a
	// number produces a runtime error only when that particular filter
	// combination is used, which is the worst possible time to find out.
	f := Filter{
		MakeIDs:       []uuid.UUID{uuid.New()},
		ModelIDs:      []uuid.UUID{uuid.New()},
		YearMin:       2018,
		YearMax:       2023,
		PriceMinMinor: 5_000_000,
		PriceMaxMinor: 20_000_000,
		MileageMax:    150_000,
		RegionalSpecs: []string{"gcc", "japanese"},
		Transmissions: []string{"automatic"},
		FuelTypes:     []string{"petrol"},
		BodyTypes:     []string{"suv"},
		SellerKind:    "private",
		Text:          "patrol",
		HasGeo:        true,
		Lat:           25.2, Lng: 55.3, RadiusM: 40_000,
		Sort: SortDistance,
	}
	sql, args := f.Build()

	found := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(sql, -1)
	seen := map[string]bool{}
	max := 0
	for _, m := range found {
		seen[m[1]] = true
		n := 0
		for _, c := range m[1] {
			n = n*10 + int(c-'0')
		}
		if n > max {
			max = n
		}
	}
	if max != len(args) {
		t.Fatalf("highest placeholder $%d but %d args", max, len(args))
	}
	for i := 1; i <= max; i++ {
		if !seen[itoa(i)] {
			t.Fatalf("$%d never appears — the builder skipped a number", i)
		}
	}
}

func TestDistanceSortFallsBackWithoutAPoint(t *testing.T) {
	// Not an error the buyer made: the client just did not have location
	// permission yet. Refusing the search would show an empty screen at the
	// exact moment the user is most likely to leave.
	f := Filter{Sort: SortDistance, HasGeo: false}.Normalise()
	if f.Sort != SortNewest {
		t.Fatalf("got %s, want fallback to newest", f.Sort)
	}
}

func TestGeolessSearchIsStillBounded(t *testing.T) {
	// No radius predicate, but still LIMITed and still status-filtered. A
	// buyer hunting a rare trim nationwide is legitimate; an unbounded scan of
	// every row ever written is not.
	sql, _ := Filter{}.Build()
	if strings.Contains(sql, "ST_DWithin") {
		t.Fatal("no geo filter was requested")
	}
	if !strings.Contains(sql, "LIMIT") || !strings.Contains(sql, "l.status = 'active'") {
		t.Fatal("a geoless search must still be bounded and status-filtered")
	}
}

func TestInvertedRangesAreSwappedNotRejected(t *testing.T) {
	// A slider dragged past itself. Swapping returns what the user obviously
	// meant; rejecting returns a validation error for a gesture.
	f := Filter{YearMin: 2023, YearMax: 2018, PriceMinMinor: 900, PriceMaxMinor: 100}.Normalise()
	if f.YearMin != 2018 || f.YearMax != 2023 {
		t.Fatalf("years not swapped: %d–%d", f.YearMin, f.YearMax)
	}
	if f.PriceMinMinor != 100 || f.PriceMaxMinor != 900 {
		t.Fatalf("prices not swapped: %d–%d", f.PriceMinMinor, f.PriceMaxMinor)
	}
}

func TestLimitIsClamped(t *testing.T) {
	if got := (Filter{Limit: 5000}).Normalise().Limit; got != MaxLimit {
		t.Fatalf("got %d want %d", got, MaxLimit)
	}
	if got := (Filter{Limit: 0}).Normalise().Limit; got != DefaultLimit {
		t.Fatalf("got %d want %d", got, DefaultLimit)
	}
}

func TestPaidPlacementIsCapped(t *testing.T) {
	// A page where every result is featured is an advertisement. The cap is
	// what keeps the top slot worth buying — and it is applied in SQL, because
	// a Go-side cap would let page 2 show the same featured listing again.
	sql, _ := Filter{}.Build()
	if !strings.Contains(sql, "featured_rank") || !strings.Contains(sql, "row_number() OVER") {
		t.Fatal("featured placement must be capped by a window function, not in Go")
	}
	if FeaturedPerPage >= DefaultLimit/2 {
		t.Fatalf("FeaturedPerPage = %d is not a cap on a page of %d", FeaturedPerPage, DefaultLimit)
	}
}

func TestRestrictedListingsAreFilteredNotDemoted(t *testing.T) {
	// A demoted scam is still a scam that somebody scrolls to. Trust bands are
	// a hard filter for the same reason deliverability is (PLAN-VIDEO-FIRST
	// §10): eligibility is never a soft signal.
	sql, _ := Filter{}.Build()
	if !strings.Contains(sql, "trust_scores") {
		t.Fatal("search must exclude restricted and blocked listings")
	}
	idx := strings.Index(sql, "trust_scores")
	if idx > strings.Index(sql, "ORDER BY") {
		t.Fatal("the trust check belongs in WHERE, not in the ordering")
	}
}

func TestOrderingIsStable(t *testing.T) {
	// Every sort ends in l.id. Without it, two listings with the same price
	// swap places between page 1 and page 2 and one of them is never shown —
	// the same class of bug the Phase 4 feed cursor was rewritten to fix.
	for sort := range validSorts {
		sql, _ := Filter{Sort: sort, HasGeo: true, Lat: 25, Lng: 55}.Build()
		tail := sql[strings.LastIndex(sql, "ORDER BY"):]
		if !strings.Contains(tail, "l.id") {
			t.Fatalf("sort %s has no stable tiebreak:\n%s", sort, tail)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
