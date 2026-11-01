package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetaWhatsAppSendsTemplateWithOrderedParams(t *testing.T) {
	var got metaMessage
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.123"}]}`))
	}))
	defer srv.Close()

	p := MetaWhatsApp{AccessToken: "tok", PhoneNumberID: "999", APIVersion: "v21.0", BaseURL: srv.URL}
	ref, err := p.Send(context.Background(), Delivery{
		Destination: "+971500000001", Locale: "ar", ProviderTemplateName: "meals_tomorrow",
		ParamValues: []string{"الأحد", "مطبخ", "برياني"},
	})
	if err != nil || ref != "wamid.123" {
		t.Fatalf("ref=%q err=%v", ref, err)
	}
	if path != "/v21.0/999/messages" || auth != "Bearer tok" {
		t.Fatalf("path=%q auth=%q", path, auth)
	}
	if got.To != "971500000001" || got.Type != "template" || got.Template.Name != "meals_tomorrow" ||
		got.Template.Language["code"] != "ar" {
		t.Fatalf("unexpected message: %+v", got)
	}
	ps := got.Template.Components[0].Parameters
	if len(ps) != 3 || ps[0].Text != "الأحد" || ps[2].Text != "برياني" {
		t.Fatalf("params out of order: %+v", ps)
	}
}

func TestMetaWhatsAppSessionTextWithoutTemplate(t *testing.T) {
	var got metaMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.9"}]}`))
	}))
	defer srv.Close()
	p := MetaWhatsApp{AccessToken: "t", PhoneNumberID: "1", BaseURL: srv.URL}
	if _, err := p.Send(context.Background(), Delivery{Destination: "+971500000002", Body: "hello"}); err != nil {
		t.Fatal(err)
	}
	if got.Type != "text" || got.Text["body"] != "hello" {
		t.Fatalf("unexpected message: %+v", got)
	}
}

func TestMetaWhatsAppErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		status    int
		permanent bool
	}{{400, true}, {401, true}, {429, false}, {503, false}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope","code":131026}}`))
		}))
		p := MetaWhatsApp{AccessToken: "t", PhoneNumberID: "1", BaseURL: srv.URL}
		_, err := p.Send(context.Background(), Delivery{Destination: "+971500000003", ProviderTemplateName: "x"})
		srv.Close()
		if err == nil || errors.Is(err, ErrPermanent) != tc.permanent {
			t.Fatalf("status %d: err=%v permanent=%v", tc.status, err, errors.Is(err, ErrPermanent))
		}
	}
}

func TestOrderedParams(t *testing.T) {
	got := orderedParams([]string{"b", "a", "missing"}, map[string]string{"a": "1", "b": "2"})
	if len(got) != 3 || got[0] != "2" || got[1] != "1" || got[2] != "" {
		t.Fatalf("got %v", got)
	}
}
