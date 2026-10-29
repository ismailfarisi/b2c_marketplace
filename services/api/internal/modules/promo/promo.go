// Package promo holds staff-controlled discounts and trials for meal
// subscriptions and four-kitchen rotations.
//
// A promo either discounts the first cycle (percent or fixed, optionally
// capped) or turns it into a short trial (trial_days, priced pro rata, no
// renewal), or both. Who funds the discount is per promo: kitchen_share_bp of
// it comes off the kitchen's earnings, the rest out of platform commission.
package promo

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/money"
	"github.com/marketplace/api/internal/seam/servicetype"
)

const (
	TypePercent = "percent"
	TypeFixed   = "fixed"
	TypeNone    = "none"

	ForSubscriptions = "subscriptions"
	ForRotations     = "rotations"
	ForBoth          = "both"

	AudienceEveryone = "everyone"
	AudienceNew      = "new_customers"

	MaxTrialDays = 14
)

type Promo struct {
	ID               uuid.UUID   `json:"id"`
	Code             *string     `json:"code"`
	Name             string      `json:"name"`
	NameAr           string      `json:"name_ar"`
	Description      string      `json:"description"`
	DescriptionAr    string      `json:"description_ar"`
	DiscountType     string      `json:"discount_type"`
	PercentBP        int         `json:"percent_bp"`
	AmountMinor      int64       `json:"amount_minor"`
	MaxDiscountMinor *int64      `json:"max_discount_minor"`
	Currency         string      `json:"currency"`
	TrialDays        *int        `json:"trial_days"`
	AppliesTo        string      `json:"applies_to"`
	Audience         string      `json:"audience"`
	KitchenShareBP   int         `json:"kitchen_share_bp"`
	UsageLimit       *int        `json:"usage_limit"`
	PerCustomerLimit *int        `json:"per_customer_limit"`
	UsageCount       int         `json:"usage_count"`
	IsPublic         bool        `json:"is_public"`
	IsActive         bool        `json:"is_active"`
	StartsAt         *time.Time  `json:"starts_at"`
	EndsAt           *time.Time  `json:"ends_at"`
	BranchIDs        []uuid.UUID `json:"branch_ids"`
	UpdatedAt        time.Time   `json:"updated_at"`

	// Kind is standard for staff-made promos; referral promos are managed by
	// the referral programme and never listed with them.
	Kind        string     `json:"-"`
	OwnerUserID *uuid.UUID `json:"-"`
	// ReferrerID is set when the promo was reached through an invite code.
	ReferrerID *uuid.UUID `json:"-"`
}

// Offer is the customer-facing view of a promo: what it gives, not how it is
// funded or how often it has been used.
type Offer struct {
	ID               uuid.UUID  `json:"id"`
	Code             *string    `json:"code,omitempty"`
	Name             string     `json:"name"`
	NameAr           string     `json:"name_ar"`
	Description      string     `json:"description"`
	DescriptionAr    string     `json:"description_ar"`
	DiscountType     string     `json:"discount_type"`
	PercentBP        int        `json:"percent_bp"`
	AmountMinor      int64      `json:"amount_minor"`
	MaxDiscountMinor *int64     `json:"max_discount_minor"`
	Currency         string     `json:"currency"`
	TrialDays        *int       `json:"trial_days"`
	AppliesTo        string     `json:"applies_to"`
	Audience         string     `json:"audience"`
	EndsAt           *time.Time `json:"ends_at"`
}

// Offer strips the promo to what a customer may see. The code is included
// only when the customer typed it.
func (p Promo) Offer(withCode bool) *Offer {
	o := &Offer{ID: p.ID, Name: p.Name, NameAr: p.NameAr, Description: p.Description,
		DescriptionAr: p.DescriptionAr, DiscountType: p.DiscountType, PercentBP: p.PercentBP,
		AmountMinor: p.AmountMinor, MaxDiscountMinor: p.MaxDiscountMinor, Currency: p.Currency,
		TrialDays: p.TrialDays, AppliesTo: p.AppliesTo, Audience: p.Audience, EndsAt: p.EndsAt}
	if withCode {
		o.Code = p.Code
	}
	return o
}

