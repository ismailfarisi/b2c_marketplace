package admin

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Versioned configuration — PLAN-ADMIN-CONSOLE §4.5.
//
// Three tables that the first pass deliberately left read-only, with the reason
// recorded: editing any of them in place would restate documents already issued.
// This file builds the editor that understands that, rather than the one that
// was easy.
//
// **Tax rates and boost rate cards** carry a `daterange` behind a GiST exclusion
// constraint. A change is not an UPDATE — it is closing the current row at a
// date and opening a new one starting the next day, in one transaction. The
// exclusion constraint is the safety net rather than the mechanism: it will
// refuse an overlap, but only after the caller has already decided what the two
// ranges should be, and getting that decision wrong by a day is how a VAT rate
// silently applies to yesterday.
//
// **Notification templates** are versioned by an integer per (key, channel,
// locale). A change writes version n+1 and deactivates n, so a message already
// sent can still be shown as the text that was actually sent.
//
// One rule runs through all three: **effective dates are never in the past.**
// Backdating a rate change rewrites what was charged on invoices that are
// already with customers, and there is no version of that which is a
// configuration edit rather than an accounting correction.

// ── tax rates ───────────────────────────────────────────────────────────────

// TaxRate is one rate over one period.
type TaxRate struct {
	ID       uuid.UUID
	Country  string
	Region   string
	Category string
	RateBP   int
	Label    string
	From     time.Time
	To       *time.Time
	// Current is computed rather than stored: a row is current when today falls
	// inside its range. Storing it would need a job to flip the flag at midnight,
	// and a flag that is one cron failure away from wrong is worse than a
	// comparison.
	Current bool
}

