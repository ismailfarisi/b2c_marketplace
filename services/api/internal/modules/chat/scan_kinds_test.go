package chat

import "testing"

// TestCleanMessageHasEmptyFindingsNotNil is a one-line regression guard for a
// bug that made chat work only for the messages it was supposed to redact.
//
// `messages.findings` is NOT NULL. A nil slice reaches Postgres as NULL, so a
// message with nothing wrong with it violated the constraint and came back as a
// 500 — while a message containing a phone number, which produced a non-empty
// slice, inserted fine.
func TestCleanMessageHasEmptyFindingsNotNil(t *testing.T) {
	kinds := Scan{}.Kinds()
	if kinds == nil {
		t.Fatal("a clean scan produced nil findings; Postgres will reject it as NULL")
	}
	if len(kinds) != 0 {
		t.Fatalf("expected no findings, got %v", kinds)
	}
}

func TestFindingKindsAreDeduplicated(t *testing.T) {
	s := Scan{Findings: []Finding{
		{Kind: FindingPhone}, {Kind: FindingPhone},
	}}
	if got := s.Kinds(); len(got) != 1 {
		t.Fatalf("expected one kind, got %v", got)
	}
}
