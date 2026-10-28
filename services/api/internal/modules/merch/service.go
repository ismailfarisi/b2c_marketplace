package merch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/kitchen"
)

const HomeLayout = "meals_home"

type Service struct {
	pool    *pgxpool.Pool
	kitchen *kitchen.Service
	now     func() time.Time
}

func NewService(pool *pgxpool.Pool, k *kitchen.Service) *Service {
	return &Service{pool: pool, kitchen: k, now: time.Now}
}

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// ---- collections -------------------------------------------------------

const collectionCols = `id, slug, title, title_ar, subtitle, subtitle_ar, rules, sort, max_items,
	starts_at, ends_at, is_active, updated_at`

func scanCollection(row pgx.Row) (Collection, error) {
	var c Collection
	var rules []byte
	if err := row.Scan(&c.ID, &c.Slug, &c.Title, &c.TitleAr, &c.Subtitle, &c.SubtitleAr, &rules, &c.Sort,
		&c.MaxItems, &c.StartsAt, &c.EndsAt, &c.IsActive, &c.UpdatedAt); err != nil {
		return c, err
	}
	c.Items = []Item{}
	return c, json.Unmarshal(rules, &c.Rules)
}

func (s *Service) loadItems(ctx context.Context, cs []Collection) error {
	for i := range cs {
		rows, err := s.pool.Query(ctx, `SELECT branch_id, role, position FROM merch_collection_items
			 WHERE collection_id = $1 ORDER BY role, position`, cs[i].ID)
		if err != nil {
			return fmt.Errorf("merch: items: %w", err)
		}
		items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Item, error) {
			var it Item
			return it, r.Scan(&it.BranchID, &it.Role, &it.Position)
		})
		if err != nil {
			return fmt.Errorf("merch: items: %w", err)
		}
		cs[i].Items = items
	}
	return nil
}

func (s *Service) Collections(ctx context.Context) ([]Collection, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+collectionCols+` FROM merch_collections
		 WHERE deleted_at IS NULL ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("merch: collections: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Collection, error) { return scanCollection(r) })
	if err != nil {
		return nil, fmt.Errorf("merch: collections: %w", err)
	}
	return out, s.loadItems(ctx, out)
}

func (s *Service) collectionWhere(ctx context.Context, where string, arg any) (Collection, error) {
	c, err := scanCollection(s.pool.QueryRow(ctx, `SELECT `+collectionCols+` FROM merch_collections
		 WHERE deleted_at IS NULL AND `+where, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httpx.ErrNotFound
	}
	if err != nil {
		return c, fmt.Errorf("merch: collection: %w", err)
	}
	one := []Collection{c}
	err = s.loadItems(ctx, one)
	return one[0], err
}

func (s *Service) Collection(ctx context.Context, id uuid.UUID) (Collection, error) {
	return s.collectionWhere(ctx, "id = $1", id)
}

func (s *Service) SaveCollection(ctx context.Context, id *uuid.UUID, in CollectionInput) (Collection, error) {
	in, err := NormaliseCollection(in)
	if err != nil {
		return Collection{}, err
	}
	rules, _ := json.Marshal(in.Rules)
	args := []any{in.Slug, in.Title, in.TitleAr, in.Subtitle, in.SubtitleAr, rules, in.Sort, in.MaxItems,
		in.StartsAt, in.EndsAt, in.IsActive}
	var newID uuid.UUID
	if id == nil {
		err = s.pool.QueryRow(ctx, `INSERT INTO merch_collections
			(slug, title, title_ar, subtitle, subtitle_ar, rules, sort, max_items, starts_at, ends_at, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`, args...).Scan(&newID)
	} else {
		err = s.pool.QueryRow(ctx, `UPDATE merch_collections SET slug=$1, title=$2, title_ar=$3, subtitle=$4,
			subtitle_ar=$5, rules=$6, sort=$7, max_items=$8, starts_at=$9, ends_at=$10, is_active=$11, updated_at=now()
			WHERE id=$12 AND deleted_at IS NULL RETURNING id`, append(args, *id)...).Scan(&newID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Collection{}, httpx.ErrNotFound
	}
	if isUniqueViolation(err) {
		return Collection{}, fieldErr("slug", "taken", "another collection uses this slug")
	}
	if err != nil {
		return Collection{}, fmt.Errorf("merch: save collection: %w", err)
	}
	return s.Collection(ctx, newID)
}

func (s *Service) SetItems(ctx context.Context, id uuid.UUID, items []Item) (Collection, error) {
	items, err := NormaliseItems(items)
	if err != nil {
		return Collection{}, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE merch_collections SET updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM merch_collection_items WHERE collection_id = $1`, id); err != nil {
			return err
		}
		for _, it := range items {
			if _, err := tx.Exec(ctx, `INSERT INTO merch_collection_items (collection_id, branch_id, role, position)
				SELECT $1, b.id, $3, $4 FROM vendor_branches b WHERE b.id = $2 AND b.deleted_at IS NULL`,
				id, it.BranchID, it.Role, it.Position); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, httpx.ErrNotFound) {
			return Collection{}, err
		}
		return Collection{}, fmt.Errorf("merch: set items: %w", err)
	}
	return s.Collection(ctx, id)
}

