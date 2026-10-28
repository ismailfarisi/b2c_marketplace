// Package merch is meals merchandising: kitchen collections, banners and the
// storefront home layout that arranges them. Staff edit; customers read the
// resolved result for their location.
package merch

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/kitchen"
)

// Rules pick kitchens for a collection. Each list means "any of"; an empty
// rule set matches every kitchen that delivers to the customer.
type Rules struct {
	Cuisines      []string `json:"cuisines,omitempty"`
	Dietary       []string `json:"dietary,omitempty"`
	Slots         []string `json:"slots,omitempty"`
	RotationOnly  bool     `json:"rotation_only,omitempty"`
	MaxPriceMinor *int64   `json:"max_price_minor,omitempty"`
}

type Item struct {
	BranchID uuid.UUID `json:"branch_id"`
	Role     string    `json:"role"`
	Position int       `json:"position"`
}

type Collection struct {
	ID         uuid.UUID  `json:"id"`
	Slug       string     `json:"slug"`
	Title      string     `json:"title"`
	TitleAr    *string    `json:"title_ar"`
	Subtitle   *string    `json:"subtitle"`
	SubtitleAr *string    `json:"subtitle_ar"`
	Rules      Rules      `json:"rules"`
	Sort       string     `json:"sort"`
	MaxItems   *int       `json:"max_items"`
	StartsAt   *time.Time `json:"starts_at"`
	EndsAt     *time.Time `json:"ends_at"`
	IsActive   bool       `json:"is_active"`
	Items      []Item     `json:"items"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type CollectionInput struct {
	Slug       string     `json:"slug"`
	Title      string     `json:"title"`
	TitleAr    *string    `json:"title_ar"`
	Subtitle   *string    `json:"subtitle"`
	SubtitleAr *string    `json:"subtitle_ar"`
	Rules      Rules      `json:"rules"`
	Sort       string     `json:"sort"`
	MaxItems   *int       `json:"max_items"`
	StartsAt   *time.Time `json:"starts_at"`
	EndsAt     *time.Time `json:"ends_at"`
	IsActive   bool       `json:"is_active"`
}

type Banner struct {
	ID        uuid.UUID  `json:"id"`
	Title     string     `json:"title"`
	TitleAr   *string    `json:"title_ar"`
	Body      *string    `json:"body"`
	BodyAr    *string    `json:"body_ar"`
	CTA       *string    `json:"cta"`
	CTAAr     *string    `json:"cta_ar"`
	ImageURL  *string    `json:"image_url"`
	LinkURL   *string    `json:"link_url"`
	StartsAt  *time.Time `json:"starts_at"`
	EndsAt    *time.Time `json:"ends_at"`
	IsActive  bool       `json:"is_active"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type BannerInput struct {
	Title    string     `json:"title"`
	TitleAr  *string    `json:"title_ar"`
	Body     *string    `json:"body"`
	BodyAr   *string    `json:"body_ar"`
	CTA      *string    `json:"cta"`
	CTAAr    *string    `json:"cta_ar"`
	ImageURL *string    `json:"image_url"`
	LinkURL  *string    `json:"link_url"`
	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
	IsActive bool       `json:"is_active"`
}

// Section types the storefront knows how to render.
const (
	SectionBanners    = "banner_carousel"
	SectionRail       = "kitchen_rail"
	SectionCuisines   = "cuisine_strip"
	SectionRotation   = "rotation_promo"
	SectionAllKitchen = "all_kitchens"
)

var SectionTypes = []string{SectionBanners, SectionRail, SectionCuisines, SectionRotation, SectionAllKitchen}

type SectionSettings struct {
	Title        string      `json:"title,omitempty"`
	TitleAr      string      `json:"title_ar,omitempty"`
	CollectionID *uuid.UUID  `json:"collection_id,omitempty"`
	BannerIDs    []uuid.UUID `json:"banner_ids,omitempty"`
}

type Section struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Enabled  bool            `json:"enabled"`
	Settings SectionSettings `json:"settings"`
}

const maxSections = 20

var (
	slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	sorts  = []string{"rating", "price_asc", "price_desc", "newest"}
)

func fieldErr(field, code, msg string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: code, Message: msg}}}
}

func lowerSet(in []string) []string {
	out := []string{}
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(*p)
	if s == "" {
		return nil
	}
	return &s
}