func (s *Service) ListTaxRates(ctx context.Context, actorID uuid.UUID) ([]TaxRate, error) {
	if _, err := s.Require(ctx, actorID, PermHealthRead); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, country, coalesce(region, ''), category, rate_bp, label,
		       lower(valid), upper(valid),
		       valid @> current_date
		  FROM tax_rates
		 ORDER BY country, category, lower(valid) DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tax rates: %w", err)
	}
	defer rows.Close()

	var out []TaxRate
	for rows.Next() {
		var t TaxRate
		if err := rows.Scan(&t.ID, &t.Country, &t.Region, &t.Category, &t.RateBP,
			&t.Label, &t.From, &t.To, &t.Current); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SupersedeTaxRateInput opens a new rate and closes the one it replaces.
type SupersedeTaxRateInput struct {
	// ReplacesID is the row being closed. Named rather than inferred from
	// (country, category) so that the operator is stating which row they looked
	// at — inference would silently pick a different one if two exist for
	// different regions.
	ReplacesID uuid.UUID
	RateBP     int
	Label      string
	// EffectiveFrom is the first day the new rate applies. The old row is closed
	// the instant before.
	EffectiveFrom time.Time
	Reason        string
}

// SupersedeTaxRate closes one rate and opens its replacement, atomically.
func (s *Service) SupersedeTaxRate(ctx context.Context, actorID uuid.UUID, in SupersedeTaxRateInput) error {
	if _, err := s.Require(ctx, actorID, PermConfigWrite); err != nil {
		return err
	}
	if err := requireReason(in.Reason,
		"a tax rate change is quoted back by an auditor — the reason is the record of who "+
			"decided it and on what authority"); err != nil {
		return err
	}
	if in.RateBP < 0 || in.RateBP > 10_000 {
		return field("rate_bp", "range", "a rate outside 0–100% is not a rate")
	}
	if err := requireFuture(in.EffectiveFrom,
		"tax rate"); err != nil {
		return err
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var old TaxRate
		var upper *time.Time
		err := tx.QueryRow(ctx, `
			SELECT id, country, coalesce(region, ''), category, rate_bp, label,
			       lower(valid), upper(valid)
			  FROM tax_rates WHERE id = $1 FOR UPDATE`, in.ReplacesID).
			Scan(&old.ID, &old.Country, &old.Region, &old.Category, &old.RateBP,
				&old.Label, &old.From, &upper)
		if err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("%w: no tax rate with that id", httpx.ErrNotFound)
			}
			return fmt.Errorf("load tax rate: %w", err)
		}
		if upper != nil {
			return fmt.Errorf("%w: that rate already ends on %s — supersede the one that is "+
				"currently open, not a closed one",
				httpx.ErrConflict, upper.Format("2 January 2006"))
		}
		if !in.EffectiveFrom.After(old.From) {
			return fmt.Errorf("%w: the new rate would start on or before the old one (%s), "+
				"which is not a supersession",
				httpx.ErrConflict, old.From.Format("2 January 2006"))
		}

		// Close first, open second. The exclusion constraint would reject the
		// reverse ordering, and relying on it to enforce the sequence would mean
		// the error an operator sees is a GiST violation rather than a sentence.
		if _, err := tx.Exec(ctx, `
			UPDATE tax_rates SET valid = daterange(lower(valid), $2, '[)') WHERE id = $1`,
			in.ReplacesID, in.EffectiveFrom); err != nil {
			return fmt.Errorf("close tax rate: %w", err)
		}

		var newID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO tax_rates (country, region, category, rate_bp, label, valid)
			VALUES ($1, NULLIF($2, ''), $3, $4, $5, daterange($6, NULL, '[)'))
			RETURNING id`,
			old.Country, old.Region, old.Category, in.RateBP,
			defaultTo(in.Label, old.Label), in.EffectiveFrom).Scan(&newID); err != nil {
			return fmt.Errorf("open tax rate: %w", err)
		}

		return s.Record(ctx, tx, Action{
			ActorID:     actorID,
			Action:      "config.tax_rate",
			SubjectType: "tax_rate",
			SubjectID:   uuid.NullUUID{UUID: newID, Valid: true},
			Reason:      in.Reason,
			Before: map[string]any{
				"id": old.ID, "rate_bp": old.RateBP, "from": old.From.Format("2006-01-02"),
			},
			After: map[string]any{
				"id": newID, "rate_bp": in.RateBP,
				"from":     in.EffectiveFrom.Format("2006-01-02"),
				"country":  old.Country,
				"category": old.Category,
			},
		})
	})
}

// ── boost rate cards ────────────────────────────────────────────────────────

// RateCard is one CPM floor over one period.
type RateCard struct {
	ID        uuid.UUID
	Placement string
	Country   string
	Region    string
	FloorCPM  int
	Currency  string
	From      time.Time
	To        *time.Time
	Current   bool
}

func (s *Service) ListRateCards(ctx context.Context, actorID uuid.UUID) ([]RateCard, error) {
	if _, err := s.Require(ctx, actorID, PermHealthRead); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, placement, country, coalesce(region, ''), floor_cpm_minor, currency,
		       lower(valid), upper(valid), valid @> current_date
		  FROM boost_rate_cards
		 ORDER BY placement, country, lower(valid) DESC`)
	if err != nil {
		return nil, fmt.Errorf("list rate cards: %w", err)
	}
	defer rows.Close()

	var out []RateCard
	for rows.Next() {
		var c RateCard
		if err := rows.Scan(&c.ID, &c.Placement, &c.Country, &c.Region, &c.FloorCPM,
			&c.Currency, &c.From, &c.To, &c.Current); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SupersedeRateCardInput opens a new floor price.
type SupersedeRateCardInput struct {
	ReplacesID    uuid.UUID
	FloorCPM      int
	EffectiveFrom time.Time
	Reason        string
}

// SupersedeRateCard closes one card and opens its replacement.
//
// Same shape as the tax rate, and deliberately not factored into a shared
// helper. The two tables differ in which columns carry identity, and a generic
// "supersede a dated row" function would take a table name and column list —
// which is a small SQL console, and §3.2 says there is never one of those.
func (s *Service) SupersedeRateCard(ctx context.Context, actorID uuid.UUID, in SupersedeRateCardInput) error {
	if _, err := s.Require(ctx, actorID, PermConfigWrite); err != nil {
		return err
	}
	if err := requireReason(in.Reason,
		"a floor price change affects every campaign priced after it"); err != nil {
		return err
	}
	if in.FloorCPM <= 0 {
		return field("floor_cpm_minor", "range",
			"a floor of zero means the auction has no reserve, which is not a price change")
	}
	if err := requireFuture(in.EffectiveFrom, "rate card"); err != nil {
		return err
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var old RateCard
		var upper *time.Time
		err := tx.QueryRow(ctx, `
			SELECT id, placement, country, coalesce(region, ''), floor_cpm_minor, currency,
			       lower(valid), upper(valid)
			  FROM boost_rate_cards WHERE id = $1 FOR UPDATE`, in.ReplacesID).
			Scan(&old.ID, &old.Placement, &old.Country, &old.Region, &old.FloorCPM,
				&old.Currency, &old.From, &upper)
		if err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("%w: no rate card with that id", httpx.ErrNotFound)
			}
			return fmt.Errorf("load rate card: %w", err)
		}
		if upper != nil {
			return fmt.Errorf("%w: that card already ends on %s",
				httpx.ErrConflict, upper.Format("2 January 2006"))
		}
		if !in.EffectiveFrom.After(old.From) {
			return fmt.Errorf("%w: the new card would start on or before the old one (%s)",
				httpx.ErrConflict, old.From.Format("2 January 2006"))
		}

		if _, err := tx.Exec(ctx, `
			UPDATE boost_rate_cards SET valid = daterange(lower(valid), $2, '[)') WHERE id = $1`,
			in.ReplacesID, in.EffectiveFrom); err != nil {
			return fmt.Errorf("close rate card: %w", err)
		}

		var newID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO boost_rate_cards (placement, country, region, floor_cpm_minor, currency, valid)
			VALUES ($1, $2, NULLIF($3, ''), $4, $5, daterange($6, NULL, '[)'))
			RETURNING id`,
			old.Placement, old.Country, old.Region, in.FloorCPM, old.Currency,
			in.EffectiveFrom).Scan(&newID); err != nil {
			return fmt.Errorf("open rate card: %w", err)
		}

		return s.Record(ctx, tx, Action{
			ActorID:     actorID,
			Action:      "config.rate_card",
			SubjectType: "boost_rate_card",
			SubjectID:   uuid.NullUUID{UUID: newID, Valid: true},
			Reason:      in.Reason,
			Before:      map[string]any{"id": old.ID, "floor_cpm_minor": old.FloorCPM},
			After: map[string]any{
				"id": newID, "floor_cpm_minor": in.FloorCPM,
				"from": in.EffectiveFrom.Format("2006-01-02"), "placement": old.Placement,
			},
		})
	})
}

// ── notification templates ──────────────────────────────────────────────────

// Template is one version of one message.
type Template struct {
	ID       uuid.UUID
	Key      string
	Channel  string
	Locale   string
	Version  int
	Category string
	Subject  string
	Body     string
	IsActive bool
	// Segments is what one send of this body costs, in SMS segments. Computed
	// rather than stored because it is a property of the text, and the whole
	// point of showing it is that the operator sees it change as they type.
	Segments int
	// Placeholders are the {{...}} names this body expects. Extracted so a new
	// version can be checked against the old one — a template that stops
	// referencing {{code}} sends a delivery OTP with no code in it, and nothing
	// downstream would notice.
	Placeholders []string
	UpdatedAt    time.Time
}

func (s *Service) ListTemplates(ctx context.Context, actorID uuid.UUID, activeOnly bool) ([]Template, error) {
	if _, err := s.Require(ctx, actorID, PermHealthRead); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, key, channel, locale::text, version, category,
		       coalesce(subject, ''), body, is_active, updated_at
		  FROM notification_templates
		 WHERE ($1 = false OR is_active)
		 ORDER BY key, channel, locale, version DESC`, activeOnly)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var out []Template
	for rows.Next() {
		var t Template
		if err := rows.Scan(&t.ID, &t.Key, &t.Channel, &t.Locale, &t.Version,
			&t.Category, &t.Subject, &t.Body, &t.IsActive, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Segments = SMSSegments(t.Body)
		t.Placeholders = Placeholders(t.Body)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ReviseTemplateInput writes the next version of a message.
type ReviseTemplateInput struct {
	ReplacesID uuid.UUID
	Subject    string
	Body       string
	Reason     string
}

// ReviseTemplate writes version n+1 and deactivates n.
//
// The old version is kept and only deactivated, never updated. A message already
// sent has to remain displayable as the text that was actually sent — a support
// agent reading "we told them X" from a template that has since been edited is
// reading a reconstruction, not a record.
//
// Placeholder loss is refused rather than warned about. A template that stops
// referencing {{code}} sends a delivery OTP with no code in it; nothing
// downstream validates the rendered text, so the first sign is a driver at a door
// with a customer who received a blank message.
func (s *Service) ReviseTemplate(ctx context.Context, actorID uuid.UUID, in ReviseTemplateInput) error {
	if _, err := s.Require(ctx, actorID, PermConfigWrite); err != nil {
		return err
	}
	if err := requireReason(in.Reason,
		"a template change alters what every customer receives from this point"); err != nil {
		return err
	}
	if strings.TrimSpace(in.Body) == "" {
		return field("body", "required", "a message with no body sends nothing")
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var old Template
		err := tx.QueryRow(ctx, `
			SELECT id, key, channel, locale::text, version, category,
			       coalesce(subject, ''), body, is_active
			  FROM notification_templates WHERE id = $1 FOR UPDATE`, in.ReplacesID).
			Scan(&old.ID, &old.Key, &old.Channel, &old.Locale, &old.Version,
				&old.Category, &old.Subject, &old.Body, &old.IsActive)
		if err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("%w: no template with that id", httpx.ErrNotFound)
			}
			return fmt.Errorf("load template: %w", err)
		}

		if lost := missingPlaceholders(old.Body, in.Body); len(lost) > 0 {
			return field("body", "placeholder_lost",
				fmt.Sprintf("the new body drops %s, which the old one used. Nothing renders "+
					"a template twice to check — a delivery code template without {{code}} "+
					"sends a blank message and the first sign of it is a driver at a door",
					strings.Join(lost, ", ")))
		}

		if _, err := tx.Exec(ctx,
			`UPDATE notification_templates SET is_active = false, updated_at = now()
			  WHERE key = $1 AND channel = $2 AND locale = $3::locale_code`,
			old.Key, old.Channel, old.Locale); err != nil {
			return fmt.Errorf("deactivate template: %w", err)
		}

		var newID uuid.UUID
		var newVersion int
		if err := tx.QueryRow(ctx, `
			INSERT INTO notification_templates
				(key, channel, locale, version, category, subject, body, is_active)
			SELECT $1, $2, $3::locale_code,
			       coalesce(max(version), 0) + 1, $4, NULLIF($5, ''), $6, true
			  FROM notification_templates
			 WHERE key = $1 AND channel = $2 AND locale = $3::locale_code
			RETURNING id, version`,
			old.Key, old.Channel, old.Locale, old.Category,
			in.Subject, in.Body).Scan(&newID, &newVersion); err != nil {
			return fmt.Errorf("write template version: %w", err)
		}

		return s.Record(ctx, tx, Action{
			ActorID:     actorID,
			Action:      "config.template",
			SubjectType: "notification_template",
			SubjectID:   uuid.NullUUID{UUID: newID, Valid: true},
			Reason:      in.Reason,
			Before: map[string]any{
				"version": old.Version, "body": old.Body, "segments": SMSSegments(old.Body),
			},
			After: map[string]any{
				"key": old.Key, "channel": old.Channel, "locale": old.Locale,
				"version": newVersion, "body": in.Body, "segments": SMSSegments(in.Body),
			},
		})
	})
}

