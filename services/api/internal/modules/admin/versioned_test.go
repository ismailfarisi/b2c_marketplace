package admin

import "testing"

// The segment count is the only number in the console that changes a decision
// while someone is typing, so it gets a test rather than a hope.
//
// Every case here is one an operator can hit by accident, and the failure in
// each is silent: the message sends, the customer receives it, and the bill
// arrives at the end of the month.
func TestSMSSegments(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty", "", 0},
		{"short ascii", "Your order is on its way.", 1},

		// The case that broke the first implementation. `{` and `}` are not in
		// the GSM-7 basic alphabet, so counting the raw template classified
		// every message containing a placeholder as UCS-2 — reporting a
		// 75-character English SMS as two segments when it is one. The braces
		// never reach a handset.
		{
			"placeholder does not force ucs-2",
			"Your delivery code is {{code}}. Please share it with the driver on arrival.",
			1,
		},

		// Arabic is entirely outside GSM-7, so the budget is 70 rather than 160.
		// An Arabic message *shorter* than an English one can cost twice as
		// much, which is the whole reason this is shown in the editor.
		{
			"arabic over 70 is two segments",
			"رمز الدخول الخاص بك هو {{code}} وينتهي خلال {{minutes}} دقيقة. لا تشاركه مع أحد.",
			2,
		},
		{"arabic under 70 is one segment", "رمز التحقق الخاص بك هو {{code}}", 1},

		// One character pasted from a word processor doubles the cost of an
		// otherwise plain English message. This is the single most expensive
		// invisible mistake available in a template editor.
		{
			"en dash forces ucs-2",
			"Your delivery code is {{code}} – share it with the driver on arrival please.",
			2,
		},

		// Concatenation overhead: a 161-character GSM-7 message is not 160 + 1,
		// because each part carries a header and the per-part budget drops to
		// 153.
		{"161 ascii is two segments", str(161), 2},
		{"160 ascii is one segment", str(160), 1},
		{"306 ascii is two segments", str(306), 2},
		{"307 ascii is three segments", str(307), 3},

		// An unclosed placeholder is broken template text that will be sent
		// literally, braces and all. Counted as written rather than silently
		// repaired — a segment count is not the place to hide a typo that is
		// about to go out to customers.
		{"unclosed placeholder counts its braces", "Code: {{code", 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SMSSegments(c.body); got != c.want {
				t.Errorf("SMSSegments(%q) = %d, want %d", c.body, got, c.want)
			}
		})
	}
}

func TestPlaceholders(t *testing.T) {
	body := "Hi {{name}}, your code is {{code}}. Say {{name}} again."
	got := Placeholders(body)
	want := []string{"name", "code"}

	if len(got) != len(want) {
		t.Fatalf("Placeholders() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			// Order matters: the editor lists them in the order they appear, and
			// a reviewer comparing two versions reads down the list.
			t.Errorf("Placeholders()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The guard that stops a delivery-code template from being saved without its
// code. Nothing downstream renders a template twice to check, so this is the
// only place the mistake can be caught.
func TestMissingPlaceholders(t *testing.T) {
	old := "Your code is {{code}}, valid {{minutes}} minutes."

	if lost := missingPlaceholders(old, "Your code is {{code}}."); len(lost) != 1 || lost[0] != "{{minutes}}" {
		t.Errorf("dropping one placeholder = %v, want [{{minutes}}]", lost)
	}
	if lost := missingPlaceholders(old, "Your order is on its way."); len(lost) != 2 {
		t.Errorf("dropping both = %v, want two entries", lost)
	}
	if lost := missingPlaceholders(old, old+" Do not share it."); len(lost) != 0 {
		t.Errorf("adding text = %v, want none lost", lost)
	}
	// Reordering is not a loss. A translator moving the code to the front of an
	// Arabic sentence is doing their job, and refusing it would make the guard
	// something people work around.
	if lost := missingPlaceholders(old, "{{minutes}} minutes: {{code}}"); len(lost) != 0 {
		t.Errorf("reordering = %v, want none lost", lost)
	}
}

func str(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
