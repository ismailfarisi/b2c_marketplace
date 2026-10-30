package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

type Service struct {
	pool      *pgxpool.Pool
	providers map[Channel]Provider
	// loc is the market's timezone, used for quiet hours. One zone rather than
	// per-user, because the alternative is storing a timezone on every user and
	// defaulting it wrong for everyone who never set it. Asia/Dubai and
	// Asia/Riyadh are both UTC+3 with no DST, so a single zone is correct for
	// both launch markets and the assumption is written here rather than
	// discovered later.
	loc *time.Location
}

func NewService(pool *pgxpool.Pool, providers map[Channel]Provider, loc *time.Location) *Service {
	if loc == nil {
		loc = time.FixedZone("GST", 4*3600)
	}
	if providers == nil {
		providers = map[Channel]Provider{}
	}
	return &Service{pool: pool, providers: providers, loc: loc}
}

// recipient is everything about a person the send decision needs, in one query.
type recipient struct {
	userID     uuid.UUID
	phone      string
	email      string
	uiLocale   string
	notifLocal string
	prefs      []Preference
	waExpires  time.Time
}

// Enqueue writes a pending notification inside the caller's transaction.
//
// Transactional on purpose, and it is the same argument the outbox makes: the
// decision to notify is part of the state change that caused it. A delivery OTP
// enqueued outside the transaction that issued it can exist without the OTP, or
// the OTP without the message — and both of those are a driver at a door with
// no way forward.
//
// What this does *not* do is send. Sending is a network call and it belongs
// nowhere near a transaction holding row locks on `fulfillments`.
func (s *Service) Enqueue(ctx context.Context, tx pgx.Tx, m Message) (uuid.UUID, error) {
	if err := m.Validate(); err != nil {
		return uuid.Nil, err
	}
	userID, err := uuid.Parse(m.UserID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("notify: user id: %w", err)
	}

	r, err := s.loadRecipient(ctx, tx, userID)
	if err != nil {
		return uuid.Nil, err
	}

	locale := PickLocale(r.notifLocal, r.uiLocale)
	now := time.Now()

	channel, tmpl, suppression := s.choose(ctx, tx, m, r, locale, now)
	if suppression != "" {
		return s.insert(ctx, tx, notificationRow{
			userID: userID, destination: destinationFor(channel, r),
			templateKey: m.TemplateKey, channel: channel, locale: locale,
			category: m.Category, body: "", status: "suppressed",
			suppressed: string(suppression), dedupe: m.DedupeKey,
		})
	}

	body, err := Render(m.TemplateKey, tmpl.body, m.Params)
	if err != nil {
		// A template whose parameters the caller did not supply is a bug in the
		// caller, and it surfaces here rather than as a broken message. The
		// enqueue fails, which fails the transaction that wanted to notify —
		// correct, because the caller asked for something impossible.
		return uuid.Nil, err
	}

	return s.insert(ctx, tx, notificationRow{
		userID: userID, destination: destinationFor(channel, r),
		templateID: &tmpl.id, templateKey: m.TemplateKey, channel: channel,
		locale: tmpl.locale, category: m.Category, body: body,
		status: "pending", dedupe: m.DedupeKey, params: m.Params,
	})
}

// DirectMessage addresses a phone number rather than an account.
//
// One caller needs this and it is the one that made the whole package
// necessary: the delivery OTP goes to `fulfillments.recipient_phone`, and the
// person at the door is frequently not the person who placed the order — a
// building watchman, a colleague, a family member. Requiring an account would
// mean cash delivery only works when the buyer answers their own door.
type DirectMessage struct {
	Phone       string
	TemplateKey string
	Category    Category
	Params      map[string]string
	DedupeKey   string
}

