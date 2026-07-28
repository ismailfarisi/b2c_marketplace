// Package notify sends messages to people.
//
// It exists because Phase 7 shipped a cash-on-delivery flow that issues a
// one-time password, hashes it, and has nowhere to send it. A driver could not
// complete a cash delivery, which meant cash-on-delivery — the payment method
// PLAN.md §6 calls "still meaningful in the region" — did not work at all.
// docs/README.md decision #8 is what that turned into, and this package closes
// it.
//
// The design constraint that shapes everything here: **the platform decides
// what may be sent, and the provider decides only whether it arrived.** Consent,
// channel eligibility, quiet hours and deduplication are all resolved before a
// provider is called, and every refusal is recorded as a suppressed row rather
// than as an absence. An audit that asks "did this customer receive the OTP" has
// to be answerable with yes, no-and-here-is-why, or it is not an audit.
package notify

import (
	"fmt"
	"time"
)

// Channel is how a message travels.
type Channel string

const (
	ChannelSMS      Channel = "sms"
	ChannelPush     Channel = "push"
	ChannelEmail    Channel = "email"
	ChannelWhatsApp Channel = "whatsapp"
	ChannelInApp    Channel = "in_app"
)

// Category is what the message is for, and it is a consent class rather than a
// description. The database enforces the same split: `notification_preferences`
// refuses a row for anything but marketing.
type Category string

const (
	// CategoryTransactional — order placed, payment taken, refund issued.
	CategoryTransactional Category = "transactional"
	// CategoryDelivery — driver on the way, delivery OTP, failed attempt.
	CategoryDelivery Category = "delivery"
	// CategoryAccount — OTP login, password-equivalent changes, security.
	CategoryAccount Category = "account"
	// CategoryModeration — content removed, strike issued, appeal decided.
	CategoryModeration Category = "moderation"
	// CategoryMarketing — everything a person is entitled to refuse.
	CategoryMarketing Category = "marketing"
)

// Optional reports whether a category may be switched off.
//
// UAE and KSA both require a working opt-out for marketing. Neither permits a
// customer to opt out of the one-time password for a delivery they are standing
// at the door for, and a product that let them would produce support calls that
// cannot be resolved.
func (c Category) Optional() bool { return c == CategoryMarketing }

// channelOrder is the fallback ladder per category, most preferred first.
//
// Not one global order, because the right channel differs by what is being
// said:
//
//   - A delivery OTP goes to SMS first. It has to reach a phone that may have
//     no app installed, no data connection in a basement car park, and may not
//     even belong to the person who placed the order — a building watchman
//     receiving a delivery is the normal case, not the edge case.
//   - Marketing never uses SMS. It is the most expensive channel per message
//     and the one with the strictest consent rules in both markets; a marketing
//     SMS is a regulatory risk bought at a premium.
//   - Moderation notices avoid WhatsApp. Telling someone their content was
//     removed inside a conversation window they opened for a delivery update
//     reads as harassment.
var channelOrder = map[Category][]Channel{
	CategoryDelivery:      {ChannelSMS, ChannelWhatsApp, ChannelPush},
	CategoryTransactional: {ChannelPush, ChannelWhatsApp, ChannelSMS, ChannelEmail},
	CategoryAccount:       {ChannelSMS, ChannelWhatsApp},
	CategoryModeration:    {ChannelPush, ChannelEmail, ChannelInApp},
	CategoryMarketing:     {ChannelPush, ChannelInApp},
}

// ChannelsFor returns the ladder for a category.
func ChannelsFor(c Category) []Channel {
	order, ok := channelOrder[c]
	if !ok {
		// An unrecognised category is a programming error, and the safe
		// direction is in-app only: it reaches the user, costs nothing, and
		// cannot be sent to a phone number that has refused it.
		return []Channel{ChannelInApp}
	}
	return order
}

// Preference is one stored opt-out.
type Preference struct {
	Category Category
	Channel  Channel
	Allowed  bool
}

// Suppression explains why a message was not sent. The empty value means it was
// not suppressed.
type Suppression string

const (
	SuppressedByPreference  Suppression = "preference"
	SuppressedByQuietHours  Suppression = "quiet_hours"
	SuppressedNoDestination Suppression = "no_destination"
	SuppressedNoTemplate    Suppression = "no_template"
)

