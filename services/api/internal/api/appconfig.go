package api

import (
	"net/http"

	"github.com/marketplace/api/internal/httpx"
)

// AppRelease is what a mobile build compares its own version against at
// launch: below MinVersion it must update, below LatestVersion it may.
type AppRelease struct {
	MinVersion    string `json:"min_version"`
	LatestVersion string `json:"latest_version"`
	StoreURL      string `json:"store_url"`
}

type AppReleases struct {
	Android AppRelease `json:"android"`
	IOS     AppRelease `json:"ios"`
}

// AppConfig serves the release thresholds. Public and cacheable: it runs before
// sign-in, and the apps treat any failure as "up to date".
func (h *Handlers) AppConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.WriteJSON(w, http.StatusOK, "", h.Apps)
}
