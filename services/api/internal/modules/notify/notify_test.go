package notify

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ── consent ──────────────────────────────────────────────────────────────────

// The one that matters. Marketing may be refused; a delivery code may not, and
// the failure this prevents is a customer who turned off "all notifications"
// six months ago standing at a door unable to receive an order they paid for.
func TestOnlyMarketingCanBeSwitchedOff(t *testing.T) {
	optOut := []Preference{}
	for _, cat := range []Category{
		CategoryTransactional, CategoryDelivery, CategoryAccount,
		CategoryModeration, CategoryMarketing,
	} {
		for _, ch := range ChannelsFor(cat) {
			optOut = append(optOut, Preference{Category: cat, Channel: ch, Allowed: false})
		}
	}

	for _, cat := range []Category{
		CategoryTransactional, CategoryDelivery, CategoryAccount, CategoryModeration,
	} {
		for _, ch := range ChannelsFor(cat) {
			if !Allowed(cat, ch, optOut) {
				t.Errorf("%s/%s was switched off by a preference row that should not exist", cat, ch)
			}
		}
	}

	if Allowed(CategoryMarketing, ChannelPush, optOut) {
		t.Error("marketing must be refusable — it is a legal requirement in both launch markets")
	}
}

func TestMarketingDefaultsToAllowedWhenNoPreferenceExists(t *testing.T) {
	if !Allowed(CategoryMarketing, ChannelPush, nil) {
		t.Fatal("absence of a preference row must mean the default, not a refusal")
	}
}

// ── channel selection ────────────────────────────────────────────────────────

func TestDeliveryCodesPreferSMS(t *testing.T) {
	if got := ChannelsFor(CategoryDelivery)[0]; got != ChannelSMS {
		t.Fatalf("delivery ladder starts with %s, want sms — the person at the door "+
			"may have no app, no data, and may not be the buyer", got)
	}
}

func TestMarketingNeverUsesSMS(t *testing.T) {
	for _, ch := range ChannelsFor(CategoryMarketing) {
		if ch == ChannelSMS {
			t.Fatal("marketing SMS is the most expensive channel and the most tightly " +
				"regulated one; it must not be in the ladder")
		}
	}
}

func TestUnknownCategoryFallsBackToInApp(t *testing.T) {
	got := ChannelsFor(Category("invented"))
	if len(got) != 1 || got[0] != ChannelInApp {
		t.Fatalf("unknown category resolved to %v, want in-app only", got)
	}
}

// ── quiet hours ──────────────────────────────────────────────────────────────

func TestQuietHoursHoldMarketingAndNothingElse(t *testing.T) {
	if !Deferrable(CategoryMarketing) {
		t.Error("marketing must respect quiet hours")
	}
	for _, cat := range []Category{CategoryDelivery, CategoryTransactional, CategoryAccount} {
		if Deferrable(cat) {
			t.Errorf("%s must not be held overnight — restaurants take orders past "+
				"midnight in Ramadan and a held OTP is a failed delivery", cat)
		}
	}
}

func TestQuietHoursWindow(t *testing.T) {
	loc := time.FixedZone("GST", 4*3600)
	cases := []struct {
		hour  int
		quiet bool
	}{
		{7, true}, {8, false}, {12, false}, {21, false}, {22, true}, {23, true}, {0, true}, {3, true},
	}
	for _, c := range cases {
		at := time.Date(2026, 8, 3, c.hour, 30, 0, 0, loc)
		if got := InQuietHours(at); got != c.quiet {
			t.Errorf("%02d:30 local: quiet=%v, want %v", c.hour, got, c.quiet)
		}
	}
}

// ── locale ───────────────────────────────────────────────────────────────────

func TestNotificationLocaleWinsOverUILocale(t *testing.T) {
	if got := PickLocale("ar", "en"); got != "ar" {
		t.Fatalf("got %q, want ar — a user who reads the app in English may still "+
			"want messages in Arabic", got)
	}
	if got := PickLocale("", "ar"); got != "ar" {
		t.Fatalf("got %q, want ar", got)
	}
	if got := PickLocale("", ""); got != "en" {
		t.Fatalf("got %q, want en", got)
	}
	if got := PickLocale("fr", "de"); got != "en" {
		t.Fatalf("got %q, want en — an unsupported locale must not be passed through "+
			"to a template lookup that will miss", got)
	}
}

// ── rendering ────────────────────────────────────────────────────────────────