// EnqueueDirect sends to a phone number, in that number's language if it is
// known.
//
// The locale is resolved by matching the number against `users.phone`, which
// works because both are stored E.164-normalised. A registered Arabic-speaking
// customer receiving their own delivery gets Arabic; an unknown number gets
// English. That is a real limitation rather than a rounding error — it means
// the watchman scenario is served in English — and the fix is a locale on the
// fulfillment, which is a change to the ordering flow rather than to this
// package.
//
// No preference check, because the only categories permitted here are ones that
// cannot be refused. A marketing message to an unaccounted phone number is
// exactly the thing consent law exists to prevent, so it is refused in code.
func (s *Service) EnqueueDirect(ctx context.Context, tx pgx.Tx, m DirectMessage) (uuid.UUID, error) {
	if m.Category.Optional() {
		return uuid.Nil, fmt.Errorf("notify: %s cannot be sent to a bare phone number", m.Category)
	}
	if m.Phone == "" {
		return uuid.Nil, fmt.Errorf("notify: direct message needs a phone number")
	}

	var (
		userID uuid.NullUUID
		locale string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, coalesce(notification_locale::text, locale::text)
		  FROM users WHERE phone = $1 AND deleted_at IS NULL LIMIT 1`, m.Phone).
		Scan(&userID, &locale)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("notify: resolve phone: %w", err)
	}
	if locale == "" {
		locale = "en"
	}

	tmpl, err := s.loadTemplate(ctx, tx, m.TemplateKey, ChannelSMS, locale)
	if err != nil {
		return s.insert(ctx, tx, notificationRow{
			destination: m.Phone, templateKey: m.TemplateKey, channel: ChannelSMS,
			locale: locale, category: m.Category, status: "suppressed",
			suppressed: string(SuppressedNoTemplate), dedupe: m.DedupeKey,
		})
	}

	body, err := Render(m.TemplateKey, tmpl.body, m.Params)
	if err != nil {
		return uuid.Nil, err
	}

	row := notificationRow{
		destination: m.Phone, templateID: &tmpl.id, templateKey: m.TemplateKey,
		channel: ChannelSMS, locale: tmpl.locale, category: m.Category,
		body: body, status: "pending", dedupe: m.DedupeKey, params: m.Params,
	}
	if userID.Valid {
		row.userID = userID.UUID
	}
	return s.insert(ctx, tx, row)
}

// choose resolves the channel and template, or explains the refusal.
//
// Walks the category's ladder and takes the first channel that has a
// destination, a template, and the user's consent. Returning the *last*
// attempted channel alongside the suppression reason is deliberate: a
// suppressed row that names no channel cannot be debugged.
func (s *Service) choose(
	ctx context.Context, tx pgx.Tx, m Message, r recipient, locale string, now time.Time,
) (Channel, template, Suppression) {
	ladder := ChannelsFor(m.Category)
	if m.ForceChannel != "" {
		ladder = []Channel{m.ForceChannel}
	}

	if Deferrable(m.Category) && InQuietHours(now.In(s.loc)) {
		return ladder[0], template{}, SuppressedByQuietHours
	}

	var last Channel = ladder[0]
	var reason Suppression = SuppressedNoTemplate

	for _, ch := range ladder {
		last = ch

		if destinationFor(ch, r) == "" {
			reason = SuppressedNoDestination
			continue
		}
		if !Allowed(m.Category, ch, r.prefs) {
			reason = SuppressedByPreference
			continue
		}

		tmpl, err := s.loadTemplate(ctx, tx, m.TemplateKey, ch, locale)
		if err != nil {
			reason = SuppressedNoTemplate
			continue
		}
		if ch == ChannelWhatsApp &&
			!WhatsAppSendable(now, r.waExpires, tmpl.providerTemplate != "") {
			reason = SuppressedNoTemplate
			continue
		}
		return ch, tmpl, ""
	}

	return last, template{}, reason
}

func destinationFor(ch Channel, r recipient) string {
	switch ch {
	case ChannelSMS, ChannelWhatsApp:
		return r.phone
	case ChannelEmail:
		return r.email
	case ChannelPush, ChannelInApp:
		// In-app and push both address the account rather than an endpoint. The
		// push token lives on the session and is resolved by the provider at
		// send time, because a user with four devices has four of them and
		// picking one here would be arbitrary.
		return r.userID.String()
	}
	return ""
}

type template struct {
	id               uuid.UUID
	locale           string
	body             string
	subject          string
	providerTemplate string
}

// loadTemplate resolves a template, falling back to English.
//
// The fallback is a real trade rather than a default. Sending English to an
// Arabic reader is bad; sending nothing is worse, and the case where it happens
// is a template that was added in English and not yet translated — which is a
// content gap, not a reason to withhold a delivery code. The row records the
// locale actually used, so the gap is countable.
func (s *Service) loadTemplate(ctx context.Context, tx pgx.Tx, key string, ch Channel, locale string) (template, error) {
	var t template
	// Every use of $3 is cast explicitly, and that is not decoration.
	//
	// Without the casts Postgres infers $3 as `locale_code` from its first use
	// inside the ARRAY constructor, and the ORDER BY then reads as
	// `text = locale_code` — an operator that does not exist. The statement
	// fails, the surrounding transaction aborts, and the *next* statement
	// reports "current transaction is aborted", which points at the wrong line
	// entirely.
	//
	// The effect was that no delivery OTP could ever be sent: `sendCode` runs
	// inside the dispatch transaction, so the failure took the whole dispatch
	// with it and cash-on-delivery was unusable — the exact thing Phase 8 was
	// built to fix. Found in QA against a real database; no Go test touches
	// Postgres, so nothing in the suite could have caught it.
	err := tx.QueryRow(ctx, `
		SELECT id, locale::text, body, coalesce(subject, ''), coalesce(provider_template_name, '')
		  FROM notification_templates
		 WHERE key = $1 AND channel = $2 AND is_active
		   AND locale = ANY (ARRAY[$3::locale_code, 'en'::locale_code])
		 ORDER BY (locale = $3::locale_code) DESC
		 LIMIT 1`, key, string(ch), locale).
		Scan(&t.id, &t.locale, &t.body, &t.subject, &t.providerTemplate)
	if err != nil {
		return template{}, err
	}
	return t, nil
}

func (s *Service) loadRecipient(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (recipient, error) {
	r := recipient{userID: userID}
	err := tx.QueryRow(ctx, `
		SELECT u.phone, coalesce(u.email::text, ''), u.locale::text,
		       coalesce(u.notification_locale::text, ''),
		       coalesce((SELECT max(w.expires_at) FROM whatsapp_sessions w
		                  WHERE w.user_id = u.id), 'epoch'::timestamptz)
		  FROM users u WHERE u.id = $1 AND u.deleted_at IS NULL`, userID).
		Scan(&r.phone, &r.email, &r.uiLocale, &r.notifLocal, &r.waExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, httpx.ErrNotFound
	}
	if err != nil {
		return r, fmt.Errorf("notify: load recipient: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT category, channel, allowed FROM notification_preferences WHERE user_id = $1`, userID)
	if err != nil {
		return r, fmt.Errorf("notify: load preferences: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p Preference
		if err := rows.Scan(&p.Category, &p.Channel, &p.Allowed); err != nil {
			return r, err
		}
		r.prefs = append(r.prefs, p)
	}
	return r, rows.Err()
}

type notificationRow struct {
	userID      uuid.UUID
	destination string
	templateID  *uuid.UUID
	templateKey string
	channel     Channel
	locale      string
	category    Category
	body        string
	status      string
	suppressed  string
	dedupe      string
	// params are kept for providers that send structured templates (WhatsApp
	// Cloud API takes the values, not the rendered text).
	params map[string]string
}

func (s *Service) insert(ctx context.Context, tx pgx.Tx, n notificationRow) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO notifications
		    (user_id, destination, template_id, template_key, channel, locale,
		     category, rendered_body, status, suppressed_reason, dedupe_key, params)
		VALUES ($1, $2, $3, $4, $5, $6::locale_code, $7, $8, $9,
		        nullif($10, ''), nullif($11, ''), $12)
		ON CONFLICT DO NOTHING
		RETURNING id`,
		n.userID, n.destination, n.templateID, n.templateKey, string(n.channel), n.locale,
		string(n.category), n.body, n.status, n.suppressed, n.dedupe, paramsJSON(n.params)).Scan(&id)

	if errors.Is(err, pgx.ErrNoRows) {
		// The dedupe key already exists. Not an error — it is the mechanism
		// working. Return the id of the message that did go out so the caller
		// can still link to it.
		if n.dedupe == "" {
			return uuid.Nil, fmt.Errorf("notify: insert returned no row")
		}
		var existing uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT id FROM notifications WHERE dedupe_key = $1 ORDER BY created_at DESC LIMIT 1`,
			n.dedupe).Scan(&existing); err != nil {
			return uuid.Nil, fmt.Errorf("notify: resolve duplicate: %w", err)
		}
		return existing, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("notify: insert: %w", err)
	}
	return id, nil
}