// Input is what staff edit. Same shape as Promo minus server-owned fields.
type Input struct {
	Code             *string     `json:"code"`
	Name             string      `json:"name"`
	NameAr           string      `json:"name_ar"`
	Description      string      `json:"description"`
	DescriptionAr    string      `json:"description_ar"`
	DiscountType     string      `json:"discount_type"`
	PercentBP        int         `json:"percent_bp"`
	AmountMinor      int64       `json:"amount_minor"`
	MaxDiscountMinor *int64      `json:"max_discount_minor"`
	Currency         string      `json:"currency"`
	TrialDays        *int        `json:"trial_days"`
	AppliesTo        string      `json:"applies_to"`
	Audience         string      `json:"audience"`
	KitchenShareBP   int         `json:"kitchen_share_bp"`
	UsageLimit       *int        `json:"usage_limit"`
	PerCustomerLimit *int        `json:"per_customer_limit"`
	IsPublic         bool        `json:"is_public"`
	IsActive         bool        `json:"is_active"`
	StartsAt         *time.Time  `json:"starts_at"`
	EndsAt           *time.Time  `json:"ends_at"`
	BranchIDs        []uuid.UUID `json:"branch_ids"`
}

var codeRe = regexp.MustCompile(`^[A-Z0-9_-]{3,32}$`)

func fieldErr(field, code, msg string) httpx.FieldError {
	return httpx.FieldError{Field: field, Code: code, Message: msg}
}

func invalid(field, code, msg string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{fieldErr(field, code, msg)}}
}

// Normalize validates staff input. Mirrors the table's CHECKs so staff get a
// field error instead of a constraint violation.
func Normalize(in Input) (Input, error) {
	var fields []httpx.FieldError
	in.Name = strings.TrimSpace(in.Name)
	in.NameAr = strings.TrimSpace(in.NameAr)
	if in.Code != nil {
		c := strings.ToUpper(strings.TrimSpace(*in.Code))
		if c == "" {
			in.Code = nil
		} else {
			in.Code = &c
			if !codeRe.MatchString(c) {
				fields = append(fields, fieldErr("code", "invalid", "3–32 letters, digits, - or _"))
			}
		}
	}
	if in.Name == "" || len(in.Name) > 120 {
		fields = append(fields, fieldErr("name", "required", "a name of up to 120 characters is required"))
	}
	if in.Currency == "" {
		in.Currency = "AED"
	}
	if in.AppliesTo == "" {
		in.AppliesTo = ForSubscriptions
	}
	if in.Audience == "" {
		in.Audience = AudienceEveryone
	}
	switch in.DiscountType {
	case TypePercent:
		in.AmountMinor = 0
		if in.PercentBP <= 0 || in.PercentBP > 10000 {
			fields = append(fields, fieldErr("percent_bp", "out_of_range", "percent must be between 0.01% and 100%"))
		}
	case TypeFixed:
		in.PercentBP = 0
		in.MaxDiscountMinor = nil
		if in.AmountMinor <= 0 {
			fields = append(fields, fieldErr("amount_minor", "required", "a fixed discount needs an amount"))
		}
	case TypeNone:
		in.PercentBP, in.AmountMinor, in.MaxDiscountMinor = 0, 0, nil
		if in.TrialDays == nil {
			fields = append(fields, fieldErr("discount_type", "required", "a promo without a discount must be a trial"))
		}
	default:
		fields = append(fields, fieldErr("discount_type", "invalid", "percent, fixed or none"))
	}
	if in.MaxDiscountMinor != nil && *in.MaxDiscountMinor <= 0 {
		in.MaxDiscountMinor = nil
	}
	if in.TrialDays != nil {
		if *in.TrialDays < 1 || *in.TrialDays > MaxTrialDays {
			fields = append(fields, fieldErr("trial_days", "out_of_range", "a trial is 1 to 14 days"))
		}
		if in.AppliesTo != ForSubscriptions {
			fields = append(fields, fieldErr("applies_to", "invalid", "a trial applies to single-kitchen subscriptions only"))
		}
	}
	if in.AppliesTo != ForSubscriptions && in.AppliesTo != ForRotations && in.AppliesTo != ForBoth {
		fields = append(fields, fieldErr("applies_to", "invalid", "subscriptions, rotations or both"))
	}
	if in.Audience != AudienceEveryone && in.Audience != AudienceNew {
		fields = append(fields, fieldErr("audience", "invalid", "everyone or new_customers"))
	}
	if in.KitchenShareBP < 0 || in.KitchenShareBP > 10000 {
		fields = append(fields, fieldErr("kitchen_share_bp", "out_of_range", "kitchen share must be 0–100%"))
	}
	if in.UsageLimit != nil && *in.UsageLimit <= 0 {
		in.UsageLimit = nil
	}
	if in.PerCustomerLimit != nil && *in.PerCustomerLimit <= 0 {
		in.PerCustomerLimit = nil
	}
	if in.Code == nil && !in.IsPublic {
		fields = append(fields, fieldErr("code", "required", "a promo that is not shown on the storefront needs a code"))
	}
	if in.StartsAt != nil && in.EndsAt != nil && !in.EndsAt.After(*in.StartsAt) {
		fields = append(fields, fieldErr("ends_at", "before_start", "end must be after start"))
	}
	seen := map[uuid.UUID]bool{}
	branches := make([]uuid.UUID, 0, len(in.BranchIDs))
	for _, b := range in.BranchIDs {
		if !seen[b] {
			seen[b] = true
			branches = append(branches, b)
		}
	}
	in.BranchIDs = branches
	if len(fields) > 0 {
		return in, &httpx.ValidationError{Fields: fields}
	}
	return in, nil
}

