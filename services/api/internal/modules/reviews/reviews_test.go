package reviews

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	if c, err := Validate(5, "  Lovely biryani  "); err != nil || c != "Lovely biryani" {
		t.Fatalf("got %q, %v", c, err)
	}
	for _, r := range []int{0, 6, -1} {
		if _, err := Validate(r, ""); err == nil {
			t.Errorf("rating %d should be rejected", r)
		}
	}
	if _, err := Validate(4, strings.Repeat("ب", MaxText)); err != nil {
		t.Fatalf("1000 Arabic characters is within the limit: %v", err)
	}
	if _, err := Validate(4, strings.Repeat("a", MaxText+1)); err == nil {
		t.Fatal("over the limit should be rejected")
	}
}
