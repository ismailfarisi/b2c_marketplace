package subscriptions

import "testing"

func TestAssignWeeksSpreadsLoad(t *testing.T) {
	// Kitchen 0 is busy in week 1, kitchen 1 in week 2.
	load := [RotationKitchens][RotationKitchens]int{
		{5, 0, 0, 0},
		{0, 5, 0, 0},
		{0, 0, 0, 0},
		{0, 0, 0, 0},
	}
	got := AssignWeeks(load)
	if got[0] == 0 || got[1] == 1 {
		t.Fatalf("busy kitchen kept its busy week: %v", got)
	}
	seen := map[int]bool{}
	for _, k := range got {
		seen[k] = true
	}
	if len(seen) != RotationKitchens {
		t.Fatalf("not a permutation: %v", got)
	}
}

func TestAssignWeeksKeepsOrderOnTie(t *testing.T) {
	var load [RotationKitchens][RotationKitchens]int
	if got := AssignWeeks(load); got != [RotationKitchens]int{0, 1, 2, 3} {
		t.Fatalf("tie should keep customer order, got %v", got)
	}
}
