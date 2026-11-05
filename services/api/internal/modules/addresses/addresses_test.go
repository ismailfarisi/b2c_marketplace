package addresses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marketplace/api/internal/httpx"
)

func TestValidateSpeaksTheCallersLanguage(t *testing.T) {
	for _, tc := range []struct{ locale, label string }{
		{"en", "Give the address a short name, like Home."},
		{"ar", "أعطِ العنوان اسمًا قصيرًا، مثل المنزل."},
	} {
		in := Input{}
		err := in.validate(tc.locale)
		var v *httpx.ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("%s: want validation error, got %v", tc.locale, err)
		}
		got := map[string]httpx.FieldError{}
		for _, f := range v.Fields {
			got[f.Field] = f
		}
		if got["label"].Message != tc.label || got["label"].Code != "required" {
			t.Errorf("%s: label = %+v", tc.locale, got["label"])
		}
		if got["city"].Code != "required" || got["point"].Code != "invalid_point" {
			t.Errorf("%s: codes = %+v", tc.locale, got)
		}
		if tc.locale == "ar" && (got["city"].Message != "أدخل المدينة." || got["point"].Message != "حدّد موقع العنوان على الخريطة.") {
			t.Errorf("ar messages not localized: %+v", got)
		}
	}
}

func TestDefaultOnlySpeaksTheCallersLanguage(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{"", "To change your default address, choose another address as the default."},
		{"ar-AE", "لتغيير عنوانك الافتراضي، اختر عنوانًا آخر ليكون الافتراضي."},
	} {
		var ctx context.Context
		h := httpx.Locale(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() }))
		req := httptest.NewRequest(http.MethodPatch, "/me/addresses/x", nil)
		if tc.header != "" {
			req.Header.Set("Accept-Language", tc.header)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
		var v *httpx.ValidationError
		if !errors.As(DefaultOnly(ctx), &v) || len(v.Fields) != 1 {
			t.Fatalf("%q: want one field error", tc.header)
		}
		if f := v.Fields[0]; f.Field != "is_default" || f.Code != "invalid" || f.Message != tc.want {
			t.Errorf("%q: got %+v", tc.header, f)
		}
	}
}
