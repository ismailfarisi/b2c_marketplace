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

func TestAssignWeeksAvoidingKeepsKitchenOutOfWeekOne(t *testing.T) {
	var load [RotationKitchens][RotationKitchens]int
	for ban := 0; ban < RotationKitchens; ban++ {
		got := AssignWeeksAvoiding(load, ban)
		if got[0] == ban {
			t.Fatalf("kitchen %d must not cook week 1, got %v", ban, got)
		}
		seen := map[int]bool{}
		for _, k := range got {
			seen[k] = true
		}
		if len(seen) != RotationKitchens {
			t.Fatalf("every kitchen must cook one week, got %v", got)
		}
	}
	// Even when week 1 is the banned kitchen's only light week.
	load = [RotationKitchens][RotationKitchens]int{
		{0, 9, 9, 9},
		{5, 0, 5, 5},
		{5, 5, 0, 5},
		{5, 5, 5, 0},
	}
	if got := AssignWeeksAvoiding(load, 0); got[0] == 0 {
		t.Fatalf("ban must win over load, got %v", got)
	}
}