func (s *Service) DeleteCollection(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE merch_collections SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("merch: delete collection: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

// newestOrder ranks branches newest first, for the `newest` sort.
func (s *Service) newestOrder(ctx context.Context, ks []kitchen.PublicKitchen) (map[uuid.UUID]int, error) {
	ids := make([]uuid.UUID, len(ks))
	for i, k := range ks {
		ids[i] = k.BranchID
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM vendor_branches WHERE id = ANY($1) ORDER BY created_at DESC, id`, ids)
	if err != nil {
		return nil, fmt.Errorf("merch: newest: %w", err)
	}
	order, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("merch: newest: %w", err)
	}
	out := make(map[uuid.UUID]int, len(order))
	for i, id := range order {
		out[id] = i
	}
	return out, nil
}

// Resolve lists a collection's kitchens for a location. It starts from the
// same discovery the storefront uses, so a collection can never show a
// kitchen that is not sellable or does not deliver here.
func (s *Service) Resolve(ctx context.Context, c Collection, lat, lng *float64) ([]kitchen.PublicKitchen, error) {
	all, err := s.kitchen.Discover(ctx, kitchen.DiscoverFilter{Lat: lat, Lng: lng})
	if err != nil {
		return nil, err
	}
	return s.arrange(ctx, c, all)
}

func (s *Service) arrange(ctx context.Context, c Collection, all []kitchen.PublicKitchen) ([]kitchen.PublicKitchen, error) {
	var newest map[uuid.UUID]int
	if c.Sort == "newest" {
		var err error
		if newest, err = s.newestOrder(ctx, all); err != nil {
			return nil, err
		}
	}
	return Arrange(c, all, newest), nil
}

type PublicCollection struct {
	Collection
	Kitchens []kitchen.PublicKitchen `json:"kitchens"`
}

// PublicCollection is the collection page. Inactive or out-of-window
// collections are a 404 so an expired campaign link does not show a page.
func (s *Service) PublicCollection(ctx context.Context, slug string, lat, lng *float64) (PublicCollection, error) {
	c, err := s.collectionWhere(ctx, "slug = $1", slug)
	if err != nil {
		return PublicCollection{}, err
	}
	if !live(c.IsActive, c.StartsAt, c.EndsAt, s.now()) {
		return PublicCollection{}, httpx.ErrNotFound
	}
	ks, err := s.Resolve(ctx, c, lat, lng)
	if err != nil {
		return PublicCollection{}, err
	}
	c.Items = []Item{}
	return PublicCollection{Collection: c, Kitchens: ks}, nil
}

// ---- banners -----------------------------------------------------------

const bannerCols = `id, title, title_ar, body, body_ar, cta, cta_ar, image_url, link_url,
	starts_at, ends_at, is_active, updated_at`

func scanBanner(row pgx.Row) (Banner, error) {
	var b Banner
	return b, row.Scan(&b.ID, &b.Title, &b.TitleAr, &b.Body, &b.BodyAr, &b.CTA, &b.CTAAr, &b.ImageURL,
		&b.LinkURL, &b.StartsAt, &b.EndsAt, &b.IsActive, &b.UpdatedAt)
}

func (s *Service) Banners(ctx context.Context) ([]Banner, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+bannerCols+` FROM merch_banners WHERE deleted_at IS NULL ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("merch: banners: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Banner, error) { return scanBanner(r) })
	if err != nil {
		return nil, fmt.Errorf("merch: banners: %w", err)
	}
	return out, nil
}

func (s *Service) SaveBanner(ctx context.Context, id *uuid.UUID, in BannerInput) (Banner, error) {
	in, err := NormaliseBanner(in)
	if err != nil {
		return Banner{}, err
	}
	args := []any{in.Title, in.TitleAr, in.Body, in.BodyAr, in.CTA, in.CTAAr, in.ImageURL, in.LinkURL,
		in.StartsAt, in.EndsAt, in.IsActive}
	var b Banner
	if id == nil {
		b, err = scanBanner(s.pool.QueryRow(ctx, `INSERT INTO merch_banners
			(title, title_ar, body, body_ar, cta, cta_ar, image_url, link_url, starts_at, ends_at, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+bannerCols, args...))
	} else {
		b, err = scanBanner(s.pool.QueryRow(ctx, `UPDATE merch_banners SET title=$1, title_ar=$2, body=$3,
			body_ar=$4, cta=$5, cta_ar=$6, image_url=$7, link_url=$8, starts_at=$9, ends_at=$10, is_active=$11,
			updated_at=now() WHERE id=$12 AND deleted_at IS NULL RETURNING `+bannerCols, append(args, *id)...))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Banner{}, httpx.ErrNotFound
	}
	if err != nil {
		return Banner{}, fmt.Errorf("merch: save banner: %w", err)
	}
	return b, nil
}

func (s *Service) DeleteBanner(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE merch_banners SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("merch: delete banner: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

// ---- layout ------------------------------------------------------------

type Version struct {
	ID            uuid.UUID  `json:"id"`
	VersionNumber int        `json:"version_number"`
	Status        string     `json:"status"`
	Sections      []Section  `json:"sections"`
	PublishAt     *time.Time `json:"publish_at"`
	PublishedBy   *uuid.UUID `json:"published_by"`
	Note          *string    `json:"note"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type LayoutState struct {
	Draft    *Version   `json:"draft"`
	LiveID   *uuid.UUID `json:"live_id"`
	Versions []Version  `json:"versions"`
	Types    []string   `json:"section_types"`
}

const versionCols = `v.id, v.version_number, v.status, v.sections, v.publish_at, v.published_by, v.note, v.updated_at`

func scanVersion(row pgx.Row) (Version, error) {
	var v Version
	var raw []byte
	if err := row.Scan(&v.ID, &v.VersionNumber, &v.Status, &raw, &v.PublishAt, &v.PublishedBy, &v.Note, &v.UpdatedAt); err != nil {
		return v, err
	}
	v.Sections = []Section{}
	return v, json.Unmarshal(raw, &v.Sections)
}

const layoutJoin = ` FROM merch_layout_versions v JOIN merch_layouts l ON l.id = v.layout_id WHERE l.slug = $1`

func (s *Service) liveVersion(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, slug string) (*Version, error) {
	v, err := scanVersion(q.QueryRow(ctx, `SELECT `+versionCols+layoutJoin+`
		   AND v.status = 'published' AND v.publish_at <= $2
		 ORDER BY v.publish_at DESC, v.version_number DESC LIMIT 1`, slug, s.now()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("merch: live version: %w", err)
	}
	return &v, nil
}

func (s *Service) Layout(ctx context.Context, slug string) (LayoutState, error) {
	st := LayoutState{Versions: []Version{}, Types: SectionTypes}
	rows, err := s.pool.Query(ctx, `SELECT `+versionCols+layoutJoin+` ORDER BY v.version_number DESC LIMIT 30`, slug)
	if err != nil {
		return st, fmt.Errorf("merch: layout: %w", err)
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Version, error) { return scanVersion(r) })
	if err != nil {
		return st, fmt.Errorf("merch: layout: %w", err)
	}
	for i := range all {
		if all[i].Status == "draft" {
			st.Draft = &all[i]
		} else {
			st.Versions = append(st.Versions, all[i])
		}
	}
	lv, err := s.liveVersion(ctx, s.pool, slug)
	if err != nil {
		return st, err
	}
	if lv != nil {
		st.LiveID = &lv.ID
	}
	return st, nil
}

func (s *Service) SaveDraft(ctx context.Context, slug string, sections []Section) (Version, error) {
	sections, err := NormaliseSections(sections)
	if err != nil {
		return Version{}, err
	}
	if err := s.checkRefs(ctx, sections); err != nil {
		return Version{}, err
	}
	raw, _ := json.Marshal(sections)
	var v Version
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var layoutID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM merch_layouts WHERE slug = $1 FOR UPDATE`, slug).Scan(&layoutID); err != nil {
			return err
		}
		var err error
		v, err = scanVersion(tx.QueryRow(ctx, `WITH up AS (
				UPDATE merch_layout_versions SET sections = $2, updated_at = now()
				 WHERE layout_id = $1 AND status = 'draft' RETURNING *),
			ins AS (
				INSERT INTO merch_layout_versions (layout_id, version_number, status, sections)
				SELECT $1, COALESCE((SELECT max(version_number) FROM merch_layout_versions WHERE layout_id = $1), 0) + 1, 'draft', $2
				 WHERE NOT EXISTS (SELECT 1 FROM up) RETURNING *)
			SELECT `+versionCols+` FROM (SELECT * FROM up UNION ALL SELECT * FROM ins) v`, layoutID, raw))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, httpx.ErrNotFound
	}
	if err != nil {
		return Version{}, fmt.Errorf("merch: save draft: %w", err)
	}
	return v, nil
}

// checkRefs refuses sections that point at deleted collections or banners.
func (s *Service) checkRefs(ctx context.Context, sections []Section) error {
	for _, sec := range sections {
		if id := sec.Settings.CollectionID; id != nil {
			if _, err := s.Collection(ctx, *id); errors.Is(err, httpx.ErrNotFound) {
				return fieldErr("sections", "unknown_collection", id.String())
			} else if err != nil {
				return err
			}
		}
		if len(sec.Settings.BannerIDs) > 0 {
			var n int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM merch_banners WHERE id = ANY($1) AND deleted_at IS NULL`,
				sec.Settings.BannerIDs).Scan(&n); err != nil {
				return fmt.Errorf("merch: check banners: %w", err)
			}
			if n != len(sec.Settings.BannerIDs) {
				return fieldErr("sections", "unknown_banner", "a banner in this section was deleted")
			}
		}
	}
	return nil
}

// Publish turns the draft into a published version, live now or at publishAt.
func (s *Service) Publish(ctx context.Context, slug string, publishAt *time.Time, by uuid.UUID, note string) (Version, error) {
	at := s.now()
	if publishAt != nil {
		if publishAt.Before(at.Add(-time.Minute)) {
			return Version{}, fieldErr("publish_at", "in_past", "schedule a future time or publish now")
		}
		at = *publishAt
	}
	var notePtr *string
	if note != "" {
		notePtr = &note
	}
	v, err := scanVersion(s.pool.QueryRow(ctx, `UPDATE merch_layout_versions v SET status = 'published',
			publish_at = $2, published_by = $3, note = $4, updated_at = now()
		  FROM merch_layouts l WHERE l.id = v.layout_id AND l.slug = $1 AND v.status = 'draft'
		RETURNING `+versionCols, slug, at, by, notePtr))
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, fieldErr("draft", "none", "nothing to publish: save a draft first")
	}
	if err != nil {
		return Version{}, fmt.Errorf("merch: publish: %w", err)
	}
	return v, nil
}

// Restore copies an older version into the draft, to review and publish.
func (s *Service) Restore(ctx context.Context, slug string, versionID uuid.UUID) (Version, error) {
	old, err := scanVersion(s.pool.QueryRow(ctx, `SELECT `+versionCols+layoutJoin+` AND v.id = $2`, slug, versionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, httpx.ErrNotFound
	}
	if err != nil {
		return Version{}, fmt.Errorf("merch: restore: %w", err)
	}
	return s.SaveDraft(ctx, slug, old.Sections)
}

// ---- storefront home ---------------------------------------------------

type HomeSection struct {
	Section
	Collection *Collection             `json:"collection,omitempty"`
	Kitchens   []kitchen.PublicKitchen `json:"kitchens,omitempty"`
	Banners    []Banner                `json:"banners,omitempty"`
	Cuisines   []CuisineCount          `json:"cuisines,omitempty"`
}

type CuisineCount struct {
	Cuisine  string `json:"cuisine"`
	Kitchens int    `json:"kitchens"`
}

type Home struct {
	VersionID *uuid.UUID    `json:"version_id"`
	Sections  []HomeSection `json:"sections"`
}

// Home resolves the live layout for a location. Sections that resolve to
// nothing (a rail with no kitchens here, a carousel whose banners all
// expired) are dropped so the page never shows an empty shelf. With nothing
// published, Sections is null and the client renders its default home.
func (s *Service) Home(ctx context.Context, lat, lng *float64) (Home, error) {
	return s.resolve(ctx, nil, lat, lng)
}

// Preview resolves a draft or any version as Home would, for staff.
func (s *Service) Preview(ctx context.Context, sections []Section, lat, lng *float64) (Home, error) {
	if sections == nil {
		sections = []Section{}
	}
	return s.resolve(ctx, &sections, lat, lng)
}

func (s *Service) resolve(ctx context.Context, override *[]Section, lat, lng *float64) (Home, error) {
	var h Home
	var sections []Section
	if override != nil {
		sections = *override
	} else {
		v, err := s.liveVersion(ctx, s.pool, HomeLayout)
		if err != nil || v == nil {
			return h, err
		}
		h.VersionID, sections = &v.ID, v.Sections
	}
	h.Sections = []HomeSection{}
	now := s.now()
	var all []kitchen.PublicKitchen
	loaded := false
	kitchens := func() ([]kitchen.PublicKitchen, error) {
		if loaded {
			return all, nil
		}
		var err error
		all, err = s.kitchen.Discover(ctx, kitchen.DiscoverFilter{Lat: lat, Lng: lng})
		loaded = err == nil
		return all, err
	}
	var banners map[uuid.UUID]Banner
	for _, sec := range sections {
		if !sec.Enabled {
			continue
		}
		hs := HomeSection{Section: sec}
		switch sec.Type {
		case SectionRail:
			c, err := s.Collection(ctx, *sec.Settings.CollectionID)
			if errors.Is(err, httpx.ErrNotFound) {
				continue
			}
			if err != nil {
				return h, err
			}
			if !live(c.IsActive, c.StartsAt, c.EndsAt, now) {
				continue
			}
			ks, err := kitchens()
			if err != nil {
				return h, err
			}
			if hs.Kitchens, err = s.arrange(ctx, c, ks); err != nil {
				return h, err
			}
			if len(hs.Kitchens) == 0 {
				continue
			}
			c.Items = nil
			hs.Collection = &c
		case SectionBanners:
			if banners == nil {
				list, err := s.Banners(ctx)
				if err != nil {
					return h, err
				}
				banners = map[uuid.UUID]Banner{}
				for _, b := range list {
					if live(b.IsActive, b.StartsAt, b.EndsAt, now) {
						banners[b.ID] = b
					}
				}
			}
			for _, id := range sec.Settings.BannerIDs {
				if b, ok := banners[id]; ok {
					hs.Banners = append(hs.Banners, b)
				}
			}
			if len(hs.Banners) == 0 {
				continue
			}
		case SectionCuisines:
			ks, err := kitchens()
			if err != nil {
				return h, err
			}
			hs.Cuisines = countCuisines(ks)
			if len(hs.Cuisines) == 0 {
				continue
			}
		}
		h.Sections = append(h.Sections, hs)
	}
	return h, nil
}

func countCuisines(ks []kitchen.PublicKitchen) []CuisineCount {
	idx := map[string]int{}
	out := []CuisineCount{}
	for _, k := range ks {
		for _, c := range k.Cuisines {
			if i, ok := idx[c]; ok {
				out[i].Kitchens++
			} else {
				idx[c] = len(out)
				out = append(out, CuisineCount{Cuisine: c, Kitchens: 1})
			}
		}
	}
	slicesSortCuisines(out)
	return out
}
