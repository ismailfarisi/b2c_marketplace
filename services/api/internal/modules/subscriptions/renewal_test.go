package subscriptions

import (
	"testing"

	"github.com/google/uuid"
)

func TestCanRenew(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		status, mode string
		rotation     *uuid.UUID
		renewedBy    *uuid.UUID
		want         bool
	}{
		{"completed", "none", nil, nil, true},
		{"cancelled", "auto", nil, nil, true},
		{"active", "manual", nil, nil, true},
		{"active", "none", nil, nil, true},
		{"active", "auto", nil, nil, false},
		{"pending_payment", "auto", nil, nil, false},
		{"suspended", "auto", nil, nil, false},
		{"completed", "none", &id, nil, false},
		{"completed", "none", nil, &id, false},
	}
	for _, c := range cases {
		if got := canRenew(c.status, c.mode, c.rotation, c.renewedBy); got != c.want {
			t.Errorf("canRenew(%s, %s, rotation=%v, renewed=%v) = %v, want %v",
				c.status, c.mode, c.rotation != nil, c.renewedBy != nil, got, c.want)
		}
	}
}
