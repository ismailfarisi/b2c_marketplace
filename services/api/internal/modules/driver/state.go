// Package driver is the driver-facing surface: shifts, a manifest, status
// transitions, proof of delivery, and cash.
//
// Like `dispatch`, it reads the fulfillment seam and nothing a vertical owns. A
// driver's screen shows an address, a window, a name, a phone number and a
// number of items — every one of which the fulfillment snapshotted at creation.
// The driver never learns whether the box contains a shawarma or a shirt, and
// the code that would tell them does not exist.
//
// PLAN.md §5 P4 calls the native driver app "the single most expensive item in
// this plan" and recommends launching with a web-based driver view instead. That
// advice is taken: this package is the API, `apps/web` is the client, and there
// is no native driver app. See docs/PHASE-7-CLIENTS-AND-LOGISTICS.md §3.
package driver

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/marketplace/api/internal/seam/fulfillment"
)

// transitions is the driver's half of the fulfillment lifecycle.
//
// A subset of what the status enum allows, on purpose. `cancelled` is reachable
// from the customer and the vendor but not from a driver — a driver who could
// cancel work has a way to make their route shorter, and the incentive is
// exactly wrong. They can fail a stop, which is a different thing with a reason
// attached and an attempt row behind it.
var transitions = map[fulfillment.Status][]fulfillment.Status{
	fulfillment.StatusScheduled:  {fulfillment.StatusDispatched},
	fulfillment.StatusDispatched: {fulfillment.StatusEnRoute, fulfillment.StatusFailed},
	fulfillment.StatusEnRoute:    {fulfillment.StatusArrived, fulfillment.StatusFailed},
	// Arrived → en_route is deliberately allowed: a driver who marks arrival at
	// the wrong building needs a way back that is not "fail the delivery".
	fulfillment.StatusArrived: {
		fulfillment.StatusCompleted, fulfillment.StatusFailed, fulfillment.StatusEnRoute,
	},
	fulfillment.StatusCompleted: {},
	fulfillment.StatusFailed:    {},
	fulfillment.StatusCancelled: {},
}

func CanTransition(from, to fulfillment.Status) bool {
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Terminal reports whether a driver can still act on this fulfillment.
func Terminal(s fulfillment.Status) bool { return len(transitions[s]) == 0 }

// ─────────────────────────────────────────────────────────────────────────────
// Proof
// ─────────────────────────────────────────────────────────────────────────────

type ProofKind string

const (
	ProofOTP           ProofKind = "otp"
	ProofSignature     ProofKind = "signature"
	ProofPhoto         ProofKind = "photo"
	ProofRecipientName ProofKind = "recipient_name"
	ProofLeftAtDoor    ProofKind = "left_at_door"
)

// strength orders the proof kinds by what they are worth in a dispute.
//
// OTP is the only one that proves the *customer* was there: a code sent to their
// phone and typed at the door. A signature proves someone signed, a photo proves
// a bag existed, and `left_at_door` proves nothing at all — it is named honestly
// so nobody builds a report that treats it as evidence.
func strength(k ProofKind) int {
	switch k {
	case ProofOTP:
		return 4
	case ProofSignature:
		return 3
	case ProofPhoto:
		return 2
	case ProofRecipientName:
		return 1
	default:
		return 0
	}
}

// RequiredProof is the weakest proof acceptable for a kind of work.
//
// A pickup needs less than a delivery, and that is not laxity: at a pickup the
// customer is handing something over, so their presence is self-evident and the
// disputed question is what was collected, which a photo answers. At a delivery
// the disputed question is whether anyone received it at all.
func RequiredProof(kind fulfillment.Kind, codMinor int64) ProofKind {
	// Anything with cash attached needs the strong proof regardless of kind.
	// "The driver says they collected AED 240" is not a position to defend.
	if codMinor > 0 {
		return ProofOTP
	}
	switch kind {
	case fulfillment.KindPickup:
		return ProofPhoto
	default:
		return ProofRecipientName
	}
}

// ProofAcceptable reports whether what the driver captured is enough.
func ProofAcceptable(captured ProofKind, kind fulfillment.Kind, codMinor int64) bool {
	return strength(captured) >= strength(RequiredProof(kind, codMinor))
}

// ─────────────────────────────────────────────────────────────────────────────
// OTP
// ─────────────────────────────────────────────────────────────────────────────

// OTPLength is four digits, not six.
//
// This is a delivery confirmation read aloud at a door, not an authentication
// factor: the attacker who could brute-force it is standing in front of the
// customer, and the cost of six digits is every customer reading two more
// numbers to a driver in the rain. The real protection is that the code is
// single-use and scoped to one fulfillment.
const OTPLength = 4

// GenerateOTP returns a code and its hash. Only the hash is stored — a database
// reader who can see every OTP can mark every delivery complete.
func GenerateOTP() (code string, hash string, err error) {
	digits := make([]byte, OTPLength)
	for i := range digits {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", "", fmt.Errorf("generate otp: %w", err)
		}
		digits[i] = byte('0' + n.Int64())
	}
	code = string(digits)
	return code, HashOTP(code), nil
}

func HashOTP(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// VerifyOTP compares in constant time.
//
// Timing-safe comparison on a four-digit code is close to theatre, and it costs
// one function call. The reason to do it anyway is that this is the pattern
// every other secret comparison in the codebase will be copied from.
func VerifyOTP(code, hash string) bool {
	if code == "" || hash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(HashOTP(code)), []byte(hash)) == 1
}

// ─────────────────────────────────────────────────────────────────────────────
// Geofence
// ─────────────────────────────────────────────────────────────────────────────

// ArrivalRadiusM is how close a driver must be to mark arrival.
//
// 250 metres. Generous because GPS in a dense urban block with towers on both
// sides is routinely 100 m out, and because a driver who cannot mark arrival
// stops using the app and calls the customer instead — at which point there is
// no record of anything.
const ArrivalRadiusM = 250

// ProofDistanceWarnM is the distance past which a completion is worth a look.
//
// Not a refusal. A driver legitimately completes from the building lobby, the
// service road, or the neighbouring tower's car park. What it does is put a
// number on the proof row, so "it never arrived" can be answered with "the
// driver was 40 m away" or "the driver was 3 km away" rather than with an
// opinion.
const ProofDistanceWarnM = 500