func NormaliseCollection(in CollectionInput) (CollectionInput, error) {
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	in.Title = strings.TrimSpace(in.Title)
	in.TitleAr, in.Subtitle, in.SubtitleAr = trimPtr(in.TitleAr), trimPtr(in.Subtitle), trimPtr(in.SubtitleAr)
	var fields []httpx.FieldError
	if !slugRe.MatchString(in.Slug) || len(in.Slug) > 60 {
		fields = append(fields, httpx.FieldError{Field: "slug", Code: "invalid", Message: "lowercase letters, digits and dashes"})
	}
	if in.Title == "" || len(in.Title) > 80 {
		fields = append(fields, httpx.FieldError{Field: "title", Code: "required"})
	}
	if in.Sort == "" {
		in.Sort = "rating"
	}
	if !slices.Contains(sorts, in.Sort) {
		fields = append(fields, httpx.FieldError{Field: "sort", Code: "invalid"})
	}
	if in.MaxItems != nil && (*in.MaxItems < 1 || *in.MaxItems > 48) {
		fields = append(fields, httpx.FieldError{Field: "max_items", Code: "out_of_range", Message: "1 to 48"})
	}
	if in.StartsAt != nil && in.EndsAt != nil && !in.EndsAt.After(*in.StartsAt) {
		fields = append(fields, httpx.FieldError{Field: "ends_at", Code: "before_start"})
	}
	in.Rules.Cuisines, in.Rules.Dietary, in.Rules.Slots = lowerSet(in.Rules.Cuisines), lowerSet(in.Rules.Dietary), lowerSet(in.Rules.Slots)
	for _, s := range in.Rules.Slots {
		if !slices.Contains(kitchen.Slots, s) {
			fields = append(fields, httpx.FieldError{Field: "rules.slots", Code: "invalid_slot"})
			break
		}
	}
	if p := in.Rules.MaxPriceMinor; p != nil && *p <= 0 {
		fields = append(fields, httpx.FieldError{Field: "rules.max_price_minor", Code: "out_of_range"})
	}
	if len(fields) > 0 {
		return in, &httpx.ValidationError{Fields: fields}
	}
	return in, nil
}

func NormaliseBanner(in BannerInput) (BannerInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.TitleAr, in.Body, in.BodyAr = trimPtr(in.TitleAr), trimPtr(in.Body), trimPtr(in.BodyAr)
	in.CTA, in.CTAAr, in.ImageURL, in.LinkURL = trimPtr(in.CTA), trimPtr(in.CTAAr), trimPtr(in.ImageURL), trimPtr(in.LinkURL)
	var fields []httpx.FieldError
	if in.Title == "" || len(in.Title) > 100 {
		fields = append(fields, httpx.FieldError{Field: "title", Code: "required"})
	}
	if u := in.ImageURL; u != nil && !strings.HasPrefix(*u, "https://") {
		fields = append(fields, httpx.FieldError{Field: "image_url", Code: "invalid", Message: "must be an https URL"})
	}
	// Links stay on the storefront or go to an https page; never javascript:.
	if u := in.LinkURL; u != nil && !strings.HasPrefix(*u, "/") && !strings.HasPrefix(*u, "https://") {
		fields = append(fields, httpx.FieldError{Field: "link_url", Code: "invalid", Message: "a /path or an https URL"})
	}
	if in.StartsAt != nil && in.EndsAt != nil && !in.EndsAt.After(*in.StartsAt) {
		fields = append(fields, httpx.FieldError{Field: "ends_at", Code: "before_start"})
	}
	if len(fields) > 0 {
		return in, &httpx.ValidationError{Fields: fields}
	}
	return in, nil
}

func NormaliseItems(items []Item) ([]Item, error) {
	seen := map[uuid.UUID]bool{}
	out := make([]Item, 0, len(items))
	for i, it := range items {
		if it.Role != "pin" && it.Role != "exclude" {
			return nil, fieldErr("items", "invalid_role", "pin or exclude")
		}
		if it.BranchID == uuid.Nil || seen[it.BranchID] {
			return nil, fieldErr("items", "duplicate", "each kitchen once")
		}
		seen[it.BranchID] = true
		it.Position = i
		out = append(out, it)
	}
	if len(out) > 100 {
		return nil, fieldErr("items", "too_many", "at most 100")
	}
	return out, nil
}

