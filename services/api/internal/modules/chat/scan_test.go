package chat

import (
	"strings"
	"testing"
)

func TestCleanMessageIsDeliveredUntouched(t *testing.T) {
	// The overwhelming majority of messages. If this ever fails, the scanner is
	// costing more conversations than it protects.
	body := "Is the car still available? Can I see it this weekend in Al Quoz?"
	got := Analyse(body)
	if got.Verdict != VerdictDeliver {
		t.Fatalf("verdict = %s, want deliver (%+v)", got.Verdict, got.Findings)
	}
	if got.Delivered != body {
		t.Fatalf("body was altered:\n got %q\nwant %q", got.Delivered, body)
	}
}

func TestPricesAndMileageAreNotPhoneNumbers(t *testing.T) {
	// The failure mode that would break the product. A car chat is full of long
	// numbers, and a scanner that redacts the price has removed the one thing
	// the two parties are there to discuss.
	for _, body := range []string{
		"Asking 145000 AED, slightly negotiable",
		"2021 model, 95000 km, AED 1,450,000 for the SVR",
		"VIN is JTMHY05JX04512341 if you want to check it",
		"Mileage 132500 km. Serviced at 130000.",
		"I paid 249999 for it new in 2019",
	} {
		got := Analyse(body)
		if got.Verdict != VerdictDeliver {
			t.Errorf("%q was flagged as %s: %+v", body, got.Verdict, got.Findings)
		}
	}
}

func TestUAEPhoneShapesAreCaught(t *testing.T) {
	cases := []string{
		"call 0501234567",
		"my mobile 050 123 4567",
		"+971 50 123 4567 anytime",
		"00971501234567",
		"971501234567",
		"just dial 971-50-123-4567",
		// The trunk zero dropped, which is how people dictate their own number.
		"501234567 is mine",
		// Landline.
		"office is 04 123 4567",
	}
	for _, body := range cases {
		got := Analyse(body)
		if !has(got, FindingPhone) {
			t.Errorf("%q: no phone found (%+v)", body, got.Findings)
			continue
		}
		if strings.Contains(got.Delivered, "1234567") {
			t.Errorf("%q: number survived redaction: %q", body, got.Delivered)
		}
	}
}

func TestArabicIndicDigitsAreFolded(t *testing.T) {
	// The detail that decides whether this works in this market at all. A
	// scanner written against [0-9] sees no digits here whatsoever, and every
	// determined evader would find that out within a week.
	body := "رقمي ٠٥٠١٢٣٤٥٦٧ اتصل بي"
	got := Analyse(body)
	if !has(got, FindingPhone) {
		t.Fatalf("Arabic-Indic digits were not recognised as a number: %+v", got.Findings)
	}
	if strings.Contains(got.Delivered, "٠٥٠١٢٣٤٥٦٧") {
		t.Fatalf("the number survived redaction: %q", got.Delivered)
	}
	// And the Arabic text around it must survive intact — a redaction that eats
	// the sentence is indistinguishable from a delivery failure.
	if !strings.Contains(got.Delivered, "اتصل بي") {
		t.Fatalf("surrounding text was destroyed: %q", got.Delivered)
	}
}

func TestExtendedArabicIndicDigitsAreFolded(t *testing.T) {
	body := "۰۵۰۱۲۳۴۵۶۷"
	if !has(Analyse(body), FindingPhone) {
		t.Fatal("Extended Arabic-Indic digits were not folded")
	}
}

func TestRedactionPreservesTheRestOfTheMessage(t *testing.T) {
	body := "Yes it is available, call me on 0501234567 to arrange a viewing"
	got := Analyse(body)
	if !strings.HasPrefix(got.Delivered, "Yes it is available") {
		t.Fatalf("prefix lost: %q", got.Delivered)
	}
	if !strings.HasSuffix(got.Delivered, "to arrange a viewing") {
		t.Fatalf("suffix lost: %q", got.Delivered)
	}
	if !strings.Contains(got.Delivered, Redaction) {
		t.Fatalf("redaction is invisible — the sender will just re-send: %q", got.Delivered)
	}
}