// ── text helpers ────────────────────────────────────────────────────────────

// SMSSegments is how many SMS this body costs to send.
//
// The rule that matters here: a message containing *any* character outside
// GSM-7 is encoded as UCS-2 and the segment size drops from 160 to 70. Arabic is
// entirely outside GSM-7, so every Arabic template is UCS-2 — and one Arabic
// character accidentally left in an English template does the same thing, taking
// a 155-character message from one segment to three.
//
// That is a real cost multiplied by every send, and it is invisible in an
// editor that shows a character count. Which is why the editor shows this
// instead.
func SMSSegments(body string) int {
	if body == "" {
		return 0
	}

	// Counted on the *rendered* shape, not the template. `{` and `}` are not in
	// the GSM-7 basic alphabet, so counting the raw template would classify
	// every message containing a placeholder as UCS-2 and report a 75-character
	// English SMS as two segments when it is one. The braces are never sent —
	// they are gone by the time the message reaches a handset.
	//
	// The placeholder *name* is kept as a stand-in for the value, which is a
	// proxy rather than a measurement: `{{code}}` becomes four characters where
	// the real code is six. Right order of magnitude, and the alternative is
	// inventing sample data per placeholder and being confidently wrong instead
	// of roughly right.
	rendered := renderShape(body)

	ucs2 := false
	for _, r := range rendered {
		if r > unicode.MaxASCII || !gsm7Basic(r) {
			ucs2 = true
			break
		}
	}

	runes := len([]rune(rendered))
	single, concat := 160, 153
	if ucs2 {
		single, concat = 70, 67
	}
	if runes <= single {
		return 1
	}
	// Concatenated messages carry a header in each part, so the per-part budget
	// drops — which is why a 161-character message is two segments of 153 rather
	// than 160 plus 1.
	return (runes + concat - 1) / concat
}

