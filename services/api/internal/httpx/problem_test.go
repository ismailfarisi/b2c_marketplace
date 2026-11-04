package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWriteProblemRateLimited(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/auth/otp/request", nil)
	WriteProblem(w, r, &RetryAfter{
		Err:   Coded(ErrRateLimited, "otp_rate_limited", "Try again later."),
		After: 90*time.Second + 300*time.Millisecond,
	})
	if w.Code != 429 {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "91" {
		t.Fatalf("Retry-After = %q, want 91", got)
	}
	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != "otp_rate_limited" || p.Status != 429 {
		t.Fatalf("problem = %+v", p)
	}
}

func TestForbiddenProblemCarriesSentence(t *testing.T) {
	cases := []struct {
		err    error
		detail string
	}{
		{fmt.Errorf("%w: this needs identity.review; you hold commerce.read", ErrForbidden), "this needs identity.review; you hold commerce.read"},
		{ErrForbidden, ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		WriteProblem(rec, httptest.NewRequest(http.MethodGet, "/v1/admin/users/x/phone", nil), c.err)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		var p Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.Detail != c.detail {
			t.Errorf("detail = %q, want %q", p.Detail, c.detail)
		}
	}
}
