package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
)

func ptr(s string) *string { return &s }

func TestProfileGetAndUpdate(t *testing.T) {
	s, pool := testService(t)
	ctx := context.Background()
	newUser := func() (uuid.UUID, string) {
		phone := testPhone()
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO users (phone) VALUES ($1) RETURNING id`, phone).Scan(&id); err != nil {
			t.Fatalf("user: %v", err)
		}
		return id, phone
	}
	id, phone := newUser()
	p, err := s.GetProfile(ctx, id)
	if err != nil || p.Phone != phone || p.DisplayName != nil || p.Locale != "en" {
		t.Fatalf("get: %v %+v", err, p)
	}

	email := "qa-" + id.String()[:8] + "@example.com"
	p, err = s.UpdateProfile(ctx, id, ProfileInput{DisplayName: ptr("  Aisha "), Email: ptr(email), Locale: ptr("ar")})
	if err != nil || p.DisplayName == nil || *p.DisplayName != "Aisha" || p.Email == nil || *p.Email != email || p.Locale != "ar" {
		t.Fatalf("update: %v %+v", err, p)
	}

	// Omitted fields stay; an empty string clears.
	p, err = s.UpdateProfile(ctx, id, ProfileInput{DisplayName: ptr("")})
	if err != nil || p.DisplayName != nil || p.Email == nil || p.Locale != "ar" {
		t.Fatalf("clear name: %v %+v", err, p)
	}

	other, _ := newUser()
	if _, err := s.UpdateProfile(ctx, other, ProfileInput{Email: ptr(email)}); problemCode(err) != "email_taken" {
		t.Fatalf("duplicate email: want email_taken, got %v", err)
	}
	var v *httpx.ValidationError
	if _, err := s.UpdateProfile(ctx, other, ProfileInput{Email: ptr("not an email")}); !errors.As(err, &v) {
		t.Fatalf("bad email: want validation error, got %v", err)
	}
}

func TestDeleteRefusalsAreLocalized(t *testing.T) {
	if got := ownsVendorDetail("ar"); got == ownsVendorDetail("en") || got == "" {
		t.Fatalf("owns_vendor detail not localized: %q", got)
	}
}
