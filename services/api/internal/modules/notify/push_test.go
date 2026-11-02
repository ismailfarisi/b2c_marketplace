package notify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestAPNsSend(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	var gotPath, gotTopic, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotTopic, gotAuth = r.URL.Path, r.Header.Get("apns-topic"), r.Header.Get("authorization")
		if strings.HasSuffix(r.URL.Path, "/dead") {
			w.WriteHeader(http.StatusGone)
			return
		}
	}))
	defer srv.Close()
	a := &APNs{Key: key, KeyID: "K", TeamID: "T", Topic: "ae.marketplace.mobile", Endpoint: srv.URL}

	if err := a.send(context.Background(), "abc", pushMessage{Title: "t", Body: "b", Link: "/x"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/3/device/abc" || gotTopic != "ae.marketplace.mobile" || !strings.HasPrefix(gotAuth, "bearer ") {
		t.Fatalf("path=%q topic=%q auth=%q", gotPath, gotTopic, gotAuth)
	}
	if err := a.send(context.Background(), "dead", pushMessage{}); !errors.Is(err, ErrDeadToken) {
		t.Fatalf("410 should be a dead token, got %v", err)
	}
}

func TestFCMSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/projects/p/messages:send" {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		if strings.Contains(readAll(r), `"token":"dead"`) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
		}
	}))
	defer srv.Close()
	f := &FCM{ProjectID: "p", Tokens: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "tok"}), Endpoint: srv.URL}
	if err := f.send(context.Background(), "abc", pushMessage{Title: "t", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := f.send(context.Background(), "dead", pushMessage{}); !errors.Is(err, ErrDeadToken) {
		t.Fatalf("UNREGISTERED should be a dead token, got %v", err)
	}
}

func readAll(r *http.Request) string {
	b := new(strings.Builder)
	buf := make([]byte, 1024)
	for {
		n, err := r.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return b.String()
		}
	}
}
