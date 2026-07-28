package driver

import (
	"testing"

	"github.com/marketplace/api/internal/seam/fulfillment"
)

func TestADriverCannotCancelWork(t *testing.T) {
	// A driver who can cancel has a way to make their route shorter, and the
	// incentive points exactly the wrong way. Failing a stop is the honest
	// alternative: it carries a reason and leaves an attempt row.
	for from := range transitions {
		if CanTransition(from, fulfillment.StatusCancelled) {
			t.Errorf("%s -> cancelled is reachable from the driver app", from)
		}
	}
}

func TestArrivedCanGoBackToEnRoute(t *testing.T) {
	// A driver who marked arrival at the wrong building needs a way back that is
	// not "fail the delivery". Without it they fail it and redeliver, and the
	// failure statistics stop meaning anything.
	if !CanTransition(fulfillment.StatusArrived, fulfillment.StatusEnRoute) {
		t.Fatal("a mistaken arrival must be reversible")
	}
}

func TestCompletionRequiresArrival(t *testing.T) {
	// Completing from `dispatched` would mean marking a delivery done without
	// ever claiming to have been there.
	for _, from := range []fulfillment.Status{
		fulfillment.StatusScheduled, fulfillment.StatusDispatched, fulfillment.StatusEnRoute,
	} {
		if CanTransition(from, fulfillment.StatusCompleted) {
			t.Errorf("%s -> completed skips arrival", from)
		}
	}
	if !CanTransition(fulfillment.StatusArrived, fulfillment.StatusCompleted) {
		t.Fatal("arrived -> completed must be allowed")
	}
}

func TestTerminalStatesAreTerminal(t *testing.T) {
	for _, s := range []fulfillment.Status{
		fulfillment.StatusCompleted, fulfillment.StatusFailed, fulfillment.StatusCancelled,
	} {
		if !Terminal(s) {
			t.Errorf("%s should be terminal for a driver", s)
		}
	}
	if Terminal(fulfillment.StatusEnRoute) {
		t.Fatal("en_route is not terminal")
	}
}

// ── proof ────────────────────────────────────────────────────────────────────

func TestCashAlwaysRequiresTheStrongestProof(t *testing.T) {
	// "The driver says they collected AED 240" is not a position anyone can
	// defend. Cash raises the bar regardless of what kind of work it is.
	for _, kind := range []fulfillment.Kind{
		fulfillment.KindPickup, fulfillment.KindDelivery, fulfillment.KindVisit,
	} {
		if got := RequiredProof(kind, 24000); got != ProofOTP {
			t.Errorf("%s with cash requires %s, want otp", kind, got)
		}
		for _, weak := range []ProofKind{ProofLeftAtDoor, ProofRecipientName, ProofPhoto, ProofSignature} {
			if ProofAcceptable(weak, kind, 24000) {
				t.Errorf("%s accepted for a cash %s", weak, kind)
			}
		}
	}
}

func TestLeftAtDoorIsNeverEnough(t *testing.T) {
	// Named honestly so nobody builds a report that treats it as evidence. It
	// proves the driver says so, and nothing else.
	for _, kind := range []fulfillment.Kind{
		fulfillment.KindPickup, fulfillment.KindDelivery, fulfillment.KindVisit,
	} {
		if ProofAcceptable(ProofLeftAtDoor, kind, 0) {
			t.Errorf("left_at_door accepted for %s", kind)
		}
	}
}

func TestStrongerProofIsAlwaysAccepted(t *testing.T) {
	// A driver who captured an OTP when a photo was required has over-delivered,
	// and refusing that would train them to capture the minimum.
	if !ProofAcceptable(ProofOTP, fulfillment.KindPickup, 0) {
		t.Fatal("an OTP must satisfy a photo requirement")
	}
	if !ProofAcceptable(ProofSignature, fulfillment.KindDelivery, 0) {
		t.Fatal("a signature must satisfy a name requirement")
	}
}

func TestPickupNeedsMoreThanADelivery(t *testing.T) {
	// At a pickup the customer is handing something over, so their presence is
	// self-evident and the disputed question is *what was collected* — which a
	// photo answers and a name does not.
	if strength(RequiredProof(fulfillment.KindPickup, 0)) <=
		strength(RequiredProof(fulfillment.KindDelivery, 0)) {
		t.Fatal("a pickup should require at least as much proof as a delivery")
	}
}

// ── OTP ──────────────────────────────────────────────────────────────────────

func TestOTPRoundTrips(t *testing.T) {
	code, hash, err := GenerateOTP()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != OTPLength {
		t.Fatalf("got %d digits, want %d", len(code), OTPLength)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Fatalf("non-digit %q in code %q", r, code)
		}
	}
	if !VerifyOTP(code, hash) {
		t.Fatal("the generated code does not verify against its own hash")
	}
}

func TestOTPHashIsNotTheCode(t *testing.T) {
	// A database reader who can see every OTP can mark every delivery complete.
	code, hash, _ := GenerateOTP()
	if hash == code {
		t.Fatal("the OTP is being stored in clear")
	}
	if len(hash) != 64 {
		t.Fatalf("hash is %d chars, expected a hex sha256", len(hash))
	}
}

func TestWrongOTPFails(t *testing.T) {
	_, hash, _ := GenerateOTP()
	for _, wrong := range []string{"0000", "9999", "", "12345", "abcd"} {
		if VerifyOTP(wrong, hash) {
			t.Errorf("%q verified against someone else's hash", wrong)
		}
	}
	if VerifyOTP("1234", "") {
		t.Fatal("an empty hash must never verify — that is a fulfillment with no OTP issued")
	}
}

func TestOTPsDiffer(t *testing.T) {
	// Not a randomness test — a check that the generator is not returning a
	// constant, which is the way this goes wrong in practice.
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, _, _ := GenerateOTP()
		seen[code] = true
	}
	if len(seen) < 50 {
		t.Fatalf("only %d distinct codes in 200 draws", len(seen))
	}
}

func TestGeofenceIsGenerous(t *testing.T) {
	// A driver who cannot mark arrival stops using the app and calls the
	// customer instead — at which point there is no record of anything. GPS in a
	// dense block is routinely 100 m out.
	if ArrivalRadiusM < 150 {
		t.Fatalf("ArrivalRadiusM = %d is tighter than urban GPS error", ArrivalRadiusM)
	}
	if ProofDistanceWarnM <= ArrivalRadiusM {
		t.Fatal("the proof-distance warning must sit outside the arrival radius, or every arrival warns")
	}
}
