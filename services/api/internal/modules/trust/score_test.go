package trust

import (
	"testing"
	"time"
)

var now = time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

func sig(kind string, severity int, state string, age time.Duration) Signal {
	return Signal{Kind: kind, Severity: severity, State: state, CreatedAt: now.Add(-age)}
}

func TestCleanSubjectScoresPerfect(t *testing.T) {
	score, band := Score(nil, now)
	if score != 100 || band != BandOK {
		t.Fatalf("got %d/%s, want 100/ok", score, band)
	}
}

func TestConfirmedCriminalityBlocksOnItsOwn(t *testing.T) {
	// Half of the pair of invariants the weights table exists to satisfy. If a
	// confirmed stolen vehicle needed corroboration, the table would be tuned
	// for tidiness rather than for the thing it exists to stop.
	for _, kind := range []string{
		"stolen_vehicle", "vin_mismatch", "odometer_rollback",
		"identity_mismatch", "advance_fee_language", "chat_advance_fee",
	} {
		score, band := Score([]Signal{sig(kind, 100, "confirmed", 0)}, now)
		if band != BandBlocked {
			t.Errorf("confirmed %s scored %d/%s, want blocked", kind, score, band)
		}
	}
}

func TestNoMachineSignalCanBlockOnItsOwn(t *testing.T) {
	// The other half, and the safety property of the whole file: a detector
	// firing once must not end an account before a human has looked. The
	// false-positive cost of a wrongly blocked dealer is a business
	// relationship, and there is no undo for the week they spent unable to
	// trade.
	//
	// It runs over every key in the map rather than a sample, because this
	// invariant is violated by *adding* a weight, not by editing an existing
	// one — which is precisely the change nobody thinks to test.
	for kind := range weights {
		score, band := Score([]Signal{sig(kind, 100, "open", 0)}, now)
		if band == BandBlocked {
			t.Errorf("a single open %s scored %d and blocked outright", kind, score)
		}
	}
	if got := Weight("anything-unweighted"); 100-got < BlockedBelow {
		t.Fatalf("DefaultWeight %v blocks on its own", got)
	}
}

func TestDismissedEvidenceCountsForNothing(t *testing.T) {
	dismissed := []Signal{
		sig("stolen_vehicle", 100, "dismissed", 0),
		sig("odometer_rollback", 100, "dismissed", 0),
		sig("vin_mismatch", 100, "dismissed", 0),
	}
	score, band := Score(dismissed, now)
	if score != 100 || band != BandOK {
		t.Fatalf("got %d/%s — a reviewed-and-cleared signal must not cost anything", score, band)
	}
}

func TestConfirmedWeighsMoreThanOpen(t *testing.T) {
	open, _ := Score([]Signal{sig("duplicate_listing", 100, "open", 0)}, now)
	confirmed, _ := Score([]Signal{sig("duplicate_listing", 100, "confirmed", 0)}, now)
	if confirmed >= open {
		t.Fatalf("confirmed %d is not worse than open %d", confirmed, open)
	}
}

func TestEvidenceDecays(t *testing.T) {
	// A seller who had one bad listing in March must not still be restricted in
	// December. The only path back to trading cannot be a support ticket.
	fresh, _ := Score([]Signal{sig("odometer_rollback", 100, "confirmed", 0)}, now)
	stale, _ := Score([]Signal{sig("odometer_rollback", 100, "confirmed", 90*24*time.Hour)}, now)
	if stale <= fresh {
		t.Fatalf("90-day-old evidence (%d) is not lighter than fresh (%d)", stale, fresh)
	}
	if stale < RestrictedBelow {
		t.Fatalf("after three half-lives a single signal still restricts (score %d)", stale)
	}
}

func TestOneHalfLifeHalvesThePenalty(t *testing.T) {
	// Guarding the decay constant itself, not the code around it.
	full, _ := Score([]Signal{sig("odometer_rollback", 100, "open", 0)}, now)
	half, _ := Score([]Signal{sig("odometer_rollback", 100, "open", HalfLife)}, now)
	fullPenalty, halfPenalty := 100-full, 100-half
	if halfPenalty*2 < fullPenalty-1 || halfPenalty*2 > fullPenalty+1 {
		t.Fatalf("penalty %d at one half-life is not half of %d", halfPenalty, fullPenalty)
	}
}

