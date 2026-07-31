package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/config"
	"github.com/marketplace/api/internal/modules/auctions"
	"github.com/marketplace/api/internal/modules/driver"
	"github.com/marketplace/api/internal/modules/notify"
)

// BuildNotify constructs the notification service and its providers.
//
// Providers are chosen per channel rather than one for everything, because they
// are separate businesses: SMS is an aggregator, WhatsApp is Meta's Business
// API, push is FCM/APNs, email is anything. A single `NOTIFY_PROVIDER` setting
// would make every one of those the same decision.
//
// A channel with nothing configured gets RefusingProvider, so its messages fail
// visibly with a reason instead of sitting pending forever. The silent-queue
// failure is the one that gets discovered by a customer.
func BuildNotify(cfg config.Config, pool *pgxpool.Pool) *notify.Service {
	providers := map[notify.Channel]notify.Provider{}

	// The log provider prints message bodies, including delivery codes. That is
	// what makes local development possible and exactly what must never run in
	// production, so it is gated on the same environment check that gates the
	// sandbox payment provider rather than on a separate flag someone could set
	// independently.
	if !cfg.IsProduction() {
		log := slog.Default().With("component", "notify")
		for _, ch := range []notify.Channel{
			notify.ChannelSMS, notify.ChannelWhatsApp, notify.ChannelPush,
			notify.ChannelEmail, notify.ChannelInApp,
		} {
			providers[ch] = notify.LogProvider{Logger: log}
		}
	}

	loc, err := time.LoadLocation(cfg.DefaultTimezone)
	if err != nil {
		// A missing tzdata is not a reason to refuse to boot, but it is a reason
		// to say so: quiet hours computed in the wrong zone send marketing at
		// 2am, which is the kind of bug that arrives as an app-store review.
		slog.Warn("timezone not loadable, falling back to UTC+4",
			"timezone", cfg.DefaultTimezone, "error", err)
		loc = time.FixedZone("GST", 4*3600)
	}

	return notify.NewService(pool, providers, loc)
}

// DeliveryCodeNotifier adapts notify.Service to driver.Notifier.
//
// The adapter exists because `driver` declares the interface it needs and
// `notify` does not know `driver` exists — the module rule, applied. It is
// four lines, and those four lines are what keeps the logistics module from
// acquiring an opinion about templates, locales and consent.
type DeliveryCodeNotifier struct{ Notify *notify.Service }

func (n DeliveryCodeNotifier) NotifyDeliveryCode(
	ctx context.Context, tx pgx.Tx, in driver.DeliveryCode,
) (uuid.UUID, error) {
	return n.Notify.EnqueueDirect(ctx, tx, notify.DirectMessage{
		Phone:       in.Phone,
		TemplateKey: "delivery.otp",
		Category:    notify.CategoryDelivery,
		Params:      map[string]string{"code": in.Code, "name": in.RecipientName},
		// Keyed by fulfillment *and attempt*, not by fulfillment alone.
		//
		// Fulfillment alone looks right and is wrong. A failed first attempt
		// sends the stop back for redelivery, which issues a fresh code — and a
		// dedupe key that ignored the attempt would suppress that message,
		// leaving the customer holding a code that no longer verifies. The
		// duplicate this is actually guarding against is a relay retrying one
		// enqueue, and those share an attempt number.
		DedupeKey: fmt.Sprintf("delivery-otp:%s:%d", in.FulfillmentID, in.Attempt),
	})
}

// AuctionNotifier adapts notify.Service to auctions.Notifier.
//
// The same four-line adapter as DeliveryCodeNotifier, for the same reason: the
// auction module declares the shape it needs and `notify` has never heard of
// auctions. What crosses the boundary is a user id, a template key and a map of
// strings — nothing about consent, locale or channel ladders, which are this
// side's business.
type AuctionNotifier struct{ Notify *notify.Service }

func (n AuctionNotifier) Enqueue(ctx context.Context, tx pgx.Tx, m auctions.NotifyMessage) error {
	userID, err := uuid.Parse(m.UserID)
	if err != nil {
		return fmt.Errorf("app: auction notify: %w", err)
	}
	_, err = n.Notify.Enqueue(ctx, tx, notify.Message{
		UserID:      userID.String(),
		TemplateKey: m.TemplateKey,
		// Transactional throughout, and that is a consent decision rather than
		// a convenience. Being outbid on a lot you are bidding on is not
		// marketing — it is the state of a transaction you initiated, and a
		// bidder who could switch it off would be switching off the auction.
		Category:  notify.CategoryTransactional,
		Params:    m.Params,
		DedupeKey: m.DedupeKey,
	})
	return err
}
