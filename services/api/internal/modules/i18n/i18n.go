// Package i18n resolves vendor-authored content into a locale.
//
// The distinction from the web app's message catalogue matters and is the
// reason this package exists at all. UI chrome — "Add to basket", "Your
// order" — is written by the team, versioned with the code, and shipped in JSON
// files. Dish names, plan descriptions, branch names and listing titles are
// written by vendors and sellers, and a deploy is not an acceptable
// prerequisite for a kitchen adding a dish in Arabic.
//
// PLAN.md §7 puts it as "Arabic / RTL — not a translation pass. Affects layout,
// date/number formatting, fonts, and the design system." The layout half was
// done in Phase 0, when the web app made locale a route segment and `dir` an
// attribute on <html>. This is the content half.
package i18n

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Source records who wrote a translation.
type Source string

const (
	Human    Source = "human"
	Machine  Source = "machine"
	Imported Source = "imported"
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Key identifies one translatable field.
type Key struct {
	EntityType string
	EntityID   uuid.UUID
	Field      string
}

// Value is a resolved string and what is known about it.
type Value struct {
	Text string
	// Locale actually served, which is not necessarily the locale asked for.
	Locale string
	Source Source
	// Stale means the source-language field changed after this translation was
	// written. Served anyway — a stale Arabic string still beats an English one
	// for an Arabic reader — but the caller can mark it in the console, and the
	// re-translation job finds it here.
	Stale bool
	// Fallback is true when nothing existed in the requested locale.
	Fallback bool
}

// Resolve looks up many fields at once.
//
// Batched rather than one call per field, because the shape of the demand is a
// menu: thirty dishes with a name and a description each is sixty lookups on
// one screen, and sixty round trips is how a localised page becomes visibly
// slower than an English one. A product where Arabic is slower than English is
// a product that has decided which language matters.
func (s *Service) Resolve(ctx context.Context, locale string, keys []Key) (map[Key]Value, error) {
	out := make(map[Key]Value, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	if locale != "ar" && locale != "en" {
		locale = "en"
	}

	// Grouped by entity type so the query stays a small number of index probes
	// rather than one enormous OR chain.
	byType := map[string][]Key{}
	for _, k := range keys {
		byType[k.EntityType] = append(byType[k.EntityType], k)
	}

	for entityType, group := range byType {
		ids := make([]uuid.UUID, 0, len(group))
		fields := make([]string, 0, len(group))
		seenID := map[uuid.UUID]bool{}
		seenField := map[string]bool{}
		for _, k := range group {
			if !seenID[k.EntityID] {
				seenID[k.EntityID] = true
				ids = append(ids, k.EntityID)
			}
			if !seenField[k.Field] {
				seenField[k.Field] = true
				fields = append(fields, k.Field)
			}
		}

		rows, err := s.pool.Query(ctx, `
			SELECT entity_id, field, locale::text, value, source, stale_since IS NOT NULL
			  FROM translations
			 WHERE entity_type = $1 AND entity_id = ANY($2) AND field = ANY($3)
			   AND locale = ANY (ARRAY[$4, 'en']::locale_code[])`,
			entityType, ids, fields, locale)
		if err != nil {
			return nil, fmt.Errorf("i18n: resolve %s: %w", entityType, err)
		}

		for rows.Next() {
			var (
				k     Key
				v     Value
				stale bool
			)
			k.EntityType = entityType
			if err := rows.Scan(&k.EntityID, &k.Field, &v.Locale, &v.Text, &v.Source, &stale); err != nil {
				rows.Close()
				return nil, err
			}
			v.Stale = stale
			v.Fallback = v.Locale != locale

			// The requested locale wins over the fallback whichever order the
			// rows arrive in. Relying on ORDER BY here would work and would
			// break silently the first time someone added a third locale.
			if existing, ok := out[k]; ok && !existing.Fallback && v.Fallback {
				continue
			}
			out[k] = v
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// Set writes a translation.
//
// Clears `stale_since` on write, because writing a translation is the act that
// un-stales it. Does not clear the machine flag — a machine translation edited
// by a human should be re-marked by whoever reviewed it, through Review, so
// that "reviewed" means a person actually read it.
func (s *Service) Set(ctx context.Context, k Key, locale, value string, source Source) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("i18n: an empty translation is a deletion, use Delete")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO translations (entity_type, entity_id, field, locale, value, source, stale_since)
		VALUES ($1, $2, $3, $4::locale_code, $5, $6, NULL)
		ON CONFLICT (entity_type, entity_id, field, locale)
		DO UPDATE SET value = EXCLUDED.value, source = EXCLUDED.source,
		              stale_since = NULL, updated_at = now()`,
		k.EntityType, k.EntityID, k.Field, locale, value, string(source))
	if err != nil {
		return fmt.Errorf("i18n: set: %w", err)
	}
	return nil
}

// MarkStale flags every translation of an entity's field.
//
// Called when the source-language value changes. Without it the failure is
// silent and expensive: a vendor edits an English description, the Arabic one
// keeps the old text, and Arabic-speaking customers read a description of a
// product that is no longer sold. Nobody notices, because the people who would
// notice are the ones who cannot read the version that changed.
func (s *Service) MarkStale(ctx context.Context, entityType string, entityID uuid.UUID, field string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE translations SET stale_since = coalesce(stale_since, now())
		 WHERE entity_type = $1 AND entity_id = $2 AND field = $3`,
		entityType, entityID, field)
	if err != nil {
		return fmt.Errorf("i18n: mark stale: %w", err)
	}
	return nil
}

// MarkStaleTx is the same, inside a caller's transaction — which is where it
// belongs. The edit and the staleness flag are one change; splitting them
// leaves a window in which the translation looks current and is not.
func MarkStaleTx(ctx context.Context, tx pgx.Tx, entityType string, entityID uuid.UUID, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE translations SET stale_since = coalesce(stale_since, now())
		 WHERE entity_type = $1 AND entity_id = $2 AND field = ANY($3)`,
		entityType, entityID, fields)
	if err != nil {
		return fmt.Errorf("i18n: mark stale: %w", err)
	}
	return nil
}

// Review marks a machine translation as read by a person.
func (s *Service) Review(ctx context.Context, k Key, locale string, reviewerID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE translations SET reviewed_by = $5, reviewed_at = now(), source = 'human'
		 WHERE entity_type = $1 AND entity_id = $2 AND field = $3 AND locale = $4::locale_code`,
		k.EntityType, k.EntityID, k.Field, locale, reviewerID)
	if err != nil {
		return fmt.Errorf("i18n: review: %w", err)
	}
	return nil
}

// Coverage reports how much of an entity type is translated.
//
// The number the console shows a vendor and the number the content operator
// works from. Counting rows rather than sampling, because the interesting cases
// are the long tail: a vendor with 95% coverage still has two dishes that read
// as English to an Arabic customer, and those two are the ones nobody ordered.
type Coverage struct {
	EntityType string
	Locale     string
	Translated int
	Stale      int
	Unreviewed int
}

func (s *Service) Coverage(ctx context.Context, locale string) ([]Coverage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT entity_type,
		       count(*),
		       count(*) FILTER (WHERE stale_since IS NOT NULL),
		       count(*) FILTER (WHERE source = 'machine' AND reviewed_at IS NULL)
		  FROM translations
		 WHERE locale = $1::locale_code
		 GROUP BY entity_type
		 ORDER BY entity_type`, locale)
	if err != nil {
		return nil, fmt.Errorf("i18n: coverage: %w", err)
	}
	defer rows.Close()

	var out []Coverage
	for rows.Next() {
		c := Coverage{Locale: locale}
		if err := rows.Scan(&c.EntityType, &c.Translated, &c.Stale, &c.Unreviewed); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
