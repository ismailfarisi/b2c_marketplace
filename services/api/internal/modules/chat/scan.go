// Package chat is buyer↔seller messaging for classified listings.
//
// It exists for one reason, stated in SCOPE-EXTENSIONS §3: "exposing seller
// phone numbers invites spam and advance-fee fraud". Chat is what lets the
// phone number stay private. That makes the scanner in this file load-bearing
// rather than a nicety — a chat product that passes a phone number through in
// the first message has created the fraud surface without the protection, and
// would be worse than publishing the number honestly.
package chat

import (
	"strings"
	"unicode"
)

type FindingKind string

const (
	FindingPhone       FindingKind = "phone"
	FindingEmail       FindingKind = "email"
	FindingHandle      FindingKind = "messaging_handle"
	FindingLink        FindingKind = "external_link"
	FindingAdvanceFee  FindingKind = "advance_fee_language"
	FindingOffPlatform FindingKind = "off_platform_push"
)

type Verdict string

const (
	// VerdictDeliver — nothing found. The overwhelming majority.
	VerdictDeliver Verdict = "deliver"
	// VerdictRedact — contact details removed, message delivered.
	//
	// Redaction rather than blocking, because most people sharing a number are
	// not defrauding anyone: they are trying to arrange a viewing and the app
	// felt slower than WhatsApp. Blocking them teaches them the product is
	// broken; redacting teaches them where the conversation lives.
	VerdictRedact Verdict = "redact"
	// VerdictHold — a human decides before the recipient sees anything.
	VerdictHold Verdict = "hold"
)

type Finding struct {
	Kind FindingKind
	// Match is the text that triggered it, kept for the reviewer. It is the
	// evidence; the delivered message no longer contains it.
	Match string
	// From and To are rune offsets into the original body.
	From, To int
}

type Scan struct {
	// Delivered is what the recipient sees. Equal to the input when the verdict
	// is deliver.
	Delivered string
	Findings  []Finding
	Verdict   Verdict
}

// Redaction is what replaces a removed span. Visible on purpose: a silently
// deleted phone number reads as a bug or a delivery failure, and the sender
// re-sends it three more ways.
const Redaction = "[hidden]"

// Analyse scans one message.
//
// Pure, and deliberately so. Every rule here is a policy judgement that will be
// argued about — where the phone threshold sits, whether "shipping agent" is
// evidence — and an argument you can settle with a table-driven test is a much
// shorter argument than one you settle by reading production logs.
func Analyse(body string) Scan {
	runes := []rune(body)
	folded := fold(runes)
	lower := strings.ToLower(string(folded))

	var findings []Finding
	findings = append(findings, scanPhones(runes, folded)...)
	findings = append(findings, scanTokens(runes, lower)...)
	findings = append(findings, scanPhrases(runes, lower)...)

	if len(findings) == 0 {
		return Scan{Delivered: body, Verdict: VerdictDeliver}
	}

	verdict := VerdictRedact
	for _, f := range findings {
		if f.Kind == FindingAdvanceFee {
			// Advance-fee language is not a contact detail to strip. It is a
			// claim about how the transaction will work, and the only useful
			// response is a person reading it.
			verdict = VerdictHold
		}
	}

	delivered := redact(runes, findings)
	// A message that is nothing but redactions is someone determined to get a
	// number across, not someone being careless. Send it to a human.
	if verdict == VerdictRedact && strings.TrimSpace(stripRedactions(delivered)) == "" {
		verdict = VerdictHold
	}

	return Scan{Delivered: delivered, Findings: findings, Verdict: verdict}
}

// ─────────────────────────────────────────────────────────────────────────────
// Digit folding
// ─────────────────────────────────────────────────────────────────────────────

