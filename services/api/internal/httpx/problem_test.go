package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