func NormaliseSections(in []Section) ([]Section, error) {
	if len(in) > maxSections {
		return nil, fieldErr("sections", "too_many", "at most 20 sections")
	}
	out := make([]Section, 0, len(in))
	ids := map[string]bool{}
	for _, s := range in {
		if !slices.Contains(SectionTypes, s.Type) {
			return nil, fieldErr("sections", "invalid_type", s.Type)
		}
		s.ID = strings.TrimSpace(s.ID)
		if s.ID == "" || ids[s.ID] {
			s.ID = uuid.NewString()
		}
		ids[s.ID] = true
		s.Settings.Title = strings.TrimSpace(s.Settings.Title)
		s.Settings.TitleAr = strings.TrimSpace(s.Settings.TitleAr)
		switch s.Type {
		case SectionRail:
			if s.Settings.CollectionID == nil {
				return nil, fieldErr("sections", "collection_required", "a kitchen rail needs a collection")
			}
			s.Settings.BannerIDs = nil
		case SectionBanners:
			s.Settings.CollectionID = nil
		default:
			s.Settings.CollectionID, s.Settings.BannerIDs = nil, nil
		}
		out = append(out, s)
	}
	return out, nil
}

// Matches reports whether a kitchen satisfies a collection's rules.
func (r Rules) Matches(k kitchen.PublicKitchen) bool {
	any := func(want, have []string) bool {
		if len(want) == 0 {
			return true
		}
		for _, w := range want {
			if slices.Contains(have, w) {
				return true
			}
		}
		return false
	}
	if !any(r.Cuisines, k.Cuisines) || !any(r.Dietary, k.DietaryTags) || !any(r.Slots, k.MealSlots) {
		return false
	}
	if r.RotationOnly && k.RotationWeekPriceMinor == nil {
		return false
	}
	if r.MaxPriceMinor != nil && k.FromPriceMinor > *r.MaxPriceMinor {
		return false
	}
	return true
}

// Arrange turns the kitchens that deliver here into a collection: pins first
// in their order, then rule matches by the collection's sort, excludes
// removed, capped at max_items. `newest` is a creation order supplied by the
// caller since PublicKitchen carries no timestamp.
func Arrange(c Collection, all []kitchen.PublicKitchen, newest map[uuid.UUID]int) []kitchen.PublicKitchen {
	byID := make(map[uuid.UUID]kitchen.PublicKitchen, len(all))
	for _, k := range all {
		byID[k.BranchID] = k
	}
	excluded := map[uuid.UUID]bool{}
	var pins []Item
	for _, it := range c.Items {
		if it.Role == "exclude" {
			excluded[it.BranchID] = true
		} else {
			pins = append(pins, it)
		}
	}
	slices.SortStableFunc(pins, func(a, b Item) int { return cmp.Compare(a.Position, b.Position) })
	out := []kitchen.PublicKitchen{}
	taken := map[uuid.UUID]bool{}
	for _, p := range pins {
		if k, ok := byID[p.BranchID]; ok && !excluded[p.BranchID] {
			out = append(out, k)
			taken[p.BranchID] = true
		}
	}
	var rest []kitchen.PublicKitchen
	for _, k := range all {
		if !taken[k.BranchID] && !excluded[k.BranchID] && c.Rules.Matches(k) {
			rest = append(rest, k)
		}
	}
	switch c.Sort {
	case "price_asc":
		slices.SortStableFunc(rest, func(a, b kitchen.PublicKitchen) int { return cmp.Compare(a.FromPriceMinor, b.FromPriceMinor) })
	case "price_desc":
		slices.SortStableFunc(rest, func(a, b kitchen.PublicKitchen) int { return cmp.Compare(b.FromPriceMinor, a.FromPriceMinor) })
	case "newest":
		slices.SortStableFunc(rest, func(a, b kitchen.PublicKitchen) int { return cmp.Compare(newest[a.BranchID], newest[b.BranchID]) })
	}
	out = append(out, rest...)
	if c.MaxItems != nil && len(out) > *c.MaxItems {
		out = out[:*c.MaxItems]
	}
	return out
}

func live(active bool, starts, ends *time.Time, now time.Time) bool {
	return active && (starts == nil || !now.Before(*starts)) && (ends == nil || now.Before(*ends))
}

func slicesSortCuisines(cs []CuisineCount) {
	slices.SortStableFunc(cs, func(a, b CuisineCount) int {
		if c := cmp.Compare(b.Kitchens, a.Kitchens); c != 0 {
			return c
		}
		return cmp.Compare(a.Cuisine, b.Cuisine)
	})
}
