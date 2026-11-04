package company

import "testing"

func TestInvitePhone(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"0500000001", "+971500000001", true},
		{"050 000 0001", "+971500000001", true},
		{"500000001", "+971500000001", true},
		{"00971500000001", "+971500000001", true},
		{"971500000001", "+971500000001", true},
		{"+971 50 000 0001", "+971500000001", true},
		{"+9710500000001", "+971500000001", true},
		{"+447700900123", "+447700900123", true},
		{"12345", "12345", false},
		{"+12", "+12", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := InvitePhone(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("InvitePhone(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestBillable(t *testing.T) {
	if billable(0, 0) {
		t.Error("no lines must not bill")
	}
	if billable(2, 0) {
		t.Error("credit cancelling the charge must carry forward, not issue an AED 0 statement")
	}
	if billable(2, -56000) {
		t.Error("net credit must carry forward")
	}
	if !billable(1, 56000) {
		t.Error("a positive net must bill")
	}
}