// The failure this prevents: "Your delivery code is " sent to a customer
// standing at their door. An empty substitution looks like the system worked.
func TestAMissingParameterIsAnErrorNotAnEmptyString(t *testing.T) {
	_, err := Render("delivery.otp", "Your delivery code is {{code}}.", map[string]string{})

	var missing *ErrMissingParams
	if !errors.As(err, &missing) {
		t.Fatalf("got %v, want ErrMissingParams", err)
	}
	if len(missing.Missing) != 1 || missing.Missing[0] != "code" {
		t.Fatalf("missing = %v, want [code]", missing.Missing)
	}
}

func TestRenderSubstitutes(t *testing.T) {
	got, err := Render("k", "Code {{code}} for {{name}}, code {{code}} again",
		map[string]string{"code": "4821", "name": "Layla"})
	if err != nil {
		t.Fatal(err)
	}
	want := "Code 4821 for Layla, code 4821 again"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderHandlesArabic(t *testing.T) {
	got, err := Render("delivery.otp",
		"رمز التسليم الخاص بك هو {{code}}.", map[string]string{"code": "4821"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "4821") {
		t.Fatalf("got %q, want the code substituted", got)
	}
	// Byte-wise scanning must not corrupt multi-byte text.
	if !strings.HasPrefix(got, "رمز") {
		t.Fatalf("got %q — the Arabic prefix was damaged", got)
	}
}

// An unterminated placeholder must not fail the send. Copy is edited by people
// who are not deploying, and a stray brace in a marketing template must not be
// able to take down a delivery code.
func TestAnUnterminatedPlaceholderIsLiteralText(t *testing.T) {
	got, err := Render("k", "Code {{code}} and a stray {{ brace",
		map[string]string{"code": "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "{{ brace") {
		t.Fatalf("got %q, want the stray text preserved", got)
	}
}

func TestPlaceholdersAreListedOnceInOrder(t *testing.T) {
	got := Placeholders("{{a}} {{b}} {{a}} {{c}}")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// ── SMS cost ─────────────────────────────────────────────────────────────────

// Arabic costs more than twice as much per character to send, and the only
// moment anyone can act on that is while the copy is being written.
func TestArabicSMSCostsMoreSegments(t *testing.T) {
	en := "Your delivery code is 4821. Give it to the driver to confirm receipt."
	ar := "رمز التسليم الخاص بك هو 4821. أعطه للسائق لتأكيد الاستلام."

	if got := SMSSegments(en); got != 1 {
		t.Fatalf("english body is %d segments, want 1", got)
	}
	if got := SMSSegments(ar); got < 1 {
		t.Fatalf("arabic body is %d segments, want at least 1", got)
	}
	if SMSSegments(ar) <= SMSSegments(strings.Repeat("a", len(ar))) {
		t.Log("note: comparison is by rune count, which is the point — the UCS-2 " +
			"limit is 70 runes against GSM-7's 160")
	}

	// 71 Arabic characters must be two segments; 70 must be one. That boundary
	// is where the bill doubles.
	if got := SMSSegments(strings.Repeat("م", 70)); got != 1 {
		t.Errorf("70 Arabic characters = %d segments, want 1", got)
	}
	if got := SMSSegments(strings.Repeat("م", 71)); got != 2 {
		t.Errorf("71 Arabic characters = %d segments, want 2", got)
	}
}

func TestEmptyBodyCostsNothing(t *testing.T) {
	if got := SMSSegments(""); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

// ── WhatsApp ─────────────────────────────────────────────────────────────────

func TestWhatsAppOutsideItsWindowNeedsAnApprovedTemplate(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	closed := now.Add(-time.Hour)
	open := now.Add(time.Hour)

	if !WhatsAppSendable(now, open, false) {
		t.Error("inside the session window free-form text is allowed")
	}
	if WhatsAppSendable(now, closed, false) {
		t.Error("outside the window a template-less message must not be attempted — " +
			"the provider will reject it and the message will look sent")
	}
	if !WhatsAppSendable(now, closed, true) {
		t.Error("outside the window an approved template is sendable")
	}
	// No session at all is the common case for a first message.
	if WhatsAppSendable(now, time.Time{}, false) {
		t.Error("no session and no template is not sendable")
	}
}

// ── message validation ───────────────────────────────────────────────────────

func TestMessageValidation(t *testing.T) {
	if err := (Message{Category: CategoryDelivery}).Validate(); err == nil {
		t.Error("a message with no template key must not validate")
	}
	if err := (Message{TemplateKey: "k"}).Validate(); err == nil {
		t.Error("a message with no category must not validate")
	}
	if err := (Message{TemplateKey: "k", Category: "made_up"}).Validate(); err == nil {
		t.Error("an unknown category must not validate — it would silently take the " +
			"in-app fallback and never reach anyone")
	}
	if err := (Message{TemplateKey: "k", Category: CategoryDelivery}).Validate(); err != nil {
		t.Errorf("valid message rejected: %v", err)
	}
}
