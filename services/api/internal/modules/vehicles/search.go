package vehicles

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Search is Postgres, not OpenSearch, and that is a decision rather than a
// shortcut.
//
// Phase 4 put OpenSearch in place for reels, where the candidate set is a geo
// circle over hundreds of thousands of rows scored by a dozen signals. Car
// search is the opposite shape: a handful of exact facets (make, model, year,
// price, spec) over an index that answers them directly, and a result the buyer
// expects to be *complete* — a buyer filtering for a 2019 Patrol under 120k who
// is shown "the best matches" instead of all of them will not believe the site.
//
// The facet index in migration 000016 covers this. It moves to OpenSearch when
// free-text over descriptions matters more than exactness, which is a later
// problem than it looks.
type Sort string

const (
	SortNewest     Sort = "newest"
	SortPriceAsc   Sort = "price_asc"
	SortPriceDesc  Sort = "price_desc"
	SortMileageAsc Sort = "mileage_asc"
	SortYearDesc   Sort = "year_desc"
	SortDistance   Sort = "distance"
)

const (
	DefaultLimit  = 24
	MaxLimit      = 60
	MaxSearchRadi = 300_000
	// FeaturedPerPage caps how much of a results page can be paid placement.
	//
	// Two. A page where every result is featured is an advertisement, and the
	// seller who paid for the top slot got nothing because the buyer stopped
	// trusting the order. Capping paid placement is what keeps it worth buying.
	FeaturedPerPage = 2
)

type Filter struct {
	MakeIDs       []uuid.UUID
	ModelIDs      []uuid.UUID
	YearMin       int
	YearMax       int
	PriceMinMinor int64
	PriceMaxMinor int64
	MileageMax    int

	RegionalSpecs []string
	Transmissions []string
	FuelTypes     []string
	BodyTypes     []string
	SellerKind    string

	// Geo. Optional: a buyer looking for a specific rare trim will happily drive
	// to Fujairah, and forcing a radius on them returns nothing.
	Lat, Lng float64
	HasGeo   bool
	RadiusM  int

	// Text matches title and description via pg_trgm (extension enabled in
	// 000001). Not a replacement for the facets — it is the escape hatch for
	// "GTR" and "شبح" and the things nobody modelled.
	Text string

	Sort   Sort
	Limit  int
	Offset int
}