// --- dispatch ---------------------------------------------------------------

// MaxAttempts bounds retries per message.
//
// Three, and then the row stays failed. A message that has failed three times
// is not going to succeed on the fourth, and an unbounded retry on a delivery
// OTP means the customer receives it an hour after the driver left.
const MaxAttempts = 3

// Dispatch sends up to `limit` pending notifications.
//
// Called on a tick by the worker. Each message is claimed with SKIP LOCKED so
// two worker replicas do not send the same message twice — the same pattern the
// media pipeline uses, and for a sharper reason here: a duplicate transcode
// costs money, a duplicate OTP costs trust.
func (s *Service) Dispatch(ctx context.Context, limit int) (sent, failed int, err error) {
	for i := 0; i < limit; i++ {
		ok, sendErr := s.dispatchOne(ctx)
		if !ok {
			break
		}
		if sendErr != nil {
			failed++
			continue
		}
		sent++
	}
	return sent, failed, nil
}

func (s *Service) dispatchOne(ctx context.Context) (claimed bool, sendErr error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var (
		id           uuid.UUID
		createdAt    time.Time
		channel      Channel
		destination  string
		locale       string
		body         string
		subject      string
		providerTmpl string
		attempts     int16
		params       map[string]string
		paramOrder   []string
	)
	err = tx.QueryRow(ctx, `
		SELECT n.id, n.created_at, n.channel, n.destination, n.locale::text,
		       n.rendered_body, coalesce(t.subject, ''), coalesce(t.provider_template_name, ''),
		       n.attempts, coalesce(n.params, '{}'::jsonb), coalesce(t.provider_params, '{}')
		  FROM notifications n
		  LEFT JOIN notification_templates t ON t.id = n.template_id
		 WHERE n.status = 'pending' AND n.attempts < $1
		 ORDER BY n.created_at
		 FOR UPDATE OF n SKIP LOCKED
		 LIMIT 1`, MaxAttempts).
		Scan(&id, &createdAt, &channel, &destination, &locale, &body, &subject, &providerTmpl, &attempts,
			&params, &paramOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	provider, ok := s.providers[channel]
	if !ok {
		provider = RefusingProvider{Channel: channel}
	}

	ref, sendErr := provider.Send(ctx, Delivery{
		NotificationID:       id.String(),
		Channel:              channel,
		Destination:          destination,
		Locale:               locale,
		Subject:              subject,
		Body:                 body,
		ProviderTemplateName: providerTmpl,
		Params:               params,
		ParamValues:          orderedParams(paramOrder, params),
	})

	status := "sent"
	var failureCode string
	switch {
	case sendErr == nil:
	case errors.Is(sendErr, ErrPermanent):
		// Jump straight to the attempt ceiling. Retrying a permanent failure
		// three times delays every message behind it and changes nothing.
		status, failureCode, attempts = "failed", sendErr.Error(), MaxAttempts
	default:
		status, failureCode = "pending", sendErr.Error()
		if attempts+1 >= MaxAttempts {
			status = "failed"
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE notifications
		   SET status = $1, provider = $2, provider_ref = nullif($3, ''),
		       failure_code = nullif($4, ''), attempts = $5,
		       sent_at = CASE WHEN $1 = 'sent' THEN now() ELSE sent_at END
		 WHERE id = $6 AND created_at = $7`,
		status, provider.Name(), ref, failureCode, attempts+1, id, createdAt); err != nil {
		return true, err
	}

	// Commit, which the first version of this function did not do.
	//
	// The deferred Rollback above then undid the UPDATE on every pass, so the
	// row stayed `pending` with `attempts` at zero and the loop re-sent it on
	// the next tick — for ever, because the attempt ceiling was never reached.
	// QA caught it as one delivery code sent a hundred times in ten seconds.
	//
	// The consequences are the two worst available: a customer receiving the
	// same SMS every ten seconds, and a per-message bill that grows without
	// bound. Nothing in the Go suite could see it — the tests never open a
	// transaction — and the log line said "sent", truthfully, a hundred times.
	if err := tx.Commit(ctx); err != nil {
		return true, fmt.Errorf("notify: commit dispatch: %w", err)
	}

	return true, sendErr
}

// --- preferences ------------------------------------------------------------

// SetPreference records an opt-in or opt-out.
//
// Refuses non-optional categories in Go as well as in SQL. The duplication is
// intentional: the constraint gives a 23514 that reaches the client as a 500,
// and a customer trying to turn off delivery notifications deserves an
// explanation rather than an error page.
func (s *Service) SetPreference(ctx context.Context, userID uuid.UUID, cat Category, ch Channel, allowed bool) error {
	if !cat.Optional() {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "category", Code: "not_optional",
			Message: "delivery, account and transactional messages cannot be switched off",
		}}}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO notification_preferences (user_id, category, channel, allowed, source)
		VALUES ($1, $2, $3, $4, 'user')
		ON CONFLICT (user_id, category, channel)
		DO UPDATE SET allowed = EXCLUDED.allowed, changed_at = now()`,
		userID, string(cat), string(ch), allowed)
	if err != nil {
		return fmt.Errorf("notify: set preference: %w", err)
	}
	return nil
}

// Inbox lists a user's in-app notifications.
func (s *Service) Inbox(ctx context.Context, userID uuid.UUID, limit int) ([]Item, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, created_at, template_key, category, rendered_body, read_at
		  FROM notifications
		 WHERE user_id = $1 AND channel IN ('in_app', 'push') AND status <> 'suppressed'
		 ORDER BY created_at DESC
		 LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("notify: inbox: %w", err)
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		var it Item
		var readAt *time.Time
		if err := rows.Scan(&it.ID, &it.CreatedAt, &it.TemplateKey, &it.Category, &it.Body, &readAt); err != nil {
			return nil, err
		}
		it.Read = readAt != nil
		out = append(out, it)
	}
	return out, rows.Err()
}

type Item struct {
	ID          uuid.UUID
	CreatedAt   time.Time
	TemplateKey string
	Category    string
	Body        string
	Read        bool
}

// MarkRead is scoped by user id in the WHERE clause rather than checked after
// loading. One statement, no window in which a caller can mark someone else's
// notification read.
func (s *Service) MarkRead(ctx context.Context, userID, notificationID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notifications SET read_at = coalesce(read_at, now())
		 WHERE id = $1 AND user_id = $2`, notificationID, userID)
	if err != nil {
		return fmt.Errorf("notify: mark read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

// EnsurePartitions creates notification partitions ahead of time.
//
// Same reasoning as the impression and ping tables: a row that lands in the
// DEFAULT partition blocks the later creation of any partition covering its
// range, so a month that slips through cannot be given its own partition
// without moving rows out first.
func (s *Service) EnsurePartitions(ctx context.Context, monthsAhead int) error {
	now := time.Now().UTC()
	for i := 0; i <= monthsAhead; i++ {
		month := now.AddDate(0, i, 0)
		if _, err := s.pool.Exec(ctx,
			`SELECT create_notification_partition($1::date)`,
			month.Format("2006-01-02")); err != nil {
			return fmt.Errorf("notify: ensure partition: %w", err)
		}
	}
	return nil
}

// paramsJSON stores nil as SQL NULL rather than an empty object.
func paramsJSON(p map[string]string) any {
	if len(p) == 0 {
		return nil
	}
	return p
}

// orderedParams lays params out in a provider template's positional order
// ({{1}}, {{2}}, ...). A missing value becomes "" so the count still matches.
func orderedParams(order []string, params map[string]string) []string {
	if len(order) == 0 {
		return nil
	}
	out := make([]string, len(order))
	for i, k := range order {
		out[i] = params[k]
	}
	return out
}
