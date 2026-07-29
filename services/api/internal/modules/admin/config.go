package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Configuration editing — PLAN-ADMIN-CONSOLE §4.5.
//
// This lives in `admin` rather than `adminview` for the reason the package doc
// on `adminview` gives: that package never writes, and this does. Two of these
// tables — `feature_flags` and `app_settings` — were created by migration 20
// specifically so the console owns them, so `admin` owning the writes is not a
// layering violation; it is the module that owns the data.
//
// The date-ranged tables (`tax_rates`, `boost_rate_cards`) are **read-only
// here**, and that is a decision rather than a gap. Both carry a `daterange` with
// an exclusion constraint, and editing one correctly means closing the current
// row and opening a new one in the same transaction — a *version*, not an edit.
// A form that let someone change a VAT rate in place would silently restate
// every invoice already issued under the old one.

// Flag is a feature flag.
type Flag struct {
	Key         string
	Description string
	IsEnabled   bool
	RolloutPct  int
	EnabledFor  []uuid.UUID
	UpdatedBy   string
	UpdatedAt   time.Time
}

func (s *Service) ListFlags(ctx context.Context, actorID uuid.UUID) ([]Flag, error) {
	if _, err := s.Require(ctx, actorID, PermHealthRead); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.key, f.description, f.is_enabled, f.rollout_pct,
		       coalesce(f.enabled_for, '{}'),
		       coalesce(u.display_name, u.phone, ''), f.updated_at
		  FROM feature_flags f
		  LEFT JOIN users u ON u.id = f.updated_by
		 ORDER BY f.key`)
	if err != nil {
		return nil, fmt.Errorf("list flags: %w", err)
	}
	defer rows.Close()

	var out []Flag
	for rows.Next() {
		var f Flag
		if err := rows.Scan(&f.Key, &f.Description, &f.IsEnabled, &f.RolloutPct,
			&f.EnabledFor, &f.UpdatedBy, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetFlagInput is a flag change.
type SetFlagInput struct {
	Key        string
	IsEnabled  bool
	RolloutPct int
	Reason     string
}

// SetFlag changes a flag and records who did it, in one transaction.
//
// This is the §3.3 rule honoured properly, unlike the order actions: the change
// and its audit row commit together, because both tables are owned here and no
// third-party gateway sits in between. Where that is achievable it should be
// achieved, and the compromise in `RecordOutside` is for the cases where it is
// not.
//
// The before-state is captured inside the transaction rather than read
// beforehand. Reading first and writing second leaves a window where two
// operators flipping the same flag each record the other's value as "before",
// producing an audit trail where neither change appears to have happened.
func (s *Service) SetFlag(ctx context.Context, actorID uuid.UUID, in SetFlagInput) error {
	if _, err := s.Require(ctx, actorID, PermConfigWrite); err != nil {
		return err
	}
	if in.Reason == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "a flag change with no stated reason cannot be explained during an incident",
		}}}
	}
	if in.RolloutPct < 0 || in.RolloutPct > 100 {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "rollout_pct", Code: "range",
			Message: "a rollout percentage outside 0–100 is not a rollout",
		}}}
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var before Flag
		err := tx.QueryRow(ctx, `
			SELECT key, is_enabled, rollout_pct FROM feature_flags
			 WHERE key = $1 FOR UPDATE`, in.Key).
			Scan(&before.Key, &before.IsEnabled, &before.RolloutPct)
		if err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("%w: there is no flag called %q — flags are declared in a "+
					"migration, not created from the console", httpx.ErrNotFound, in.Key)
			}
			return fmt.Errorf("load flag: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE feature_flags
			   SET is_enabled = $2, rollout_pct = $3, updated_by = $4, updated_at = now()
			 WHERE key = $1`, in.Key, in.IsEnabled, in.RolloutPct, actorID); err != nil {
			return fmt.Errorf("update flag: %w", err)
		}

		return s.Record(ctx, tx, Action{
			ActorID:     actorID,
			Action:      "config.flag",
			SubjectType: "feature_flag",
			Reason:      in.Reason,
			Before:      map[string]any{"enabled": before.IsEnabled, "rollout_pct": before.RolloutPct},
			After: map[string]any{
				"key": in.Key, "enabled": in.IsEnabled, "rollout_pct": in.RolloutPct,
			},
		})
	})
}