// LiveAt reports whether a promo can be used at t.
func (p Promo) LiveAt(t time.Time) bool {
	if !p.IsActive {
		return false
	}
	if p.StartsAt != nil && t.Before(*p.StartsAt) {
		return false
	}
	if p.EndsAt != nil && !t.Before(*p.EndsAt) {
		return false
	}
	return p.UsageLimit == nil || p.UsageCount < *p.UsageLimit
}

// CoversAll reports whether every branch is in the promo's kitchen list.
func (p Promo) CoversAll(branches []uuid.UUID) bool {
	if len(p.BranchIDs) == 0 {
		return true
	}
	in := map[uuid.UUID]bool{}
	for _, b := range p.BranchIDs {
		in[b] = true
	}
	for _, b := range branches {
		if !in[b] {
			return false
		}
	}
	return true
}

// Discount is the amount off a subtotal (meals only; delivery is never
// discounted), never more than the subtotal.
func (p Promo) Discount(subtotalMinor int64) int64 {
	var d int64
	switch p.DiscountType {
	case TypePercent:
		d = subtotalMinor * int64(p.PercentBP) / 10000
	case TypeFixed:
		d = p.AmountMinor
	}
	if p.MaxDiscountMinor != nil && d > *p.MaxDiscountMinor {
		d = *p.MaxDiscountMinor
	}
	if d > subtotalMinor {
		d = subtotalMinor
	}
	if d < 0 {
		d = 0
	}
	return d
}

// PlatformFunded is the part of a discount the platform pays for.
func (p Promo) PlatformFunded(discountMinor int64) int64 {
	return discountMinor - discountMinor*int64(p.KitchenShareBP)/10000
}

// TrialPrice prorates a cycle price to a trial length, rounding half up.
func TrialPrice(cyclePriceMinor int64, trialDays, cycleDays int) int64 {
	if cycleDays <= 0 || trialDays >= cycleDays {
		return cyclePriceMinor
	}
	return (cyclePriceMinor*int64(trialDays)*2 + int64(cycleDays)) / (int64(cycleDays) * 2)
}

// ApplyToQuote takes a discount off a tax-inclusive quote. The VAT on the
// item lines is re-extracted from the discounted amount; delivery VAT stays.
func ApplyToQuote(q *servicetype.QuoteResult, discountMinor int64, itemTaxBP int) {
	if discountMinor <= 0 {
		return
	}
	oldItemTax := money.ExtractTax(money.New(q.SubtotalMinor-q.DiscountMinor, q.Currency), itemTaxBP).Minor
	q.DiscountMinor += discountMinor
	newItemTax := money.ExtractTax(money.New(q.SubtotalMinor-q.DiscountMinor, q.Currency), itemTaxBP).Minor
	q.TaxMinor += newItemTax - oldItemTax
	q.TotalMinor -= discountMinor
	q.AuthorizeMinor = q.TotalMinor
	q.Lines = append(q.Lines, servicetype.QuoteLine{Kind: "discount", Label: "Promo", AmountMinor: -discountMinor})
}

// Spread splits an amount across weights proportionally; the last share takes
// the rounding remainder so the parts always sum to the whole.
func Spread(totalMinor int64, weights []int64) []int64 {
	out := make([]int64, len(weights))
	var sum int64
	for _, w := range weights {
		sum += w
	}
	if sum <= 0 || len(weights) == 0 {
		return out
	}
	var given int64
	for i, w := range weights {
		if i == len(weights)-1 {
			out[i] = totalMinor - given
			break
		}
		out[i] = totalMinor * w / sum
		given += out[i]
	}
	return out
}
