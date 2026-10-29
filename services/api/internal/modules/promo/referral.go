package promo

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Referral statuses.
const (
	ReferralPending  = "pending"
	ReferralRewarded = "rewarded"
	ReferralRejected = "rejected"
	ReferralVoid     = "void"

	// A friend delivering to within this many metres of one of the referrer's
	// own addresses is treated as the referrer inviting themselves.
	sameAddressMetres = 50
)

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randomCode(n int) (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(codeAlphabet)))
	for range n {
		i, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(codeAlphabet[i.Int64()])
	}
	return b.String(), nil
}

// Program is the referral programme staff configure.
type Program struct {
	IsActive          bool       `json:"is_active"`
	FriendAmountMinor int64      `json:"friend_amount_minor"`
	RewardAmountMinor int64      `json:"reward_amount_minor"`
	Currency          string     `json:"currency"`
	MonthlyCap        int        `json:"monthly_cap"`
	RewardValidDays   int        `json:"reward_valid_days"`
	FriendPromoID     *uuid.UUID `json:"friend_promo_id"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

const programCols = `is_active, friend_amount_minor, reward_amount_minor, currency, monthly_cap,
	reward_valid_days, friend_promo_id, updated_at`

func scanProgram(row pgx.Row) (Program, error) {
	var p Program
	err := row.Scan(&p.IsActive, &p.FriendAmountMinor, &p.RewardAmountMinor, &p.Currency,
		&p.MonthlyCap, &p.RewardValidDays, &p.FriendPromoID, &p.UpdatedAt)
	return p, err
}

func (s *Service) Program(ctx context.Context) (Program, error) {
	p, err := scanProgram(s.pool.QueryRow(ctx, `SELECT `+programCols+` FROM referral_program`))
	if err != nil {
		return p, fmt.Errorf("referral: program: %w", err)
	}
	return p, nil
}

// ProgramInput is what staff edit.
type ProgramInput struct {
	IsActive          bool   `json:"is_active"`
	FriendAmountMinor int64  `json:"friend_amount_minor"`
	RewardAmountMinor int64  `json:"reward_amount_minor"`
	Currency          string `json:"currency"`
	MonthlyCap        int    `json:"monthly_cap"`
	RewardValidDays   int    `json:"reward_valid_days"`
}

func (in *ProgramInput) validate() error {
	var fields []httpx.FieldError
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "AED"
	}
	if len(in.Currency) != 3 {
		fields = append(fields, fieldErr("currency", "invalid", "a 3-letter currency code"))
	}
	if in.FriendAmountMinor <= 0 {
		fields = append(fields, fieldErr("friend_amount_minor", "required", "the friend's discount must be more than zero"))
	}
	if in.RewardAmountMinor <= 0 {
		fields = append(fields, fieldErr("reward_amount_minor", "required", "the referrer's voucher must be more than zero"))
	}
	if in.MonthlyCap < 1 || in.MonthlyCap > 100 {
		fields = append(fields, fieldErr("monthly_cap", "out_of_range", "1 to 100 vouchers a month"))
	}
	if in.RewardValidDays < 7 || in.RewardValidDays > 365 {
		fields = append(fields, fieldErr("reward_valid_days", "out_of_range", "vouchers last 7 to 365 days"))
	}
	if len(fields) > 0 {
		return &httpx.ValidationError{Fields: fields}
	}
	return nil
}

// SaveProgram updates the programme and keeps the friend promo in step with it.
func (s *Service) SaveProgram(ctx context.Context, actor uuid.UUID, in ProgramInput) (Program, error) {
	if err := in.validate(); err != nil {
		return Program{}, err
	}
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		cur, err := scanProgram(tx.QueryRow(ctx, `SELECT `+programCols+` FROM referral_program FOR UPDATE`))
		if err != nil {
			return fmt.Errorf("referral: program: %w", err)
		}
		friendID := cur.FriendPromoID
		if friendID == nil {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO promos (code, name, name_ar, description, description_ar, discount_type,
					amount_minor, currency, applies_to, audience, per_customer_limit, is_public,
					is_active, kind, created_by)
				VALUES ('REFERRAL_FRIEND', 'Invite from a friend', 'دعوة من صديق',
					'Welcome discount on your first meal plan', 'خصم ترحيبي على أول خطة وجبات',
					'fixed', $1, $2, 'both', 'new_customers', 1, false, $3, 'referral_friend', $4)
				RETURNING id`, in.FriendAmountMinor, in.Currency, in.IsActive, actor).Scan(&id); err != nil {
				return fmt.Errorf("referral: friend promo: %w", err)
			}
			friendID = &id
		} else if _, err := tx.Exec(ctx, `
			UPDATE promos SET amount_minor = $2, currency = $3, is_active = $4, updated_at = now()
			 WHERE id = $1`, *friendID, in.FriendAmountMinor, in.Currency, in.IsActive); err != nil {
			return fmt.Errorf("referral: friend promo: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE referral_program SET is_active = $1, friend_amount_minor = $2,
				reward_amount_minor = $3, currency = $4, monthly_cap = $5, reward_valid_days = $6,
				friend_promo_id = $7, updated_by = $8, updated_at = now()`,
			in.IsActive, in.FriendAmountMinor, in.RewardAmountMinor, in.Currency, in.MonthlyCap,
			in.RewardValidDays, *friendID, actor); err != nil {
			return fmt.Errorf("referral: save program: %w", err)
		}
		return nil
	})
	if err != nil {
		return Program{}, err
	}
	return s.Program(ctx)
}

// invitePromo resolves a customer's invite code to the friend promo, marked
// with who invited them. pgx.ErrNoRows when the code is not an invite code.
func (s *Service) invitePromo(ctx context.Context, tx pgx.Tx, code string, userID uuid.UUID) (Promo, error) {
	var referrer uuid.UUID
	err := tx.QueryRow(ctx, `SELECT user_id FROM referral_codes WHERE code = upper(btrim($1))`,
		code).Scan(&referrer)
	if err != nil {
		return Promo{}, err
	}
	if referrer == userID {
		return Promo{}, invalid("promo_code", "own_code", "you can't use your own invite code")
	}
	prog, err := scanProgram(tx.QueryRow(ctx, `SELECT `+programCols+` FROM referral_program`))
	if err != nil {
		return Promo{}, fmt.Errorf("referral: program: %w", err)
	}
	if !prog.IsActive || prog.FriendPromoID == nil {
		return Promo{}, invalid("promo_code", "not_live", "invites are not running right now")
	}
	var referred bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM referrals WHERE friend_id = $1 AND status <> 'pending')`,
		userID).Scan(&referred); err != nil {
		return Promo{}, fmt.Errorf("referral: already referred: %w", err)
	}
	if referred {
		return Promo{}, invalid("promo_code", "already_used", "you have already used an invite")
	}
	p, err := scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM promos p
		 WHERE p.id = $1 AND p.deleted_at IS NULL`, *prog.FriendPromoID))
	if err != nil {
		return Promo{}, fmt.Errorf("referral: friend promo: %w", err)
	}
	p.ReferrerID = &referrer
	return p, nil
}

// recordReferral links the friend's checkout to the referrer. A pending row
// from an earlier abandoned checkout is replaced.
func recordReferral(ctx context.Context, tx pgx.Tx, referrer, friend uuid.UUID, t Target) error {
	tag, err := tx.Exec(ctx, `
		INSERT INTO referrals (referrer_id, friend_id, subscription_id, rotation_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (friend_id) DO UPDATE SET referrer_id = EXCLUDED.referrer_id,
			subscription_id = EXCLUDED.subscription_id, rotation_id = EXCLUDED.rotation_id,
			created_at = now()
		 WHERE referrals.status = 'pending'`, referrer, friend, t.SubscriptionID, t.RotationID)
	if err != nil {
		return fmt.Errorf("referral: record: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return invalid("promo_code", "already_used", "you have already used an invite")
	}
	return nil
}

// ---- customer -------------------------------------------------------------

type Voucher struct {
	Code        string     `json:"code"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	EndsAt      *time.Time `json:"ends_at"`
	Used        bool       `json:"used"`
	Active      bool       `json:"active"`
}

// MyReferrals is a customer's invite page.
type MyReferrals struct {
	Active            bool      `json:"active"`
	Code              string    `json:"code,omitempty"`
	FriendAmountMinor int64     `json:"friend_amount_minor"`
	RewardAmountMinor int64     `json:"reward_amount_minor"`
	Currency          string    `json:"currency"`
	Pending           int       `json:"pending"`
	Rewarded          int       `json:"rewarded"`
	Vouchers          []Voucher `json:"vouchers"`
}

func (s *Service) MyReferrals(ctx context.Context, userID uuid.UUID) (MyReferrals, error) {
	prog, err := s.Program(ctx)
	if err != nil {
		return MyReferrals{}, err
	}
	out := MyReferrals{Active: prog.IsActive, FriendAmountMinor: prog.FriendAmountMinor,
		RewardAmountMinor: prog.RewardAmountMinor, Currency: prog.Currency, Vouchers: []Voucher{}}
	if prog.IsActive {
		if out.Code, err = s.ensureCode(ctx, userID); err != nil {
			return out, err
		}
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'pending'), count(*) FILTER (WHERE status = 'rewarded')
		  FROM referrals WHERE referrer_id = $1`, userID).Scan(&out.Pending, &out.Rewarded); err != nil {
		return out, fmt.Errorf("referral: counts: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT code, amount_minor, currency, ends_at, usage_count > 0,
		       is_active AND deleted_at IS NULL AND (ends_at IS NULL OR ends_at > now()) AND usage_count = 0
		  FROM promos WHERE owner_user_id = $1 AND kind = 'referral_reward'
		 ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		return out, fmt.Errorf("referral: vouchers: %w", err)
	}
	vs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Voucher, error) {
		var v Voucher
		err := r.Scan(&v.Code, &v.AmountMinor, &v.Currency, &v.EndsAt, &v.Used, &v.Active)
		return v, err
	})
	if err != nil {
		return out, fmt.Errorf("referral: vouchers: %w", err)
	}
	out.Vouchers = append(out.Vouchers, vs...)
	return out, nil
}

func (s *Service) ensureCode(ctx context.Context, userID uuid.UUID) (string, error) {
	var code string
	err := s.pool.QueryRow(ctx, `SELECT code FROM referral_codes WHERE user_id = $1`, userID).Scan(&code)
	if err == nil {
		return code, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("referral: code: %w", err)
	}
	for range 5 {
		c, err := randomCode(8)
		if err != nil {
			return "", fmt.Errorf("referral: code: %w", err)
		}
		err = s.pool.QueryRow(ctx, `
			INSERT INTO referral_codes (user_id, code)
			SELECT $1, $2 WHERE NOT EXISTS (SELECT 1 FROM promos WHERE code = $2)
			ON CONFLICT (user_id) DO UPDATE SET code = referral_codes.code
			RETURNING code`, userID, c).Scan(&code)
		if err == nil {
			return code, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) && !isUnique(err) {
			return "", fmt.Errorf("referral: code: %w", err)
		}
	}
	return "", errors.New("referral: could not allocate an invite code")
}

// ---- settlement -----------------------------------------------------------

// SettleReferrals decides pending referrals: a friend whose first meal was
// delivered earns the referrer a voucher, unless the referral looks like the
// referrer inviting themselves or they hit the monthly cap; a friend who
// cancelled before any delivery earns nothing. Returns how many were decided.
func (s *Service) SettleReferrals(ctx context.Context, batch int) (int, error) {
	rejected, err := s.pool.Exec(ctx, `
		UPDATE referrals r SET status = 'rejected', reason = 'cancelled before the first delivery',
		       settled_at = now()
		 WHERE r.status = 'pending'
		   AND EXISTS (SELECT 1 FROM promo_redemptions pr
		                WHERE pr.status = 'redeemed'
		                  AND (pr.subscription_id = r.subscription_id OR pr.rotation_id = r.rotation_id))
		   AND NOT EXISTS (SELECT 1 FROM subscriptions s
		                    WHERE (s.id = r.subscription_id OR s.rotation_id = r.rotation_id)
		                      AND s.status <> 'cancelled')
		   AND NOT EXISTS (SELECT 1 FROM fulfillments f JOIN subscriptions s ON s.id = f.subscription_id
		                    WHERE (s.id = r.subscription_id OR s.rotation_id = r.rotation_id)
		                      AND f.status = 'completed')`)
	if err != nil {
		return 0, fmt.Errorf("referral: reject cancelled: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.id FROM referrals r
		 WHERE r.status = 'pending'
		   AND EXISTS (SELECT 1 FROM fulfillments f JOIN subscriptions s ON s.id = f.subscription_id
		                WHERE (s.id = r.subscription_id OR s.rotation_id = r.rotation_id)
		                  AND f.status = 'completed')
		 ORDER BY r.created_at LIMIT $1`, batch)
	if err != nil {
		return 0, fmt.Errorf("referral: due: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, fmt.Errorf("referral: due: %w", err)
	}
	n := int(rejected.RowsAffected())
	for _, id := range ids {
		if err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error { return s.settleOne(ctx, tx, id) }); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Service) settleOne(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	var referrer uuid.UUID
	var sub, rot *uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT referrer_id, subscription_id, rotation_id FROM referrals
		 WHERE id = $1 AND status = 'pending' FOR UPDATE`, id).Scan(&referrer, &sub, &rot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("referral: lock: %w", err)
	}
	// One referrer at a time, so two settlements cannot both slip under the cap.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('referral:' || $1::text))`, referrer); err != nil {
		return fmt.Errorf("referral: lock referrer: %w", err)
	}
	reject := func(reason string) error {
		_, err := tx.Exec(ctx, `UPDATE referrals SET status = 'rejected', reason = $2, settled_at = now()
			 WHERE id = $1`, id, reason)
		if err != nil {
			return fmt.Errorf("referral: reject: %w", err)
		}
		return nil
	}

	var sameAddress bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM subscriptions s
			  JOIN addresses fa ON fa.id = s.address_id
			  JOIN addresses ra ON ra.user_id = $1 AND ra.id <> fa.id
			 WHERE (s.id = $2 OR s.rotation_id = $3)
			   AND ST_DWithin(fa.point, ra.point, $4))`,
		referrer, sub, rot, sameAddressMetres).Scan(&sameAddress); err != nil {
		return fmt.Errorf("referral: address check: %w", err)
	}
	if sameAddress {
		return reject("friend delivers to the referrer's address")
	}

	prog, err := scanProgram(tx.QueryRow(ctx, `SELECT `+programCols+` FROM referral_program`))
	if err != nil {
		return fmt.Errorf("referral: program: %w", err)
	}
	var thisMonth int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM referrals
		 WHERE referrer_id = $1 AND status = 'rewarded'
		   AND settled_at >= date_trunc('month', now() AT TIME ZONE 'Asia/Dubai') AT TIME ZONE 'Asia/Dubai'`,
		referrer).Scan(&thisMonth); err != nil {
		return fmt.Errorf("referral: monthly count: %w", err)
	}
	if thisMonth >= prog.MonthlyCap {
		return reject("monthly voucher limit reached")
	}

	var promoID uuid.UUID
	for attempt := 0; ; attempt++ {
		code, err := randomCode(8)
		if err != nil {
			return fmt.Errorf("referral: voucher code: %w", err)
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO promos (code, name, name_ar, description, description_ar, discount_type,
				amount_minor, currency, applies_to, audience, usage_limit, per_customer_limit,
				is_public, is_active, ends_at, kind, owner_user_id)
			SELECT $1, 'Referral reward', 'مكافأة دعوة صديق',
				'Thanks for inviting a friend', 'شكرًا لدعوتك صديقًا',
				'fixed', $2, $3, 'both', 'everyone', 1, 1, false, true,
				now() + make_interval(days => $4), 'referral_reward', $5
			 WHERE NOT EXISTS (SELECT 1 FROM referral_codes WHERE code = $1)
			RETURNING id`, "RW"+code, prog.RewardAmountMinor, prog.Currency, prog.RewardValidDays,
			referrer).Scan(&promoID)
		if err == nil {
			break
		}
		if attempt >= 4 || (!errors.Is(err, pgx.ErrNoRows) && !isUnique(err)) {
			return fmt.Errorf("referral: voucher: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE referrals SET status = 'rewarded', reward_promo_id = $2, settled_at = now()
		 WHERE id = $1`, id, promoID); err != nil {
		return fmt.Errorf("referral: reward: %w", err)
	}
	return nil
}

