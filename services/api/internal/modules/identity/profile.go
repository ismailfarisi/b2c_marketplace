package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/marketplace/api/internal/httpx"
)

// Profile is the signed-in user as the account screen shows them.
type Profile struct {
	ID          uuid.UUID `json:"id"`
	Phone       string    `json:"phone"`
	Email       *string   `json:"email"`
	DisplayName *string   `json:"display_name"`
	Locale      string    `json:"locale"`
	Status      string    `json:"status"`
	CreatedAt   string    `json:"created_at"`
}

// ProfileInput is a partial update: a nil field is left alone, an empty
// string clears display_name or email.
type ProfileInput struct {
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email"`
	Locale      *string `json:"locale"`
}

func profileOf(u User) Profile {
	return Profile{
		ID: u.ID, Phone: u.Phone, Email: u.Email, DisplayName: u.DisplayName,
		Locale: u.Locale, Status: u.Status, CreatedAt: u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// GetProfile is GET /me.
func (s *Service) GetProfile(ctx context.Context, userID uuid.UUID) (Profile, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Profile{}, fmt.Errorf("profile: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	u, err := loadUser(ctx, tx, userID)
	if err != nil {
		return Profile{}, err
	}
	return profileOf(u), nil
}

// validate trims the input and reports field errors in the caller's language.
func (in *ProfileInput) validate(locale string) error {
	var fields []httpx.FieldError
	if in.DisplayName != nil {
		v := strings.TrimSpace(*in.DisplayName)
		in.DisplayName = &v
		if len([]rune(v)) > 80 {
			fields = append(fields, httpx.FieldError{Field: "display_name", Code: "too_long",
				Message: say(locale, "Use 80 characters or fewer.", "استخدم ٨٠ حرفًا أو أقل.")})
		}
	}
	if in.Email != nil {
		v := strings.ToLower(strings.TrimSpace(*in.Email))
		in.Email = &v
		if v != "" {
			if a, err := mail.ParseAddress(v); err != nil || a.Address != v || len(v) > 254 {
				fields = append(fields, httpx.FieldError{Field: "email", Code: "invalid",
					Message: say(locale, "Enter an email like name@example.com.", "أدخل بريدًا إلكترونيًا مثل name@example.com.")})
			}
		}
	}
	if in.Locale != nil && *in.Locale != "en" && *in.Locale != "ar" {
		fields = append(fields, httpx.FieldError{Field: "locale", Code: "invalid",
			Message: say(locale, "Choose English or Arabic.", "اختر العربية أو الإنجليزية.")})
	}
	if len(fields) > 0 {
		return &httpx.ValidationError{Fields: fields}
	}
	return nil
}

// UpdateProfile is PATCH /me. A changed email is stored unverified; an email
// another live account already uses is 409 email_taken.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, in ProfileInput) (Profile, error) {
	locale := httpx.LocaleFromContext(ctx)
	if err := in.validate(locale); err != nil {
		return Profile{}, err
	}
	var name, email, loc any
	if in.DisplayName != nil {
		name = *in.DisplayName
	}
	if in.Email != nil {
		email = *in.Email
	}
	if in.Locale != nil {
		loc = *in.Locale
	}
	var u User
	err := s.pool.QueryRow(ctx, `
		UPDATE users SET
		    display_name = CASE WHEN $2::text IS NULL THEN display_name ELSE nullif($2::text, '') END,
		    email = CASE WHEN $3::text IS NULL THEN email ELSE nullif($3::text, '')::citext END,
		    email_verified_at = CASE WHEN $3::text IS NULL OR nullif($3::text, '')::citext IS NOT DISTINCT FROM email
		                             THEN email_verified_at ELSE NULL END,
		    locale = coalesce($4::text::locale_code, locale)
		 WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, phone, email, display_name, locale::text, status::text, created_at`,
		userID, name, email, loc,
	).Scan(&u.ID, &u.Phone, &u.Email, &u.DisplayName, &u.Locale, &u.Status, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, httpx.ErrUnauthorized
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return Profile{}, httpx.Coded(httpx.ErrConflict, "email_taken",
			say(locale, "Another account already uses this email.", "هذا البريد الإلكتروني مستخدم في حساب آخر."))
	}
	if err != nil {
		return Profile{}, fmt.Errorf("profile: update: %w", err)
	}
	return profileOf(u), nil
}

// say picks the caller's language for a sentence shown to a person.
func say(locale, en, ar string) string {
	if locale == "ar" {
		return ar
	}
	return en
}
