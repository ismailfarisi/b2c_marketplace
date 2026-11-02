package support

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	if got, err := clean("subject", "  Cold lunch \n", MaxSubject); err != nil || got != "Cold lunch" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := clean("body", "   ", MaxBody); err == nil {
		t.Fatal("blank accepted")
	}
	if _, err := clean("subject", strings.Repeat("ع", MaxSubject+1), MaxSubject); err == nil {
		t.Fatal("too long accepted")
	}
	if _, err := clean("subject", strings.Repeat("ع", MaxSubject), MaxSubject); err != nil {
		t.Fatalf("200 Arabic characters rejected: %v", err)
	}
}

func TestValidStatus(t *testing.T) {
	for _, s := range []string{"open", "answered", "closed"} {
		if err := validStatus(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if validStatus("resolved") == nil {
		t.Fatal("unknown status accepted")
	}
}
