package notify

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Rendering is deliberately not text/template.
//
// text/template would give conditionals, loops and function calls to a string
// that is edited in a database by someone who is correcting Arabic copy rather
// than writing Go. Two consequences follow, and both are bad: a template can
// fail at render time on a message that must go out, and a template author has
// far more power than "put the code here" requires.
//
// So the syntax is one thing — {{name}} — and the only failure mode is a
// missing parameter, which is caught before anything is sent.

// ErrMissingParams is returned when a template references a parameter the
// caller did not supply.
type ErrMissingParams struct {
	TemplateKey string
	Missing     []string
}

func (e *ErrMissingParams) Error() string {
	return fmt.Sprintf("notify: template %q needs %s", e.TemplateKey, strings.Join(e.Missing, ", "))
}

// Render substitutes {{name}} placeholders.
//
// A missing parameter is an error rather than an empty string, and that choice
// is the whole point of this function. The alternative sends "Your delivery
// code is " to a customer standing at their door — a message that is worse than
// none, because it looks like the system worked.
func Render(key, body string, params map[string]string) (string, error) {
	var out strings.Builder
	var missing []string
	seen := map[string]bool{}

	for i := 0; i < len(body); {
		if !strings.HasPrefix(body[i:], "{{") {
			out.WriteByte(body[i])
			i++
			continue
		}

		end := strings.Index(body[i+2:], "}}")
		if end < 0 {
			// An unterminated placeholder is literal text. Copy is written by
			// people, and refusing to send a delivery OTP because a marketing
			// writer left a stray brace in an unrelated template would be a
			// spectacular own goal.
			out.WriteString(body[i:])
			break
		}

		name := strings.TrimSpace(body[i+2 : i+2+end])
		i += 2 + end + 2

		value, ok := params[name]
		if !ok {
			if !seen[name] {
				seen[name] = true
				missing = append(missing, name)
			}
			continue
		}
		out.WriteString(value)
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return "", &ErrMissingParams{TemplateKey: key, Missing: missing}
	}
	return out.String(), nil
}

// Placeholders lists the parameters a template body needs.
//
// Used by the console's template editor, so someone correcting Arabic copy is
// told which values the English version supplies rather than discovering the
// mismatch when a customer gets a broken message.
func Placeholders(body string) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(body); {
		if !strings.HasPrefix(body[i:], "{{") {
			i++
			continue
		}
		end := strings.Index(body[i+2:], "}}")
		if end < 0 {
			break
		}
		name := strings.TrimSpace(body[i+2 : i+2+end])
		i += 2 + end + 2
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// SMSSegments counts the billable segments a body will cost.
//
// The GSM-7 alphabet fits 160 characters in one segment and 153 in each segment
// of a concatenated message. Any character outside it — which includes every
// Arabic letter — forces the whole message into UCS-2, where the limits drop to
// 70 and 67.
//
// That is a factor of more than two on the cost of every Arabic SMS, and it is
// invisible until the bill arrives. Exposing it here lets the console show a
// segment count next to the Arabic body while it is being written, which is the
// only moment anyone can do anything about it.
func SMSSegments(body string) int {
	if body == "" {
		return 0
	}

	unicodeNeeded := false
	length := 0
	for _, r := range body {
		if !gsm7(r) {
			unicodeNeeded = true
		}
		// Extended GSM characters (^, {, }, [, ], ~, |, €) cost two septets.
		if strings.ContainsRune(gsmExtended, r) {
			length += 2
			continue
		}
		length++
	}

	single, multi := 160, 153
	if unicodeNeeded {
		single, multi = 70, 67
	}
	if length <= single {
		return 1
	}
	return (length + multi - 1) / multi
}

const gsmExtended = "^{}\\[~]|€"

func gsm7(r rune) bool {
	if r > unicode.MaxASCII {
		// A handful of non-ASCII runes are in GSM-7 (£, ¥, é, Greek capitals).
		// They are not worth enumerating: none appears in this product's copy,
		// and guessing wrong in this direction over-counts segments, which is
		// the harmless direction to be wrong in.
		return false
	}
	switch r {
	case '`', '@', '$':
		return true
	}
	return r >= 0x20 && r <= 0x7E || r == '\n' || r == '\r'
}