// ---- admin ----------------------------------------------------------------

type AdminReferral struct {
	ID             uuid.UUID  `json:"id"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason"`
	ReferrerID     uuid.UUID  `json:"referrer_id"`
	ReferrerPhone  string     `json:"referrer_phone"`
	ReferrerName   *string    `json:"referrer_name"`
	FriendID       uuid.UUID  `json:"friend_id"`
	FriendPhone    string     `json:"friend_phone"`
	FriendName     *string    `json:"friend_name"`
	SubscriptionID *uuid.UUID `json:"subscription_id"`
	RotationID     *uuid.UUID `json:"rotation_id"`
	VoucherCode    *string    `json:"voucher_code"`
	VoucherUsed    bool       `json:"voucher_used"`
	CreatedAt      time.Time  `json:"created_at"`
	SettledAt      *time.Time `json:"settled_at"`
}

func (s *Service) AdminReferrals(ctx context.Context, status, q string) ([]AdminReferral, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.status, r.reason, r.referrer_id, ru.phone, ru.display_name,
		       r.friend_id, fu.phone, fu.display_name, r.subscription_id, r.rotation_id,
		       p.code, COALESCE(p.usage_count > 0, false), r.created_at, r.settled_at
		  FROM referrals r
		  JOIN users ru ON ru.id = r.referrer_id
		  JOIN users fu ON fu.id = r.friend_id
		  LEFT JOIN promos p ON p.id = r.reward_promo_id
		 WHERE ($1 = '' OR r.status = $1)
		   AND ($2 = '' OR ru.phone LIKE '%' || $2 || '%' OR fu.phone LIKE '%' || $2 || '%'
		        OR p.code = upper($2)
		        OR EXISTS (SELECT 1 FROM referral_codes rc WHERE rc.user_id = r.referrer_id AND rc.code = upper($2)))
		 ORDER BY r.created_at DESC LIMIT 200`, status, strings.TrimSpace(q))
	if err != nil {
		return nil, fmt.Errorf("referral: list: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AdminReferral, error) {
		var a AdminReferral
		err := r.Scan(&a.ID, &a.Status, &a.Reason, &a.ReferrerID, &a.ReferrerPhone, &a.ReferrerName,
			&a.FriendID, &a.FriendPhone, &a.FriendName, &a.SubscriptionID, &a.RotationID,
			&a.VoucherCode, &a.VoucherUsed, &a.CreatedAt, &a.SettledAt)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("referral: list: %w", err)
	}
	return out, nil
}

// VoidReferral cancels a referral staff judge to be abuse. An unused voucher
// stops working; one already spent stays spent.
func (s *Service) VoidReferral(ctx context.Context, id uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return invalid("reason", "required", "say why this referral is being voided")
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var promoID *uuid.UUID
		err := tx.QueryRow(ctx, `
			UPDATE referrals SET status = 'void', reason = $2, settled_at = COALESCE(settled_at, now())
			 WHERE id = $1 AND status IN ('pending', 'rewarded')
			RETURNING reward_promo_id`, id, reason).Scan(&promoID)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.Coded(httpx.ErrConflict, "not_voidable", "only pending or rewarded referrals can be voided")
		}
		if err != nil {
			return fmt.Errorf("referral: void: %w", err)
		}
		if promoID != nil {
			if _, err := tx.Exec(ctx, `UPDATE promos SET is_active = false, updated_at = now()
				 WHERE id = $1 AND usage_count = 0`, *promoID); err != nil {
				return fmt.Errorf("referral: void voucher: %w", err)
			}
		}
		return nil
	})
}
