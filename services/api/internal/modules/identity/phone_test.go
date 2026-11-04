package identity

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"+971501234567":    "+971501234567",
		"+971 50 123 4567": "+971501234567",
		"0501234567":       "+971501234567",
		"050-123-4567":     "+971501234567",
		"00971501234567":   "+971501234567",
		"00447700900123":   "+447700900123",
		"501234567":        "501234567",
	}
	for in, want := range cases {
		got := normalizePhone(in)
		if got != want {
			t.Errorf("normalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
	if e164.MatchString(normalizePhone("0001234567")) {
		t.Error("a malformed number must still fail E.164 after normalising")
	}
	if !e164.MatchString(normalizePhone("0501234567")) {
		t.Error("a UAE national number should normalise to valid E.164")
	}
}
