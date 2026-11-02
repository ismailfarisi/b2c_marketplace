package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppConfigServesReleases(t *testing.T) {
	h := &Handlers{Apps: AppReleases{Android: AppRelease{MinVersion: "1.2.0", LatestVersion: "1.4.0"}}}
	rec := httptest.NewRecorder()
	h.AppConfig(rec, httptest.NewRequest(http.MethodGet, "/v1/app/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got AppReleases
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Android.MinVersion != "1.2.0" || got.IOS.MinVersion != "" {
		t.Fatalf("got %+v", got)
	}
}