// Setting is one runtime setting.
type Setting struct {
	Key         string
	Value       json.RawMessage
	Description string
	IsSensitive bool
	UpdatedBy   string
	UpdatedAt   time.Time
}

// ListSettings returns settings, redacting the sensitive ones.
//
// A sensitive setting's *value* is withheld while its existence, description and
// last-changed-by are not. Hiding the row entirely would mean an operator cannot
// tell whether a setting exists, which produces a second one created under a
// slightly different name — and now the system has two and reads the wrong one.
func (s *Service) ListSettings(ctx context.Context, actorID uuid.UUID) ([]Setting, error) {
	grant, err := s.Require(ctx, actorID, PermHealthRead)
	if err != nil {
		return nil, err
	}
	canSeeSensitive := grant.Permissions[PermConfigWrite]

	rows, err := s.pool.Query(ctx, `
		SELECT s.key,
		       CASE WHEN s.is_sensitive AND NOT $1 THEN '"«redacted»"'::jsonb ELSE s.value END,
		       s.description, s.is_sensitive,
		       coalesce(u.display_name, u.phone, ''), s.updated_at
		  FROM app_settings s
		  LEFT JOIN users u ON u.id = s.updated_by
		 ORDER BY s.key`, canSeeSensitive)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close()

	var out []Setting
	for rows.Next() {
		var st Setting
		if err := rows.Scan(&st.Key, &st.Value, &st.Description, &st.IsSensitive,
			&st.UpdatedBy, &st.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SetSettingInput is a settings change.
type SetSettingInput struct {
	Key    string
	Value  json.RawMessage
	Reason string
}

// SetSetting writes a setting and its audit row together.
func (s *Service) SetSetting(ctx context.Context, actorID uuid.UUID, in SetSettingInput) error {
	if _, err := s.Require(ctx, actorID, PermConfigWrite); err != nil {
		return err
	}
	if in.Reason == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "a settings change with no stated reason is indistinguishable from a mistake",
		}}}
	}
	if !json.Valid(in.Value) {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "value", Code: "invalid",
			Message: "the value must be valid JSON — a bare string needs quotes",
		}}}
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var before json.RawMessage
		var sensitive bool
		err := tx.QueryRow(ctx,
			`SELECT value, is_sensitive FROM app_settings WHERE key = $1 FOR UPDATE`, in.Key).
			Scan(&before, &sensitive)
		if err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("%w: there is no setting called %q — settings are declared "+
					"in a migration so that a typo cannot create one", httpx.ErrNotFound, in.Key)
			}
			return fmt.Errorf("load setting: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE app_settings SET value = $2, updated_by = $3, updated_at = now()
			 WHERE key = $1`, in.Key, in.Value, actorID); err != nil {
			return fmt.Errorf("update setting: %w", err)
		}

		// A sensitive setting's old and new values stay out of the audit row.
		// The audit trail is read by more people than the setting is, and
		// recording "who changed the signing key, when and why" is the useful
		// part — recording the key itself would put a secret somewhere it was
		// deliberately kept out of.
		beforeState := any(map[string]any{"value": before})
		afterState := any(map[string]any{"value": in.Value})
		if sensitive {
			beforeState = map[string]any{"redacted": true}
			afterState = map[string]any{"redacted": true}
		}

		return s.Record(ctx, tx, Action{
			ActorID:     actorID,
			Action:      "config.setting",
			SubjectType: "app_setting",
			Reason:      in.Reason,
			Before:      beforeState,
			After:       afterState,
		})
	})
}