// gsm7Basic is the ASCII subset of the GSM 03.38 basic alphabet.
//
// Approximate on purpose, and conservative in the right direction: characters it
// does not recognise are treated as UCS-2, which over-estimates cost rather than
// under-estimating it. An editor that promises one segment and sends three is
// worse than one that warns about three and sends one.
func gsm7Basic(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case strings.ContainsRune("@£$¥èéùìòÇØøÅå_ÆæßÉ !\"#¤%&'()*+,-./:;<=>?¡ÄÖÑÜ§¿äöñüà\n\r", r):
		return true
	}
	return false
}

// renderShape strips the {{ }} delimiters, keeping the names.
//
// What is left approximates what a handset receives, which is what a segment
// count is about. Doing this as a separate function rather than inline keeps
// SMSSegments readable and makes the approximation nameable in a test.
func renderShape(body string) string {
	var b strings.Builder
	rest := body
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:i])
		rest = rest[i+2:]
		j := strings.Index(rest, "}}")
		if j < 0 {
			// An unclosed placeholder is broken template text and will be sent
			// literally, braces and all. Counted as written rather than
			// silently repaired — the segment count is not the place to hide a
			// typo that is about to go out to customers.
			b.WriteString("{{")
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(strings.TrimSpace(rest[:j]))
		rest = rest[j+2:]
	}
}

