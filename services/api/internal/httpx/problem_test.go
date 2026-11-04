package httpx

import (
	"encoding/json"
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
