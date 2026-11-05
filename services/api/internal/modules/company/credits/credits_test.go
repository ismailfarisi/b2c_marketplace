package credits

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestAllocate(t *testing.T) {
	open := func(dues ...int64) []Open {
		out := make([]Open, len(dues))
		for i, d := range dues {
			out[i] = Open{ID: uuid.New(), DueMinor: d}
		}
		return out
	}
	credit := func(amounts ...int64) []Credit {
		out := make([]Credit, len(amounts))
		for i, a := range amounts {
			out[i] = Credit{ID: uuid.New(), AmountMinor: a}
		}
		return out
	}
	for _, tc := range []struct {
		name    string
		open    []Open
		credits []Credit
		want    []Step
	}{
		{"no open statement carries everything", nil, credit(500), nil},
		{"fits on the statement", open(1500), credit(400),
			[]Step{{Credit: 0, Statement: 0, AmountMinor: 400}}},
		{"exactly settles it", open(400), credit(400),
			[]Step{{Credit: 0, Statement: 0, AmountMinor: 400}}},
		{"only the excess carries forward", open(1000), credit(1500),
			[]Step{{Credit: 0, Statement: 0, AmountMinor: 1000, Split: true}}},
		{"spills onto the next open statement", open(300, 1000), credit(500),
			[]Step{{Credit: 0, Statement: 0, AmountMinor: 300, Split: true}, {Credit: 0, Statement: 1, AmountMinor: 200}}},
		{"oldest credit first, fully paid statements skipped", open(0, 600), credit(400, 400),
			[]Step{{Credit: 0, Statement: 1, AmountMinor: 400}, {Credit: 1, Statement: 1, AmountMinor: 200, Split: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Allocate(tc.open, tc.credits); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Allocate = %+v, want %+v", got, tc.want)
			}
		})
	}
}
