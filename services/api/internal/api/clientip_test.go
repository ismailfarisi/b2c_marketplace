package api

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	if err := SetTrustedProxies(""); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/auth/otp/request", nil)
	r.RemoteAddr = "198.51.100.9:4321"
	r.Header.Set("X-Forwarded-For", "203.0.113.77")
	if got := clientIP(r); got != "198.51.100.9" {
		t.Fatalf("clientIP = %q, want the socket address", got)
	}
}

func TestClientIPTakesRightmostUntrustedHop(t *testing.T) {
	if err := SetTrustedProxies("10.0.0.0/8, 192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetTrustedProxies("") })

	r := httptest.NewRequest("POST", "/v1/auth/otp/request", nil)
	r.RemoteAddr = "10.1.2.3:4321"
	// A client-forged first entry, the real client, then a second trusted hop.
	r.Header.Set("X-Forwarded-For", "203.0.113.77, 198.51.100.20, 192.0.2.1")
	if got := clientIP(r); got != "198.51.100.20" {
		t.Fatalf("clientIP = %q, want 198.51.100.20", got)
	}

	r.Header.Del("X-Forwarded-For")
	if got := clientIP(r); got != "10.1.2.3" {
		t.Fatalf("no XFF: clientIP = %q, want the proxy address", got)
	}
}

func TestSetTrustedProxiesRejectsGarbage(t *testing.T) {
	if err := SetTrustedProxies("not-an-ip"); err == nil {
		t.Fatal("want an error")
	}
	_ = SetTrustedProxies("")
}