func TestOutsideTheWindowEvidenceIsIgnoredEntirely(t *testing.T) {
	score, _ := Score([]Signal{sig("stolen_vehicle", 100, "confirmed", Window+time.Hour)}, now)
	if score != 100 {
		t.Fatalf("got %d — evidence past the window must not be read", score)
	}
}

func TestManySmallSignalsAccumulate(t *testing.T) {
	// This is the point of the model. No single one of these means anything —
	// sharing a phone number, mentioning WhatsApp, pushing off-platform — and
	// the pattern of all of them across a week is exactly the shape being
	// looked for.
	var signals []Signal
	for i := 0; i < 8; i++ {
		signals = append(signals,
			sig("chat_phone", 60, "open", time.Duration(i)*24*time.Hour),
			sig("chat_messaging_handle", 60, "open", time.Duration(i)*24*time.Hour),
			sig("chat_off_platform_push", 60, "open", time.Duration(i)*24*time.Hour),
		)
	}
	score, band := Score(signals, now)
	if band == BandOK {
		t.Fatalf("got %d/%s — a sustained off-platform push must move the band", score, band)
	}
}

func TestSeverityScalesTheWeight(t *testing.T) {
	low, _ := Score([]Signal{sig("duplicate_listing", 20, "open", 0)}, now)
	high, _ := Score([]Signal{sig("duplicate_listing", 100, "open", 0)}, now)
	if low <= high {
		t.Fatalf("severity 20 (%d) should cost less than severity 100 (%d)", low, high)
	}
}

func TestUnknownKindStillCosts(t *testing.T) {
	// A detector shipped without a weights entry is a bug. Scoring it at zero
	// would make the bug invisible; scoring it at DefaultWeight makes it show
	// up as an unexplained band change, which is how it gets found.
	score, _ := Score([]Signal{sig("some_detector_nobody_weighted", 100, "confirmed", 0)}, now)
	if score == 100 {
		t.Fatal("an unweighted signal kind must not be free")
	}
}

func TestScoreIsClamped(t *testing.T) {
	var signals []Signal
	for i := 0; i < 50; i++ {
		signals = append(signals, sig("stolen_vehicle", 100, "confirmed", 0))
	}
	score, band := Score(signals, now)
	if score != 0 || band != BandBlocked {
		t.Fatalf("got %d/%s, want 0/blocked", score, band)
	}
}

func TestBandsAreOrdered(t *testing.T) {
	if !(BlockedBelow < RestrictedBelow && RestrictedBelow < WatchBelow) {
		t.Fatal("band thresholds are not monotonic")
	}
	for _, c := range []struct {
		score int
		want  Band
	}{
		{100, BandOK}, {80, BandOK}, {79, BandWatch}, {55, BandWatch},
		{54, BandRestricted}, {25, BandRestricted}, {24, BandBlocked}, {0, BandBlocked},
	} {
		if got := BandFor(c.score); got != c.want {
			t.Errorf("BandFor(%d) = %s, want %s", c.score, got, c.want)
		}
	}
}

func TestExplainAgreesWithScore(t *testing.T) {
	// The number in the appeal has to be the number the decision used. If
	// Explain were a reconstruction it would drift from Score the first time
	// either was touched, and the drift would only surface in front of a
	// suspended seller.
	signals := []Signal{
		sig("odometer_rollback", 80, "confirmed", 3*24*time.Hour),
		sig("chat_phone", 50, "open", time.Hour),
		sig("duplicate_listing", 90, "open", 10*24*time.Hour),
		sig("stolen_vehicle", 100, "dismissed", 0),
		sig("rapid_relist", 60, "open", Window+time.Hour),
	}
	score, _ := Score(signals, now)

	total := 0.0
	for _, c := range Explain(signals, now) {
		total += c.Points
	}
	// Within a point: both round independently at the end, so demanding exact
	// equality would make this test fail on a fixture whose penalty lands on
	// .5 without anything being wrong. The fixture is also chosen to stay off
	// the 0 floor — a clamped score agrees with nothing by construction, and
	// asserting against it would be asserting that the clamp exists.
	rebuilt := 100 - total
	if diff := rebuilt - float64(score); diff > 1 || diff < -1 {
		t.Fatalf("Explain sums to %.2f but Score says %d", rebuilt, score)
	}
	if score == 0 {
		t.Fatal("fixture is clamped — pick lighter evidence so this test compares arithmetic")
	}
}