// fold maps Arabic-Indic and Extended Arabic-Indic digits onto ASCII, rune for
// rune.
//
// This is the detail that decides whether the scanner works in this market at
// all. A phone number typed on an Arabic keyboard is ٠٥٠١٢٣٤٥٦٧, and a scanner
// written against [0-9] does not see a single digit in it. Every serious
// evasion attempt in the region will use these characters, and they are not
// evasion — they are how half the users type.
//
// One-to-one on purpose: the returned slice has the same length as the input,
// so a match found in the folded text has the same rune offsets in the original
// and the redaction lands where the user actually typed.
func fold(in []rune) []rune {
	out := make([]rune, len(in))
	for i, r := range in {
		switch {
		case r >= 0x0660 && r <= 0x0669: // Arabic-Indic ٠١٢٣٤٥٦٧٨٩
			out[i] = '0' + (r - 0x0660)
		case r >= 0x06F0 && r <= 0x06F9: // Extended Arabic-Indic ۰۱۲۳۴۵۶۷۸۹
			out[i] = '0' + (r - 0x06F0)
		default:
			out[i] = r
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Phone numbers
// ─────────────────────────────────────────────────────────────────────────────

// isSeparator lists the characters that can sit inside a written phone number.
//
// The last three are the ones a Latin-alphabet developer never thinks of and
// this market produces constantly: a non-breaking space from a paste, and the
// two bidirectional marks a phone keyboard inserts around a Latin-digit number
// sitting inside Arabic text. Treating those as terminators would split every
// such number into two short runs, neither of which matches.
func isSeparator(r rune) bool {
	switch r {
	case ' ', '-', '.', '(', ')', ' ', '‏', '‎':
		return true
	}
	return false
}

// scanPhones finds digit runs and asks whether each one is shaped like a phone
// number.
//
// "Nine or more digits" on its own is not usable here, and getting that wrong
// is the failure mode that matters: a car chat is full of long numbers. A
// listing at AED 1,450,000 with 95,000 km on a 2021 model is three numbers in
// one sentence, and a naive length rule redacts the price — which breaks the
// conversation the product exists to enable.
//
// So the rule is shape, not length: Gulf country codes, local trunk formats,
// bare UAE mobiles, or an explicit international +. A price never matches any
// of them.
func scanPhones(orig, folded []rune) []Finding {
	var findings []Finding
	i := 0
	for i < len(folded) {
		if !unicode.IsDigit(folded[i]) && folded[i] != '+' {
			i++
			continue
		}

		start := i
		var digits strings.Builder
		plus := folded[i] == '+'
		if plus {
			i++
		}
		lastDigit := start
		for i < len(folded) {
			r := folded[i]
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
				lastDigit = i
				i++
				continue
			}
			// A separator only continues the run if a digit follows it; a
			// trailing space is not part of the number.
			if isSeparator(r) && i+1 < len(folded) && folded[i+1] >= '0' && folded[i+1] <= '9' {
				i++
				continue
			}
			break
		}

		if looksLikePhone(digits.String(), plus) {
			findings = append(findings, Finding{
				Kind:  FindingPhone,
				Match: string(orig[start : lastDigit+1]),
				From:  start,
				To:    lastDigit + 1,
			})
		}
		if i == start {
			i++
		}
	}
	return findings
}

// looksLikePhone tests the shapes that actually occur here.
func looksLikePhone(digits string, explicitPlus bool) bool {
	n := len(digits)
	switch {
	case explicitPlus:
		// E.164. The same shape users_phone_e164 enforces on the users table.
		return n >= 8 && n <= 15 && digits[0] != '0'
	case strings.HasPrefix(digits, "00"):
		// International access code.
		return n >= 10 && n <= 17
	case strings.HasPrefix(digits, "971") || strings.HasPrefix(digits, "966") ||
		strings.HasPrefix(digits, "968") || strings.HasPrefix(digits, "973") ||
		strings.HasPrefix(digits, "974") || strings.HasPrefix(digits, "965"):
		// Gulf country codes without a plus. Twelve digits for a UAE mobile,
		// nine to twelve across the rest.
		return n >= 11 && n <= 13
	case strings.HasPrefix(digits, "0"):
		// Local trunk format: 050 123 4567, 04 123 4567.
		return n >= 9 && n <= 11
	case strings.HasPrefix(digits, "5"):
		// A bare UAE mobile with the trunk zero dropped, which is how people
		// dictate their own number.
		return n == 9
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Tokens and phrases
// ─────────────────────────────────────────────────────────────────────────────

// handles are the places a conversation goes to die.
//
// Not because the apps are bad, but because a deal moved to WhatsApp has no
// record: no report button, no evidence for a dispute, and no way to tell a
// buyer that the seller they are talking to has three open fraud signals.
var handles = []string{
	"whatsapp", "wa.me", "واتساب", "واتس اب",
	"telegram", "t.me", "تليجرام", "تلجرام",
	"botim", "بوتيم", "imo.im",
	"snapchat", "سناب شات", "سناب",
	"instagram", "انستقرام", "انستغرام",
	"viber", "signal.me",
}

var linkMarkers = []string{"http://", "https://", "www."}

var emailMarkers = []string{"@gmail", "@hotmail", "@outlook", "@yahoo", "@icloud", "@proton"}

// advanceFee is the vocabulary of the scam this category is defined by.
//
// The pattern: the "seller" is abroad or the "buyer" will send a shipping
// agent, money moves before anyone sees the car, and the instrument is one that
// cannot be reversed. Every phrase here appears in the middle of an otherwise
// normal-sounding message, which is why this list beats a classifier: it is
// specific, auditable, and a moderator can see exactly why a message was held.
var advanceFee = []string{
	"western union", "moneygram", "money gram",
	"shipping agent", "shipping company", "courier will collect",
	"gift card", "itunes card", "steam card",
	"i am abroad", "i'm abroad", "currently abroad", "out of the country",
	"deposit to secure", "deposit first", "pay a deposit before",
	"customs clearance", "clearance fee", "release fee",
	"send the money first", "transfer first", "advance payment",
	"حوالة", "ويسترن يونيون", "عربون قبل", "أنا خارج البلد", "رسوم التخليص",
}

var offPlatform = []string{
	"call me on", "call me at", "my number is", "here is my number",
	"text me on", "message me on", "contact me on",
	"رقمي هو", "اتصل بي على", "كلمني على",
}

func scanTokens(orig []rune, lower string) []Finding {
	var findings []Finding
	// Offsets are in runes throughout, so a match in the lowercased folded
	// string points at the same characters in the original. Working in bytes
	// here would misplace every redaction in an Arabic message.
	lowerRunes := []rune(lower)
	add := func(kind FindingKind, needles []string) {
		for _, needle := range needles {
			n := []rune(needle)
			from := 0
			for {
				idx := indexRunes(lowerRunes, n, from)
				if idx < 0 {
					break
				}
				end := min(idx+len(n), len(orig))
				findings = append(findings, Finding{
					Kind: kind, Match: string(orig[idx:end]),
					From: idx, To: end,
				})
				from = idx + len(n)
			}
		}
	}
	add(FindingHandle, handles)
	add(FindingLink, linkMarkers)
	add(FindingEmail, emailMarkers)
	return findings
}

func scanPhrases(orig []rune, lower string) []Finding {
	var findings []Finding
	lowerRunes := []rune(lower)
	check := func(kind FindingKind, needles []string) {
		for _, needle := range needles {
			idx := indexRunes(lowerRunes, []rune(needle), 0)
			if idx < 0 {
				continue
			}
			end := min(idx+len([]rune(needle)), len(orig))
			findings = append(findings, Finding{
				Kind: kind, Match: string(orig[idx:end]), From: idx, To: end,
			})
		}
	}
	check(FindingAdvanceFee, advanceFee)
	check(FindingOffPlatform, offPlatform)
	return findings
}

func indexRunes(haystack, needle []rune, from int) int {
	if len(needle) == 0 || from < 0 {
		return -1
	}
	for i := from; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// ─────────────────────────────────────────────────────────────────────────────
// Redaction
// ─────────────────────────────────────────────────────────────────────────────

// redact removes the contact spans and leaves the phrase findings in place.
//
// Advance-fee language is not redacted, and that is deliberate. It is not a
// detail to strip — it is the message. Removing "western union" from an
// otherwise fluent scam leaves a message that reads perfectly and is still a
// scam, which is why those findings hold the message for a human instead.
func redact(orig []rune, findings []Finding) string {
	spans := make([]Finding, 0, len(findings))
	for _, f := range findings {
		switch f.Kind {
		case FindingPhone, FindingEmail, FindingHandle, FindingLink:
			spans = append(spans, f)
		}
	}
	if len(spans) == 0 {
		return string(orig)
	}

	// Sort by start, then merge overlaps. Overlaps are normal: "wa.me/9715..."
	// is a handle and a phone number in the same characters, and redacting one
	// after the other without merging would corrupt the offsets of the second.
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].From < spans[j-1].From; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}

	var b strings.Builder
	cursor := 0
	for _, s := range spans {
		from, to := s.From, s.To
		if from < cursor {
			from = cursor
		}
		if to <= from {
			continue
		}
		b.WriteString(string(orig[cursor:from]))
		b.WriteString(Redaction)
		cursor = to
	}
	if cursor < len(orig) {
		b.WriteString(string(orig[cursor:]))
	}
	return b.String()
}

func stripRedactions(s string) string {
	return strings.ReplaceAll(s, Redaction, "")
}

// Kinds returns the finding kinds, deduplicated, for storage on the message row
// and for the fraud detectors to aggregate over.
func (s Scan) Kinds() []string {
	seen := map[FindingKind]bool{}
	// Empty and non-nil, which is the whole point of this line.
	//
	// `var out []string` is nil for a clean message, pgx encodes a nil slice as
	// SQL NULL, and `messages.findings` is NOT NULL with a `'{}'` default — so
	// every message that had *nothing* wrong with it failed to insert, while
	// every message carrying a phone number went through. Chat worked only for
	// the messages it was supposed to redact.
	out := []string{}
	for _, f := range s.Findings {
		if seen[f.Kind] {
			continue
		}
		seen[f.Kind] = true
		out = append(out, string(f.Kind))
	}
	return out
}
