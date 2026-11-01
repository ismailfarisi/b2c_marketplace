package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// Delivery is one rendered message handed to a provider.
type Delivery struct {
	NotificationID string
	Channel        Channel
	Destination    string
	Locale         string
	Subject        string
	Body           string
	// ProviderTemplateName is set for WhatsApp sends outside a session window,
	// where only a pre-approved template may go out.
	ProviderTemplateName string
	Params               map[string]string
	// ParamValues are Params in the provider template's positional order, when
	// the template declares one (notification_templates.provider_params).
	ParamValues []string
}

// Provider is a transport. One per channel family; the service picks by
// channel.
//
// Name() rather than a type switch, because it lands in the notification row
// and "which provider sent this" is the first question when messages stop
// arriving in one country and keep arriving in another.
type Provider interface {
	Name() string
	Send(ctx context.Context, d Delivery) (ref string, err error)
}

// ErrPermanent marks a failure that must not be retried.
//
// The distinction is the whole reason this error exists. A gateway timeout is
// worth retrying; "this number is not a mobile" is not, and retrying it burns
// the retry budget of every message behind it in the queue while changing
// nothing.
var ErrPermanent = errors.New("notify: permanent failure")

// LogProvider writes messages to the log instead of sending them.
//
// The development default, and it prints the body. That is deliberate and it is
// also why config.Load refuses to boot production with it: printing a delivery
// OTP to stdout is exactly what makes local development possible and exactly
// what must never happen in production.
type LogProvider struct{ Logger *slog.Logger }

func (LogProvider) Name() string { return "log" }

func (p LogProvider) Send(ctx context.Context, d Delivery) (string, error) {
	log := p.Logger
	if log == nil {
		log = slog.Default()
	}
	log.InfoContext(ctx, "notification (not actually sent)",
		"channel", d.Channel,
		"destination", d.Destination,
		"locale", d.Locale,
		"body", d.Body,
	)
	return "log:" + d.NotificationID, nil
}

// RefusingProvider fails every send with a permanent error.
//
// Wired for any channel with no real provider configured, so the outcome is a
// failed row with a reason rather than a message that appears to have been sent.
// A queue that silently accepts messages for a channel nobody implemented is
// how "we sent it, check your phone" gets said to a customer for a week.
type RefusingProvider struct{ Channel Channel }

func (p RefusingProvider) Name() string { return "none" }

func (p RefusingProvider) Send(context.Context, Delivery) (string, error) {
	return "", fmt.Errorf("%w: no provider configured for %s", ErrPermanent, p.Channel)
}