func TestExplainRanksTheReasonsForTheDecision(t *testing.T) {
	signals := []Signal{
		sig("chat_phone", 100, "open", 0),
		sig("odometer_rollback", 100, "open", 0),
		sig("chat_email", 100, "open", 0),
	}
	got := Explain(signals, now)
	if got[0].Kind != "odometer_rollback" {
		t.Fatalf("largest contribution is %s, want odometer_rollback", got[0].Kind)
	}
}

func TestExplainMarksEvidenceItDidNotCount(t *testing.T) {
	got := Explain([]Signal{sig("stolen_vehicle", 100, "confirmed", Window+time.Hour)}, now)
	if len(got) != 1 || !got[0].Dropped {
		t.Fatalf("out-of-window evidence must be shown as dropped, got %+v", got)
	}
	if got[0].Points != 0 {
		t.Fatalf("dropped evidence contributed %v points", got[0].Points)
	}
}

// ── the figure shown to a reviewer ───────────────────────────────────────────

// QA found the fraud queue telling a moderator a confirmation would cost 90
// points when it cost 40. The displayed number was the kind's weight at full
// severity, doubled; the applied number is scaled by the signal's own severity
// and decayed by its age.
//
// A moderator deciding whether confirming crosses a restriction threshold was
// reading a figure consistently overstating the consequence of their decision.
func TestTheCostShownIsTheCostApplied(t *testing.T) {
	now := time.Now()
	issued := now.Add(-2 * time.Hour)

	const kind = "odometer_rollback"
	const severity = 45

	open := []Signal{{Kind: kind, Severity: severity, State: "open", CreatedAt: issued}}
	confirmed := []Signal{{Kind: kind, Severity: severity, State: "confirmed", CreatedAt: issued}}

	scoreOpen, _ := Score(open, now)
	scoreConfirmed, _ := Score(confirmed, now)
	applied := scoreOpen - scoreConfirmed

	shown := CostOfConfirming(kind, severity, issued, now)

	// Within a point, because both round independently.
	if diff := shown - applied; diff > 1 || diff < -1 {
		t.Fatalf("the queue shows %d points, confirming applies %d — a reviewer is "+
			"reasoning with the wrong number", shown, applied)
	}
}

func TestCostOfConfirmingScalesWithSeverity(t *testing.T) {
	now := time.Now()
	low := CostOfConfirming("odometer_rollback", 10, now, now)
	high := CostOfConfirming("odometer_rollback", 100, now, now)

	if low >= high {
		t.Fatalf("severity 10 costs %d and severity 100 costs %d — severity must "+
			"scale the penalty, or the field is decoration", low, high)
	}
}

// An old signal is worth less, and the figure a reviewer sees has to say so —
// otherwise confirming a nine-month-old report looks as consequential as
// confirming one from this morning.
func TestCostOfConfirmingDecaysWithAge(t *testing.T) {
	now := time.Now()
	fresh := CostOfConfirming("odometer_rollback", 50, now, now)
	old := CostOfConfirming("odometer_rollback", 50, now.Add(-4*HalfLife), now)

	if old >= fresh {
		t.Fatalf("a fresh signal costs %d and a four-half-life-old one costs %d", fresh, old)
	}
}

func TestAnUnknownKindStillHasACost(t *testing.T) {
	now := time.Now()
	if got := CostOfConfirming("invented_kind", 100, now, now); got <= 0 {
		t.Fatalf("got %d — an unrecognised kind must still carry the default weight, "+
			"or a typo in a policy code makes a confirmation free", got)
	}
}
