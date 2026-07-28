package boosts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct {
	pool *pgxpool.Pool
	loc  *time.Location
}

func NewService(pool *pgxpool.Pool, loc *time.Location) *Service {
	if loc == nil {
		loc = time.FixedZone("GST", 4*3600)
	}
	return &Service{pool: pool, loc: loc}
}

// Placement is one slot the feed wants filled.
type Placement struct {
	Lat, Lng   float64
	ViewerKey  string
	ViewerID   uuid.NullUUID
	CategoryID *uuid.UUID
	Locale     string
	// ExcludeReels are reels already in the organic page. A sponsored slot that
	// repeats one of them wastes the slot and looks like a bug to the viewer.
	ExcludeReels []uuid.UUID
	Slots        int
}

// Fill runs the auction and reserves budget for the winners.
//
// The query below is the load-bearing part of this package and the geo predicate
// is why. `ST_Intersects(r.coverage, viewer)` is the *same* test the organic
// feed makes, against the same column: a boosted reel is eligible only where its
// author actually serves, and the campaign's own targeting can narrow that and
// never widen it.
//
// There is no parameter, flag or admin override that relaxes it. That is
// deliberate — the moment one exists, someone sells a national campaign to a
// vendor with one branch in Al Quoz.
func (s *Service) Fill(ctx context.Context, p Placement) ([]Delivery, error) {
	if p.Slots <= 0 {
		return nil, nil
	}

	now := time.Now()
	localNow := now.In(s.loc)
	today := localNow.Format("2006-01-02")

	rows, err := s.pool.Query(ctx, `
		WITH live AS (
		    SELECT c.id, c.reel_id, c.bid_cpm_minor, c.advertiser_type, c.advertiser_id,
		           c.daily_cap_minor,
		           r.author_type, r.author_id
		      FROM boost_campaigns c
		      JOIN reels r ON r.id = c.reel_id
		      LEFT JOIN boost_targeting t ON t.campaign_id = c.id
		     WHERE c.status = 'active'
		       AND c.starts_on <= $6::date
		       AND (c.ends_on IS NULL OR c.ends_on >= $6::date)
		       AND c.spent_minor < c.funded_minor
		       -- The reel must be publishable on its own terms. A campaign
		       -- cannot buy its way past moderation.
		       AND r.state = 'published'
		       AND r.removed_at IS NULL
		       AND NOT (r.id = ANY($5::uuid[]))
		       -- The same predicate the organic feed applies, against the same
		       -- column. Not a reimplementation of the rule: reels.coverage is
		       -- resolved once at publish time from the author's service area,
		       -- and this is byte-for-byte the test feed.Service.retrieve
		       -- makes.
		       AND ST_Intersects(r.coverage, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography)
		       -- Targeting narrows only.
		       AND (t.centre IS NULL OR ST_DWithin(
		               t.centre, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography, t.radius_m))
		       AND (t.category_ids IS NULL OR $3::uuid IS NULL OR $3::uuid = ANY(t.category_ids))
		       AND (t.locales IS NULL OR $4 = ANY(t.locales))
		       AND (t.hours_of_day IS NULL OR $7::int = ANY(t.hours_of_day))
		       AND (t.days_of_week IS NULL OR $8::int = ANY(t.days_of_week))
		)
		SELECT live.id, live.reel_id, live.bid_cpm_minor,
		       live.author_type || ':' || live.author_id,
		       live.daily_cap_minor,
		       coalesce(d.spent_micros, 0), coalesce(d.reserved_micros, 0),
		       -- Quality is the author's organic view-through rate, computed
		       -- here rather than stored. A new author with no history gets
		       -- 0.5 — good enough to compete, not good enough to win against a
		       -- proven one — because the alternative is that a first campaign
		       -- either never serves or always wins, and both are wrong.
		       CASE WHEN coalesce(aq.impressions, 0) < 100 THEN 0.5
		            ELSE least(1.0, aq.views::float8 / aq.impressions)
		       END
		  FROM live
		  LEFT JOIN boost_daily_spend d ON d.campaign_id = live.id AND d.day = $6::date
		  LEFT JOIN author_quality aq
		         ON aq.author_type = live.author_type AND aq.author_id = live.author_id
		 LIMIT 200`,
		p.Lat, p.Lng, nullUUIDArg(p.CategoryID), localeOr(p.Locale),
		excludeArg(p.ExcludeReels), today, localNow.Hour(), int(localNow.Weekday()))
	if err != nil {
		return nil, fmt.Errorf("boosts: candidate scan: %w", err)
	}
	defer rows.Close()

	var bids []Bid
	caps := map[string]dailyState{}

	for rows.Next() {
		var (
			campaignID, reelID          uuid.UUID
			bidCPM                      int
			authorKey                   string
			capMinor                    int64
			spentMicros, reservedMicros int64
			quality                     float64
		)
		if err := rows.Scan(&campaignID, &reelID, &bidCPM, &authorKey, &capMinor,
			&spentMicros, &reservedMicros, &quality); err != nil {
			return nil, err
		}

		price := PriceMicros(bidCPM)
		capMicros := capMinor * MicrosPerMinor
		if !CanServe(capMicros, spentMicros, reservedMicros, price, localNow) {
			// Paced out for now. Not an error and not logged: it is the pacing
			// working, and it happens thousands of times a day.
			continue
		}

		bids = append(bids, Bid{
			CampaignID:  campaignID.String(),
			ReelID:      reelID.String(),
			AuthorKey:   authorKey,
			BidCPMinor:  bidCPM,
			Quality:     quality,
			PriceMicros: price,
		})
		caps[campaignID.String()] = dailyState{capMicros: capMicros}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	winners := Auction(bids, p.Slots)
	if len(winners) == 0 {
		return nil, nil
	}

	// Reserve inside a transaction, and re-check the cap under the row lock.
	//
	// The scan above read spend without a lock, which is correct — locking every
	// candidate on every feed request would serialise the feed on the ad system.
	// The consequence is that two concurrent requests can both see room for the
	// last impression, and the re-check under `ON CONFLICT DO UPDATE` with the
	// CHECK constraint is what makes the loser fail rather than overspend.
	var out []Delivery
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		for i, w := range winners {
			campaignID := uuid.MustParse(w.CampaignID)
			reelID := uuid.MustParse(w.ReelID)

			reserved, err := s.reserve(ctx, tx, campaignID, today, caps[w.CampaignID].capMicros, w.PriceMicros)
			if err != nil {
				return err
			}
			if !reserved {
				continue
			}

			var deliveryID uuid.UUID
			err = tx.QueryRow(ctx, `
				INSERT INTO boost_deliveries
				    (campaign_id, reel_id, viewer_key, user_id, position, price_micros)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT DO NOTHING
				RETURNING id`,
				campaignID, reelID, p.ViewerKey, nullableUUID(p.ViewerID), i, w.PriceMicros).
				Scan(&deliveryID)
			if errors.Is(err, pgx.ErrNoRows) {
				// The dedupe index fired: this viewer already has this campaign
				// at this position today. A re-requested page, not a new
				// impression — release the reservation and move on.
				if err := s.release(ctx, tx, campaignID, today, w.PriceMicros); err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return fmt.Errorf("boosts: record delivery: %w", err)
			}

			out = append(out, Delivery{
				ID: deliveryID, CampaignID: campaignID, ReelID: reelID,
				PriceMicros: w.PriceMicros,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type dailyState struct{ capMicros int64 }

// Delivery is one sold slot.
type Delivery struct {
	ID          uuid.UUID
	CampaignID  uuid.UUID
	ReelID      uuid.UUID
	PriceMicros int64
}

func (s *Service) reserve(ctx context.Context, tx pgx.Tx, campaignID uuid.UUID, day string, capMicros, priceMicros int64) (bool, error) {
	_, err := tx.Exec(ctx, `
		INSERT INTO boost_daily_spend (campaign_id, day, served, reserved_micros, cap_micros)
		VALUES ($1, $2::date, 1, $3, $4)
		ON CONFLICT (campaign_id, day) DO UPDATE
		   SET served = boost_daily_spend.served + 1,
		       reserved_micros = boost_daily_spend.reserved_micros + $3,
		       updated_at = now()`,
		campaignID, day, priceMicros, capMicros)
	if err != nil {
		// The CHECK caught a race: two requests both saw room for the last
		// impression. Losing one impression is the correct outcome and is not
		// an error worth propagating to a feed request.
		if isCheckViolation(err, "boost_daily_spend_within_cap") {
			return false, nil
		}
		return false, fmt.Errorf("boosts: reserve: %w", err)
	}
	return true, nil
}

func (s *Service) release(ctx context.Context, tx pgx.Tx, campaignID uuid.UUID, day string, priceMicros int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE boost_daily_spend
		   SET reserved_micros = greatest(0, reserved_micros - $3),
		       served = greatest(0, served - 1), updated_at = now()
		 WHERE campaign_id = $1 AND day = $2::date`, campaignID, day, priceMicros)
	if err != nil {
		return fmt.Errorf("boosts: release reservation: %w", err)
	}
	return nil
}

// Confirm converts served impressions into spend.
//
// Called from the events pipeline when the client reports a view threshold was
// met. Charging here rather than at serve is what makes the numbers defensible:
// a page prefetched and abandoned bills nobody.
func (s *Service) Confirm(ctx context.Context, viewerKey string, reelIDs []uuid.UUID) (int, error) {
	if len(reelIDs) == 0 {
		return 0, nil
	}

	tag, err := s.pool.Exec(ctx, `
		WITH confirmed AS (
		    UPDATE boost_deliveries
		       SET state = 'confirmed', confirmed_at = now()
		     WHERE viewer_key = $1 AND reel_id = ANY($2)
		       AND state = 'served'
		       AND served_at > now() - interval '30 minutes'
		    RETURNING campaign_id, served_at, price_micros
		)
		UPDATE boost_daily_spend d
		   SET spent_micros = d.spent_micros + c.total,
		       reserved_micros = greatest(0, d.reserved_micros - c.total),
		       charged = d.charged + c.n,
		       updated_at = now()
		  FROM (SELECT campaign_id, (served_at AT TIME ZONE 'UTC')::date AS day,
		               sum(price_micros) AS total, count(*) AS n
		          FROM confirmed GROUP BY 1, 2) c
		 WHERE d.campaign_id = c.campaign_id AND d.day = c.day`,
		viewerKey, reelIDs)
	if err != nil {
		return 0, fmt.Errorf("boosts: confirm: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ExpireReservations releases budget held by impressions nobody confirmed.
//
// Without this a campaign leaks budget permanently every time a client fetches a
// page and closes the app: the reservation is never released, the daily cap
// fills with holds, and the campaign stops serving while showing spend of zero.
// That is the failure that looks like "the ad system is broken" and is actually
// arithmetic.
func (s *Service) ExpireReservations(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		WITH stale AS (
		    UPDATE boost_deliveries
		       SET state = 'expired'
		     WHERE state = 'served' AND served_at < now() - $1::interval
		    RETURNING campaign_id, served_at, price_micros
		)
		UPDATE boost_daily_spend d
		   SET reserved_micros = greatest(0, d.reserved_micros - s.total), updated_at = now()
		  FROM (SELECT campaign_id, (served_at AT TIME ZONE 'UTC')::date AS day,
		               sum(price_micros) AS total
		          FROM stale GROUP BY 1, 2) s
		 WHERE d.campaign_id = s.campaign_id AND d.day = s.day`,
		fmt.Sprintf("%d seconds", int(ReservationTTL.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("boosts: expire reservations: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// --- billing ----------------------------------------------------------------

// Bill rolls confirmed impressions into charges and posts them.
//
// Hourly. Posting a double-entry journal for 1.2 fils would produce a ledger
// nobody can read and a table two orders of magnitude larger than
// `ledger_entries` already is. The delivery rows are the evidence; this is the
// accounting.
func (s *Service) Bill(ctx context.Context, upTo time.Time) (int, error) {
	period := upTo.Truncate(time.Hour)
	start := period.Add(-time.Hour)

	rows, err := s.pool.Query(ctx, `
		SELECT campaign_id, count(*), sum(price_micros), currency
		  FROM boost_deliveries
		 WHERE state = 'confirmed' AND charge_id IS NULL
		   AND served_at >= $1 AND served_at < $2
		 GROUP BY campaign_id, currency`, start, period)
	if err != nil {
		return 0, fmt.Errorf("boosts: bill scan: %w", err)
	}

	type group struct {
		campaignID  uuid.UUID
		impressions int64
		micros      int64
		currency    string
	}
	var groups []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.campaignID, &g.impressions, &g.micros, &g.currency); err != nil {
			rows.Close()
			return 0, err
		}
		groups = append(groups, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	posted := 0
	for _, g := range groups {
		amount := MinorFromMicros(g.micros)
		if amount <= 0 {
			// An hour of impressions worth less than half a fils. Left
			// unbilled rather than rounded to zero and marked charged — the
			// micros stay in the table and roll into the next hour that
			// crosses the threshold.
			continue
		}

		err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			var chargeID uuid.UUID
			err := tx.QueryRow(ctx, `
				INSERT INTO boost_charges
				    (campaign_id, period_start, period_end, impressions, amount_minor, currency)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (campaign_id, period_start) DO NOTHING
				RETURNING id`,
				g.campaignID, start, period, g.impressions, amount, g.currency).Scan(&chargeID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // already billed
			}
			if err != nil {
				return fmt.Errorf("boosts: insert charge: %w", err)
			}

			var advertiserType string
			var advertiserID uuid.UUID
			if err := tx.QueryRow(ctx,
				`SELECT advertiser_type, advertiser_id FROM boost_campaigns WHERE id = $1`,
				g.campaignID).Scan(&advertiserType, &advertiserID); err != nil {
				return fmt.Errorf("boosts: load advertiser: %w", err)
			}

			journalID, err := ledger.PostBoostSpend(ctx, tx, ledger.BoostSpend{
				CampaignID:     g.campaignID,
				AdvertiserType: advertiserType,
				AdvertiserID:   advertiserID,
				Currency:       g.currency,
				AmountMinor:    amount,
			})
			if err != nil {
				return err
			}

			if _, err := tx.Exec(ctx,
				`UPDATE boost_charges SET journal_id = $2 WHERE id = $1`, chargeID, journalID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE boost_deliveries SET charge_id = $1
				 WHERE campaign_id = $2 AND state = 'confirmed' AND charge_id IS NULL
				   AND served_at >= $3 AND served_at < $4`,
				chargeID, g.campaignID, start, period); err != nil {
				return err
			}
			// The campaign's own running total, which is what the
			// cannot-outspend-funding constraint checks.
			if _, err := tx.Exec(ctx, `
				UPDATE boost_campaigns
				   SET spent_minor = spent_minor + $2,
				       status = CASE WHEN spent_minor + $2 >= funded_minor
				                     THEN 'exhausted' ELSE status END
				 WHERE id = $1`, g.campaignID, amount); err != nil {
				return err
			}
			posted++
			return nil
		})
		if err != nil {
			return posted, err
		}
	}
	return posted, nil
}

// EnsurePartitions creates delivery partitions ahead of time.
func (s *Service) EnsurePartitions(ctx context.Context, daysAhead int) error {
	now := time.Now().UTC()
	for i := 0; i <= daysAhead; i++ {
		day := now.AddDate(0, 0, i).Format("2006-01-02")
		if _, err := s.pool.Exec(ctx, `SELECT create_boost_delivery_partition($1::date)`, day); err != nil {
			return fmt.Errorf("boosts: ensure partition: %w", err)
		}
	}
	return nil
}

// --- campaign lifecycle -----------------------------------------------------

// Create opens a draft campaign.
func (s *Service) Create(ctx context.Context, in CreateInput) (uuid.UUID, error) {
	var floor int
	if err := s.pool.QueryRow(ctx, `
		SELECT floor_cpm_minor FROM boost_rate_cards
		 WHERE placement = 'feed' AND country = $1 AND valid @> CURRENT_DATE
		 LIMIT 1`, in.Country).Scan(&floor); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("%w: no rate card for %s", httpx.ErrConflict, in.Country)
		}
		return uuid.Nil, fmt.Errorf("boosts: load rate card: %w", err)
	}

	c := Campaign{
		BidCPMinor: in.BidCPMinor, FundedMinor: in.FundedMinor,
		DailyCapMinor: in.DailyCapMinor, StartsOn: in.StartsOn, EndsOn: in.EndsOn,
	}
	if err := c.Validate(floor); err != nil {
		return uuid.Nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "bid_cpm_minor", Code: "invalid", Message: err.Error(),
		}}}
	}

	var id uuid.UUID
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// The reel must already be published. A campaign that could be created
		// against a draft would let an advertiser buy a slot and then change
		// what fills it, which is how a moderation queue gets bypassed.
		var state string
		if err := tx.QueryRow(ctx,
			`SELECT state::text FROM reels WHERE id = $1 AND removed_at IS NULL`, in.ReelID).
			Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return err
		}
		if state != "published" {
			return fmt.Errorf("%w: a %s reel cannot be boosted", httpx.ErrConflict, state)
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO boost_campaigns
			    (advertiser_type, advertiser_id, reel_id, objective, bid_cpm_minor,
			     daily_cap_minor, starts_on, ends_on, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'draft')
			RETURNING id`,
			in.AdvertiserType, in.AdvertiserID, in.ReelID, in.Objective,
			in.BidCPMinor, in.DailyCapMinor, in.StartsOn, nullableDate(in.EndsOn)).
			Scan(&id); err != nil {
			return fmt.Errorf("boosts: create campaign: %w", err)
		}

		if in.Targeting != nil {
			if err := insertTargeting(ctx, tx, id, *in.Targeting); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

type CreateInput struct {
	AdvertiserType string
	AdvertiserID   uuid.UUID
	ReelID         uuid.UUID
	Country        string
	Objective      string
	BidCPMinor     int
	FundedMinor    int64
	DailyCapMinor  int64
	StartsOn       time.Time
	EndsOn         time.Time
	Targeting      *Targeting
}

type Targeting struct {
	Lat, Lng    float64
	RadiusM     int
	Emirates    []string
	CategoryIDs []uuid.UUID
	Locales     []string
	HoursOfDay  []int
	DaysOfWeek  []int
}

func insertTargeting(ctx context.Context, tx pgx.Tx, campaignID uuid.UUID, t Targeting) error {
	var centre any
	if t.RadiusM > 0 {
		centre = fmt.Sprintf("SRID=4326;POINT(%f %f)", t.Lng, t.Lat)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO boost_targeting
		    (campaign_id, centre, radius_m, emirates, category_ids, locales, hours_of_day, days_of_week)
		VALUES ($1, $2::geography, nullif($3, 0), $4, $5, $6, $7, $8)`,
		campaignID, centre, t.RadiusM, nilIfEmptyStr(t.Emirates), nilIfEmptyUUID(t.CategoryIDs),
		nilIfEmptyStr(t.Locales), nilIfEmptyInt(t.HoursOfDay), nilIfEmptyInt(t.DaysOfWeek))
	if err != nil {
		return fmt.Errorf("boosts: insert targeting: %w", err)
	}
	return nil
}

// Fund records a completed payment against a campaign.
//
// Prepaid, so this is what makes a campaign runnable at all. Called from the
// payment webhook path; the campaign moves to `pending_review` rather than
// straight to `active` because a paid campaign still has creative a human has
// not looked at.
func (s *Service) Fund(ctx context.Context, campaignID uuid.UUID, amountMinor int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE boost_campaigns
		   SET funded_minor = funded_minor + $2,
		       status = CASE WHEN status = 'draft' THEN 'pending_review'
		                     WHEN status = 'exhausted' THEN 'active'
		                     ELSE status END
		 WHERE id = $1`, campaignID, amountMinor)
	if err != nil {
		return fmt.Errorf("boosts: fund campaign: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

// Review approves or rejects a campaign's creative.
func (s *Service) Review(ctx context.Context, campaignID, reviewerID uuid.UUID, approve bool, note string) error {
	if !approve && note == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "note", Code: "required",
			Message: "a rejected campaign with no reason is resubmitted unchanged",
		}}}
	}
	status := "active"
	if !approve {
		status = "rejected"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE boost_campaigns
		   SET status = $2, reviewed_by = $3, review_note = nullif($4, '')
		 WHERE id = $1 AND status = 'pending_review'`,
		campaignID, status, reviewerID, note)
	if err != nil {
		return fmt.Errorf("boosts: review campaign: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: this campaign is not awaiting review", httpx.ErrConflict)
	}
	return nil
}

// AuthorizeAdvertiser proves the caller owns the campaign.
//
// User-owned campaigns check the user id directly. Vendor-owned ones go through
// `vendor_users`, which is the same membership table the console uses — so a
// staff member who loses console access loses campaign control in the same
// moment, rather than keeping it until someone remembers this table exists.
func (s *Service) AuthorizeAdvertiser(ctx context.Context, userID, campaignID uuid.UUID) error {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM boost_campaigns c
		     WHERE c.id = $1
		       AND ((c.advertiser_type = 'user' AND c.advertiser_id = $2)
		         OR (c.advertiser_type = 'vendor' AND EXISTS (
		                SELECT 1 FROM vendor_users vu
		                 WHERE vu.vendor_id = c.advertiser_id AND vu.user_id = $2))))`,
		campaignID, userID).Scan(&ok)
	if err != nil {
		return fmt.Errorf("boosts: authorize: %w", err)
	}
	if !ok {
		// Not found rather than forbidden: a 403 confirms the campaign exists,
		// which lets an advertiser enumerate competitors' campaign ids.
		return httpx.ErrNotFound
	}
	return nil
}

// PendingReview is a campaign waiting on a human.
type PendingReview struct {
	ID             uuid.UUID
	Reference      string
	AdvertiserType string
	AdvertiserID   uuid.UUID
	AdvertiserName string
	ReelID         uuid.UUID
	Caption        string
	PosterKey      string
	BidCPMinor     int
	FundedMinor    int64
	DailyCapMinor  int64
	Currency       string
	StartsOn       time.Time
	CreatedAt      time.Time
	// Targeting, rendered as a sentence rather than as fields. The reviewer's
	// question is "is this sane", and eight nullable columns is not the shape
	// that answers it.
	Targeting string
}

// ReviewQueue lists campaigns awaiting approval.
//
// Added when the admin panel was built, and its absence was the finding: Phase 9
// shipped `POST /moderation/boosts/{id}/decide` and no way to find a campaign
// to decide on. A funded campaign sat in `pending_review` forever, which is the
// worst outcome available — the advertiser has paid and nothing is running.
func (s *Service) ReviewQueue(ctx context.Context, limit int) ([]PendingReview, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.reference_no, c.advertiser_type, c.advertiser_id,
		       coalesce(v.trade_name, u.display_name, u.phone, ''),
		       c.reel_id, coalesce(r.caption, ''), coalesce(r.poster_key, ''),
		       c.bid_cpm_minor, c.funded_minor, c.daily_cap_minor, c.currency,
		       c.starts_on, c.created_at,
		       coalesce(
		           nullif(concat_ws(', ',
		               CASE WHEN t.radius_m IS NOT NULL
		                    THEN (t.radius_m / 1000)::text || ' km radius' END,
		               CASE WHEN t.locales IS NOT NULL
		                    THEN array_to_string(t.locales, '/') END,
		               CASE WHEN t.hours_of_day IS NOT NULL
		                    THEN array_length(t.hours_of_day, 1)::text || ' hours of the day' END,
		               CASE WHEN t.days_of_week IS NOT NULL
		                    THEN array_length(t.days_of_week, 1)::text || ' days of the week' END,
		               CASE WHEN t.category_ids IS NOT NULL
		                    THEN array_length(t.category_ids, 1)::text || ' categories' END
		           ), ''),
		           'no narrowing — the reel''s own service area only')
		  FROM boost_campaigns c
		  JOIN reels r ON r.id = c.reel_id
		  LEFT JOIN boost_targeting t ON t.campaign_id = c.id
		  LEFT JOIN vendors v ON c.advertiser_type = 'vendor' AND v.id = c.advertiser_id
		  LEFT JOIN users u   ON c.advertiser_type = 'user'   AND u.id = c.advertiser_id
		 WHERE c.status = 'pending_review'
		 ORDER BY c.created_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("boosts: review queue: %w", err)
	}
	defer rows.Close()

	var out []PendingReview
	for rows.Next() {
		var p PendingReview
		if err := rows.Scan(&p.ID, &p.Reference, &p.AdvertiserType, &p.AdvertiserID,
			&p.AdvertiserName, &p.ReelID, &p.Caption, &p.PosterKey,
			&p.BidCPMinor, &p.FundedMinor, &p.DailyCapMinor, &p.Currency,
			&p.StartsOn, &p.CreatedAt, &p.Targeting); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetStatus pauses or resumes.
func (s *Service) SetStatus(ctx context.Context, campaignID uuid.UUID, status string) error {
	if status != "paused" && status != "active" && status != "completed" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "status", Code: "invalid",
		}}}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE boost_campaigns SET status = $2
		 WHERE id = $1 AND status IN ('active', 'paused')`, campaignID, status)
	if err != nil {
		return fmt.Errorf("boosts: set status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: this campaign cannot be %s from its current state",
			httpx.ErrConflict, status)
	}
	return nil
}

// Report is what an advertiser sees.
//
// Impressions and spend, and deliberately not "clicks". The click already has a
// home — `action_clicks` and the Phase 3 funnel — and duplicating it here would
// produce a second conversion number computed a different way, which is exactly
// what PHASE-3 §4 warns against.
type Report struct {
	CampaignID  uuid.UUID
	Reference   string
	Status      string
	Impressions int64
	SpentMinor  int64
	FundedMinor int64
	Currency    string
}

func (s *Service) Report(ctx context.Context, advertiserType string, advertiserID uuid.UUID) ([]Report, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.reference_no, c.status, c.spent_minor, c.funded_minor, c.currency,
		       coalesce(sum(d.charged), 0)
		  FROM boost_campaigns c
		  LEFT JOIN boost_daily_spend d ON d.campaign_id = c.id
		 WHERE c.advertiser_type = $1 AND c.advertiser_id = $2
		 GROUP BY c.id
		 ORDER BY c.created_at DESC
		 LIMIT 100`, advertiserType, advertiserID)
	if err != nil {
		return nil, fmt.Errorf("boosts: report: %w", err)
	}
	defer rows.Close()

	var out []Report
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.CampaignID, &r.Reference, &r.Status, &r.SpentMinor,
			&r.FundedMinor, &r.Currency, &r.Impressions); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- helpers ----------------------------------------------------------------

// isCheckViolation reports whether err is a 23514 on the named constraint.
//
// Named rather than any check violation, for the same reason `ordering` names
// its unique constraints: catching all of them would swallow a genuine bug in an
// unrelated CHECK and silently drop an impression instead of failing.
func isCheckViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23514" && pgErr.ConstraintName == constraint
}

func nullUUIDArg(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

func nullableUUID(id uuid.NullUUID) any {
	if !id.Valid {
		return nil
	}
	return id.UUID
}

func nullableDate(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func localeOr(l string) string {
	if l == "" {
		return "en"
	}
	return l
}

func excludeArg(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}

func nilIfEmptyStr(xs []string) any {
	if len(xs) == 0 {
		return nil
	}
	return xs
}

func nilIfEmptyUUID(xs []uuid.UUID) any {
	if len(xs) == 0 {
		return nil
	}
	return xs
}

func nilIfEmptyInt(xs []int) any {
	if len(xs) == 0 {
		return nil
	}
	return xs
}