// Placeholders extracts the {{name}} tokens from a body, in order, deduplicated.
func Placeholders(body string) []string {
	var out []string
	seen := map[string]bool{}
	rest := body
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			break
		}
		rest = rest[i+2:]
		j := strings.Index(rest, "}}")
		if j < 0 {
			break
		}
		name := strings.TrimSpace(rest[:j])
		rest = rest[j+2:]
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func missingPlaceholders(oldBody, newBody string) []string {
	have := map[string]bool{}
	for _, p := range Placeholders(newBody) {
		have[p] = true
	}
	var lost []string
	for _, p := range Placeholders(oldBody) {
		if !have[p] {
			lost = append(lost, "{{"+p+"}}")
		}
	}
	return lost
}

// ── shared ──────────────────────────────────────────────────────────────────

func field(name, code, message string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{
		Field: name, Code: code, Message: message,
	}}}
}

func requireReason(reason, why string) error {
	if strings.TrimSpace(reason) == "" {
		return field("reason", "required", why)
	}
	return nil
}

// requireFuture refuses a backdated effective date.
//
// Backdating rewrites what was charged on documents already with customers.
// There is no version of that which is a configuration edit rather than an
// accounting correction, and an accounting correction is a credit note.
func requireFuture(from time.Time, what string) error {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if from.Before(today) {
		return field("effective_from", "past",
			fmt.Sprintf("a %s cannot start in the past — that would restate documents "+
				"already issued. Correcting those is a credit note, not a config change", what))
	}
	return nil
}

func defaultTo(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