// Normalise clamps everything a client can get wrong.
//
// Separated from Build so the clamping is testable without reading SQL, and so
// Build has no branches that depend on unvalidated input.
func (f Filter) Normalise() Filter {
	if f.Limit <= 0 {
		f.Limit = DefaultLimit
	}
	if f.Limit > MaxLimit {
		f.Limit = MaxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	if f.RadiusM <= 0 {
		f.RadiusM = DefaultRadiusM
	}
	if f.RadiusM > MaxSearchRadi {
		f.RadiusM = MaxSearchRadi
	}
	if f.YearMin > 0 && f.YearMax > 0 && f.YearMin > f.YearMax {
		f.YearMin, f.YearMax = f.YearMax, f.YearMin
	}
	if f.PriceMinMinor > 0 && f.PriceMaxMinor > 0 && f.PriceMinMinor > f.PriceMaxMinor {
		f.PriceMinMinor, f.PriceMaxMinor = f.PriceMaxMinor, f.PriceMinMinor
	}
	// Sorting by distance without a point is not an error the buyer made — the
	// client just did not have location permission yet. Fall back rather than
	// refuse.
	if f.Sort == SortDistance && !f.HasGeo {
		f.Sort = SortNewest
	}
	if !validSorts[f.Sort] {
		f.Sort = SortNewest
	}
	f.Text = strings.TrimSpace(f.Text)
	if len(f.Text) > 120 {
		f.Text = f.Text[:120]
	}
	return f
}

var validSorts = map[Sort]bool{
	SortNewest: true, SortPriceAsc: true, SortPriceDesc: true,
	SortMileageAsc: true, SortYearDesc: true, SortDistance: true,
}

// orderBy maps a sort to SQL.
//
// A map rather than string concatenation from the request, so there is no path
// by which a client-supplied value reaches the query text. Every other filter
// value travels as a bind parameter for the same reason.
var orderBy = map[Sort]string{
	// Newest means "most recently surfaced", which includes bumps — that is
	// what a seller buys when they buy a bump, and hiding it in a different sort
	// nobody selects would make the product not work.
	SortNewest:     "GREATEST(l.published_at, l.bumped_at) DESC NULLS LAST",
	SortPriceAsc:   "l.price_minor ASC",
	SortPriceDesc:  "l.price_minor DESC",
	SortMileageAsc: "l.mileage_km ASC",
	SortYearDesc:   "l.year DESC, l.mileage_km ASC",
	SortDistance:   "distance_m ASC NULLS LAST",
}

// Build returns the search query and its arguments.
//
// Two ordering keys, in this order: featured-within-cap, then the buyer's sort.
// The cap is applied in SQL with a window function rather than in Go, because
// paginating a Go-side cap means page 2 can show the same featured listing
// again — the exact bug the Phase 4 feed cursor was rewritten to avoid.
func (f Filter) Build() (string, []any) {
	f = f.Normalise()

	var (
		args  []any
		where []string
	)
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	where = append(where, "l.status = 'active'")

	if len(f.MakeIDs) > 0 {
		where = append(where, "l.make_id = ANY("+arg(f.MakeIDs)+")")
	}
	if len(f.ModelIDs) > 0 {
		where = append(where, "l.model_id = ANY("+arg(f.ModelIDs)+")")
	}
	if f.YearMin > 0 {
		where = append(where, "l.year >= "+arg(f.YearMin))
	}
	if f.YearMax > 0 {
		where = append(where, "l.year <= "+arg(f.YearMax))
	}
	if f.PriceMinMinor > 0 {
		where = append(where, "l.price_minor >= "+arg(f.PriceMinMinor))
	}
	if f.PriceMaxMinor > 0 {
		where = append(where, "l.price_minor <= "+arg(f.PriceMaxMinor))
	}
	if f.MileageMax > 0 {
		where = append(where, "l.mileage_km <= "+arg(f.MileageMax))
	}
	if len(f.RegionalSpecs) > 0 {
		where = append(where, "l.regional_spec = ANY("+arg(f.RegionalSpecs)+")")
	}
	if len(f.Transmissions) > 0 {
		where = append(where, "l.transmission = ANY("+arg(f.Transmissions)+")")
	}
	if len(f.FuelTypes) > 0 {
		where = append(where, "l.fuel_type = ANY("+arg(f.FuelTypes)+")")
	}
	if len(f.BodyTypes) > 0 {
		where = append(where, "l.body_type = ANY("+arg(f.BodyTypes)+")")
	}
	if f.SellerKind != "" {
		where = append(where, "l.seller_kind = "+arg(f.SellerKind))
	}
	if f.Text != "" {
		// Trigram similarity rather than to_tsquery: this market types brand
		// names in two scripts and three spellings, and a stemmer for neither
		// language helps with "パジェロ" or "بترول".
		//
		// The make and model are matched as well as the seller's own words, and
		// that is a fix rather than a nicety. Title and description are both
		// optional and the app's sell flow sets neither — the overlay's words
		// come from Summarize, so there is nothing to ask the seller for — which
		// meant every listing created from a phone was invisible to search.
		// Nobody would have noticed until a seller asked why their car cannot be
		// found by its own name.
		//
		// It is also more correct for the listings that *do* have a title: a car
		// headlined "Immaculate family SUV, first owner" should still come back
		// for "nissan".
		t := arg(f.Text)
		where = append(where, "("+
			"l.title % "+t+
			" OR l.description ILIKE '%' || "+t+" || '%'"+
			" OR mk.name ILIKE '%' || "+t+" || '%'"+
			" OR md.name ILIKE '%' || "+t+" || '%'"+
			" OR (mk.name || ' ' || md.name) % "+t+
			// The year, typed as part of the phrase people actually use —
			// "2021 patrol" is one query, not a filter plus a query.
			" OR l.year::text = "+t+
			")")
	}

	distance := "NULL::float8"
	if f.HasGeo {
		lat, lng := arg(f.Lat), arg(f.Lng)
		point := fmt.Sprintf("ST_SetSRID(ST_MakePoint(%s, %s), 4326)::geography", lng, lat)
		where = append(where, fmt.Sprintf("ST_DWithin(l.point, %s, %s)", point, arg(f.RadiusM)))
		distance = fmt.Sprintf("ST_Distance(l.point, %s)", point)
	}

	// Trust gates the results, and it is a hard filter rather than a ranking
	// penalty. A listing under active fraud review is not "less relevant" — it
	// is a car that may not exist, and a demoted scam is still a scam that
	// somebody scrolls to.
	where = append(where,
		`NOT EXISTS (SELECT 1 FROM trust_scores ts
		              WHERE ts.subject_type = 'listing' AND ts.subject_id = l.id
		                AND ts.band IN ('restricted', 'blocked'))`)

	// The alias stays `l` through every CTE so the orderBy fragments above are
	// valid at each level. Renaming it per stage is how a window function ends
	// up ordered by a different column than the outer query and nobody notices,
	// because the result is still plausibly sorted.
	sql := fmt.Sprintf(`
		WITH matched AS (
			SELECT l.id, l.reference_no, l.seller_id, l.seller_kind, l.dealer_id,
			       l.make_id, l.model_id, l.trim_id, l.year, l.mileage_km,
			       l.transmission, l.fuel_type, l.body_type, l.regional_spec,
			       l.exterior_colour, l.price_minor, l.currency, l.is_negotiable,
			       -- Composed when the seller gave no headline, which is every
			       -- listing the app creates: the sell flow has no title field
			       -- because the overlay's words come from Summarize. A result
			       -- row still has to name the car, and "(untitled)" does not.
			       COALESCE(NULLIF(l.title, ''),
			                l.year || ' ' || mk.name || ' ' || md.name) AS title,
			       l.emirate, l.city, l.published_at, l.bumped_at,
			       (l.featured_until IS NOT NULL AND l.featured_until > now()) AS is_featured,
			       rl.id                       AS reel_id,
			       COALESCE(rl.poster_key, '') AS poster_key,
			       COALESCE(rl.lqip, '')       AS lqip,
			       %s AS distance_m
			  FROM vehicle_listings l
			  -- Inner joins, matching the schema: make_id and model_id are both
			  -- NOT NULL, so this filters nothing. It is here so the text
			  -- predicate can reach the taxonomy — see the note on f.Text.
			  JOIN vehicle_makes mk ON mk.id = l.make_id
			  JOIN vehicle_models md ON md.id = l.model_id
			  -- The card's picture: the newest *published* reel for this
			  -- listing.
			  --
			  -- LATERAL rather than a plain join, for the reason MyListings
			  -- gives: a relisted car legitimately has several reels, and a join
			  -- would return the listing once per reel — silently inflating both
			  -- the page and the count(*) OVER () total.
			  --
			  -- Restricted to published rather than taking the newest of any
			  -- state. A seller who is midway through replacing their video has
			  -- a fresher reel that is still transcoding, and preferring it would
			  -- blank the thumbnail of a listing that has a perfectly good older
			  -- one.
			  LEFT JOIN LATERAL (
			      SELECT id, poster_key, lqip FROM reels
			       WHERE subject_type = 'vehicle_listing' AND subject_id = l.id
			         AND state = 'published'
			       ORDER BY published_at DESC NULLS LAST
			       LIMIT 1
			  ) rl ON true
			 WHERE %s
		),
		ranked AS (
			SELECT l.*,
			       -- Position among featured rows only. Rows that are not
			       -- featured get a rank of 0 and never compete for the cap.
			       CASE WHEN l.is_featured
			            THEN row_number() OVER (PARTITION BY l.is_featured ORDER BY %s)
			            ELSE 0 END AS featured_rank
			  FROM matched l
		)
		SELECT l.id, l.reference_no, l.seller_id, l.seller_kind, l.dealer_id,
		       l.make_id, l.model_id, l.trim_id, l.year, l.mileage_km,
		       COALESCE(l.transmission, ''), COALESCE(l.fuel_type, ''),
		       COALESCE(l.body_type, ''), l.regional_spec,
		       COALESCE(l.exterior_colour, ''), l.price_minor, l.currency,
		       l.is_negotiable, COALESCE(l.title, ''),
		       COALESCE(l.emirate, ''), COALESCE(l.city, ''),
		       l.published_at, l.distance_m,
		       (l.is_featured AND l.featured_rank <= %d) AS promoted,
		       l.poster_key, l.lqip, l.reel_id,
		       count(*) OVER () AS total
		  FROM ranked l
		 ORDER BY (l.is_featured AND l.featured_rank <= %d) DESC, %s, l.id
		 LIMIT %s OFFSET %s`,
		distance,
		strings.Join(where, "\n			   AND "),
		orderBy[f.Sort],
		FeaturedPerPage,
		FeaturedPerPage,
		orderBy[f.Sort],
		arg(f.Limit), arg(f.Offset),
	)

	return sql, args
}