func TestMessagingHandlesAreRedacted(t *testing.T) {
	for _, body := range []string{
		"message me on whatsapp",
		"here: wa.me/971501234567",
		"add me on telegram",
		"واتساب افضل",
		"my snapchat is same name",
	} {
		got := Analyse(body)
		if got.Verdict == VerdictDeliver {
			t.Errorf("%q was delivered untouched: %+v", body, got.Findings)
		}
	}
}

func TestOverlappingSpansDoNotCorruptTheMessage(t *testing.T) {
	// "wa.me/971501234567" is a handle and a phone number in the same
	// characters. Redacting one after the other without merging would splice
	// the string at stale offsets.
	body := "here wa.me/971501234567 ok"
	got := Analyse(body)
	if strings.Contains(got.Delivered, "971501234567") {
		t.Fatalf("number survived: %q", got.Delivered)
	}
	if !strings.HasPrefix(got.Delivered, "here ") || !strings.HasSuffix(got.Delivered, " ok") {
		t.Fatalf("message corrupted around the overlap: %q", got.Delivered)
	}
}

func TestAdvanceFeeLanguageIsHeldNotRedacted(t *testing.T) {
	// The distinction the whole verdict model turns on. Stripping "western
	// union" from a scam leaves a fluent message that is still a scam; the only
	// useful response is a person reading it.
	body := "I am abroad, my shipping agent will collect. Please send the money first via Western Union."
	got := Analyse(body)
	if got.Verdict != VerdictHold {
		t.Fatalf("verdict = %s, want hold: %+v", got.Verdict, got.Findings)
	}
	if !strings.Contains(strings.ToLower(got.Delivered), "western union") {
		t.Fatal("advance-fee language must reach the reviewer intact, not be redacted away")
	}
}

func TestArabicAdvanceFeeLanguageIsCaught(t *testing.T) {
	body := "أنا خارج البلد، أرسل عربون قبل المعاينة"
	if Analyse(body).Verdict != VerdictHold {
		t.Fatal("the Arabic phrasing of the same scam must be held too")
	}
}

func TestMessageThatIsOnlyContactDetailsIsHeld(t *testing.T) {
	// Someone determined to get a number across, not someone being careless.
	// Redacting and delivering an empty message helps nobody.
	got := Analyse("0501234567")
	if got.Verdict != VerdictHold {
		t.Fatalf("verdict = %s, want hold (delivered %q)", got.Verdict, got.Delivered)
	}
}

func TestKindsAreDeduplicated(t *testing.T) {
	// The message row stores these and the fraud detectors aggregate over them.
	// Three phone numbers in one message is one kind of evidence, not three.
	got := Analyse("call 0501234567 or 0559876543 or 0561112222")
	kinds := got.Kinds()
	if len(kinds) != 1 || kinds[0] != string(FindingPhone) {
		t.Fatalf("kinds = %v, want exactly [phone]", kinds)
	}
	if len(got.Findings) != 3 {
		t.Fatalf("expected 3 findings for the reviewer, got %d", len(got.Findings))
	}
}

func TestScannerIsPure(t *testing.T) {
	// Called twice with the same input, twice from the message service and once
	// from a replay during an appeal. If it ever disagreed with itself, a held
	// message could not be re-adjudicated.
	body := "call me on ٠٥٠١٢٣٤٥٦٧ or wa.me/971501234567"
	a, b := Analyse(body), Analyse(body)
	if a.Delivered != b.Delivered || a.Verdict != b.Verdict || len(a.Findings) != len(b.Findings) {
		t.Fatal("Analyse is not deterministic")
	}
}

func has(s Scan, kind FindingKind) bool {
	for _, f := range s.Findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}