// Allowed reports whether a category may go out on a channel for this user.
//
// The order of the two checks matters. Optionality is consulted first, so a
// stored preference row that should not exist — written by a migration, a bug,
// or a support tool — cannot switch off a delivery OTP. The database CHECK is
// the primary defence; this is the second, because the two failure modes are
// different (a constraint protects against inserts, this protects against rows
// that arrived before the constraint did).
func Allowed(cat Category, ch Channel, prefs []Preference) bool {
	if !cat.Optional() {
		return true
	}
	for _, p := range prefs {
		if p.Category == cat && p.Channel == ch {
			return p.Allowed
		}
	}
	// Default for marketing is on, with a visible switch. That is the standard
	// the region's e-commerce operates on; the requirement is that refusing
	// works, not that consent is collected up front.
	return true
}

// QuietHours is the window in which non-urgent messages are held.
//
// Local time, always. A 22:00 cut-off computed in UTC lands at 02:00 in Dubai,
// which is the precise mistake this exists to prevent.
const (
	QuietFromHour  = 22
	QuietUntilHour = 8
)

// InQuietHours reports whether a local time falls in the overnight window.
func InQuietHours(local time.Time) bool {
	h := local.Hour()
	return h >= QuietFromHour || h < QuietUntilHour
}

// Deferrable reports whether a category waits for morning.
//
// Only marketing does. A delivery OTP at 23:40 is a delivery happening at
// 23:40, and holding it until eight is not politeness — it is a failed
// delivery. Restaurants in this region take orders past midnight during
// Ramadan, and a quiet-hours rule that swallowed transactional messages would
// break the busiest month of the year.
func Deferrable(c Category) bool { return c == CategoryMarketing }

// WhatsApp Business enforces a 24-hour session window: inside it, free-form
// messages are allowed; outside it, only pre-approved templates.
const WhatsAppWindow = 24 * time.Hour

// WhatsAppSendable reports whether a WhatsApp message can go out now.
//
// `sessionExpires` is the zero time when there is no open session. A template
// name is what makes a send possible outside the window, which is why
// `notification_templates` refuses a WhatsApp row without one — a template-less
// WhatsApp message is deliverable for 24 hours after the customer last wrote to
// you and undeliverable the rest of the time, and that is not a channel, it is
// a coin toss.
func WhatsAppSendable(now, sessionExpires time.Time, hasApprovedTemplate bool) bool {
	if now.Before(sessionExpires) {
		return true
	}
	return hasApprovedTemplate
}

// PickLocale chooses the language a message is written in.
//
// Three inputs, in order: the notification locale the user set explicitly, the
// locale they use the app in, and English.
//
// The first exists because these genuinely differ. A resident who reads the app
// in English may want SMS in Arabic because that is what their household reads,
// and the person receiving a delivery OTP is frequently not the person who
// placed the order.
func PickLocale(notificationLocale, uiLocale string) string {
	for _, l := range []string{notificationLocale, uiLocale} {
		if l == "en" || l == "ar" {
			return l
		}
	}
	return "en"
}

// Message is what a caller asks for. It names a template and supplies its
// parameters; it does not choose a channel, a locale or a body.
type Message struct {
	UserID      string
	TemplateKey string
	Category    Category
	Params      map[string]string
	// DedupeKey makes the send idempotent. The dispatcher is at-least-once —
	// an outbox relay always is — and a customer who receives the same OTP
	// three times has been given three reasons to distrust the product.
	DedupeKey string
	// ForceChannel overrides the ladder. Used by exactly one caller: the login
	// OTP, which must be SMS because the user has no session and therefore no
	// push token yet.
	ForceChannel Channel
}

// Validate catches the mistakes that would otherwise be discovered by a
// customer.
func (m Message) Validate() error {
	if m.TemplateKey == "" {
		return fmt.Errorf("notify: template key is required")
	}
	if m.Category == "" {
		return fmt.Errorf("notify: category is required")
	}
	if _, ok := channelOrder[m.Category]; !ok {
		return fmt.Errorf("notify: unknown category %q", m.Category)
	}
	return nil
}
