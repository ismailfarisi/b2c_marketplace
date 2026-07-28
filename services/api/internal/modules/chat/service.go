package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Signals is the fraud module's write side, as chat needs it. Same inversion
// as in `vehicles`: this package records evidence without knowing how it is
// scored.
type Signals interface {
	Raise(ctx context.Context, tx pgx.Tx, subjectType string, subjectID uuid.UUID,
		kind string, severity int, detail map[string]any) error
}

const (
	// MaxBodyRunes is generous. People paste service histories in here.
	MaxBodyRunes = 4000

	// BurstWindow and BurstLimit bound how many people one account can contact
	// at once.
	//
	// This is not spam protection, it is the advance-fee scam's own shape: the
	// scammer's economics require volume, because the message only works on a
	// small fraction of recipients. A genuine buyer messages three sellers in an
	// evening; nobody messages forty.
	BurstWindow = time.Hour
	BurstLimit  = 25
)

type Service struct {
	pool    *pgxpool.Pool
	signals Signals
}

func NewService(pool *pgxpool.Pool, signals Signals) *Service {
	return &Service{pool: pool, signals: signals}
}

// Open returns the thread for this listing and buyer, creating it if needed.
//
// It satisfies vehicles.Threads. Idempotent by (listing, buyer) at the database
// level rather than by a read-then-write here: two taps on "message seller"
// arrive concurrently often enough that a check-then-insert would produce a
// duplicate-key error on a completely ordinary user action.
func (s *Service) Open(ctx context.Context, tx pgx.Tx, listingID, buyerID, sellerID uuid.UUID) (uuid.UUID, error) {
	if buyerID == sellerID {
		return uuid.Nil, fmt.Errorf("%w: a seller cannot open a thread with themselves", httpx.ErrConflict)
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO conversations (listing_id, buyer_id, seller_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (listing_id, buyer_id) DO UPDATE
		   -- A no-op update rather than DO NOTHING, because DO NOTHING returns
		   -- no row and the caller needs the id either way.
		   SET state = conversations.state
		RETURNING id`,
		listingID, buyerID, sellerID,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("open conversation: %w", err)
	}
	return id, nil
}

type Message struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	SenderID       uuid.UUID
	Body           string
	State          string
	Findings       []string
	CreatedAt      time.Time
}

// Send scans, stores and delivers.
//
// The scan runs *before* the insert and its verdict decides the row's state, so
// a held message is never briefly visible. Doing it the other way — insert,
// then scan, then update — leaves a window in which a websocket push or a
// polling client has already delivered a phone number, and the window is
// exactly as long as the scan takes.
func (s *Service) Send(ctx context.Context, conversationID, senderID uuid.UUID, body string) (*Message, error) {
	if len([]rune(body)) == 0 {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "body", Code: "required",
		}}}
	}
	if len([]rune(body)) > MaxBodyRunes {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "body", Code: "too_long",
			Message: fmt.Sprintf("maximum %d characters", MaxBodyRunes),
		}}}
	}

	conv, err := s.load(ctx, conversationID, senderID)
	if err != nil {
		return nil, err
	}
	if conv.State == "blocked" || conv.State == "closed" {
		return nil, fmt.Errorf("%w: this conversation is %s", httpx.ErrConflict, conv.State)
	}

	scan := Analyse(body)

	var out Message
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		bursting, count, err := s.burst(ctx, tx, senderID)
		if err != nil {
			return err
		}
		state, delivered := stateFor(scan)
		if bursting {
			// Volume is itself the finding. Holding rather than blocking,
			// because a dealer legitimately answering thirty buyers after a
			// weekend is the same shape and must not be silenced.
			state = "held"
			if s.signals != nil {
				if err := s.signals.Raise(ctx, tx, "user", senderID, "message_burst", 60,
					map[string]any{"window": BurstWindow.String(), "count": count}); err != nil {
					return err
				}
			}
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO messages (conversation_id, sender_id, body, delivered_body, state, findings, review_reason)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''))
			RETURNING id, created_at`,
			conversationID, senderID, body, delivered, state, scan.Kinds(), reviewReason(scan, bursting),
		).Scan(&out.ID, &out.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert message: %w", err)
		}

		// A held message does not advance the thread. Bumping last_message_at
		// or the unread badge would tell the recipient something arrived and
		// then show them nothing, which reads as a bug and produces a support
		// ticket for every held scam.
		if state != "held" && state != "blocked" {
			recipientCol := "seller_unread"
			if senderID == conv.SellerID {
				recipientCol = "buyer_unread"
			}
			if _, err := tx.Exec(ctx, fmt.Sprintf(`
				UPDATE conversations
				   SET last_message_at = now(), %s = %s + 1
				 WHERE id = $1`, recipientCol, recipientCol), conversationID); err != nil {
				return fmt.Errorf("bump conversation: %w", err)
			}
		}

		if s.signals != nil && len(scan.Findings) > 0 {
			for _, kind := range scan.Kinds() {
				if err := s.signals.Raise(ctx, tx, "conversation", conversationID,
					"chat_"+kind, severityFor(FindingKind(kind)),
					map[string]any{"sender_id": senderID.String()}); err != nil {
					return err
				}
			}
		}

		out.ConversationID = conversationID
		out.SenderID = senderID
		out.Body = delivered
		out.State = state
		out.Findings = scan.Kinds()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func stateFor(s Scan) (state, delivered string) {
	switch s.Verdict {
	case VerdictHold:
		// delivered_body stays NULL-equivalent: nothing has been decided yet,
		// and writing a redacted version now would imply a decision that a
		// moderator has not made.
		return "held", ""
	case VerdictRedact:
		return "redacted", s.Delivered
	default:
		return "sent", s.Delivered
	}
}

func reviewReason(s Scan, bursting bool) string {
	if bursting {
		return "message volume"
	}
	if s.Verdict == VerdictHold {
		for _, f := range s.Findings {
			if f.Kind == FindingAdvanceFee {
				return "advance-fee language: " + f.Match
			}
		}
		return "message consists only of contact details"
	}
	return ""
}

// severityFor weights the finding kinds against each other.
//
// Advance-fee language is not here: it holds the message outright, so it never
// reaches this function. What is here is the difference between "shared a phone
// number" — common, mostly innocent — and "pushed the conversation off the
// platform", which is the deliberate act.
func severityFor(k FindingKind) int {
	switch k {
	case FindingOffPlatform:
		return 55
	case FindingHandle:
		return 45
	case FindingPhone, FindingEmail:
		return 30
	case FindingLink:
		return 35
	}
	return 25
}

func (s *Service) burst(ctx context.Context, tx pgx.Tx, senderID uuid.UUID) (bool, int, error) {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(DISTINCT conversation_id)
		  FROM messages
		 WHERE sender_id = $1 AND created_at > now() - $2::interval`,
		senderID, BurstWindow.String(),
	).Scan(&count); err != nil {
		return false, 0, fmt.Errorf("burst check: %w", err)
	}
	// Distinct conversations, not messages. Forty messages in one thread is an
	// argument; forty threads is a campaign.
	return count >= BurstLimit, count, nil
}

type conversation struct {
	ID       uuid.UUID
	BuyerID  uuid.UUID
	SellerID uuid.UUID
	State    string
}

func (s *Service) load(ctx context.Context, id, userID uuid.UUID) (*conversation, error) {
	var c conversation
	err := s.pool.QueryRow(ctx, `
		SELECT id, buyer_id, seller_id, state
		  FROM conversations
		 WHERE id = $1 AND (buyer_id = $2 OR seller_id = $2)`,
		id, userID,
	).Scan(&c.ID, &c.BuyerID, &c.SellerID, &c.State)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not found rather than forbidden. A 403 confirms the conversation
		// exists, which is enough to enumerate who is talking to whom.
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load conversation: %w", err)
	}
	return &c, nil
}

// History returns a page of a thread.
//
// The sender always sees their own held message; the recipient never does. A
// sender whose message vanished without trace re-sends it four more ways, and
// each attempt is another row for a moderator to read.
func (s *Service) History(ctx context.Context, conversationID, userID uuid.UUID, limit int) ([]Message, error) {
	if _, err := s.load(ctx, conversationID, userID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, sender_id,
		       CASE WHEN m.sender_id = $2 THEN m.body
		            ELSE COALESCE(m.delivered_body, '') END,
		       m.state, m.findings, m.created_at
		  FROM messages m
		 WHERE m.conversation_id = $1
		   AND (m.state IN ('sent', 'redacted') OR m.sender_id = $2)
		   AND m.state <> 'deleted'
		 ORDER BY m.created_at DESC
		 LIMIT $3`,
		conversationID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.Body, &m.State, &m.Findings, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		m.ConversationID = conversationID
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) MarkRead(ctx context.Context, conversationID, userID uuid.UUID) error {
	c, err := s.load(ctx, conversationID, userID)
	if err != nil {
		return err
	}
	col := "buyer_unread"
	if userID == c.SellerID {
		col = "seller_unread"
	}
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE conversations SET %s = 0 WHERE id = $1`, col), conversationID); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE messages SET read_at = now()
		 WHERE conversation_id = $1 AND sender_id <> $2 AND read_at IS NULL
		   AND state IN ('sent', 'redacted')`,
		conversationID, userID); err != nil {
		return fmt.Errorf("mark messages read: %w", err)
	}
	return nil
}

type InboxEntry struct {
	ConversationID uuid.UUID
	ListingID      uuid.UUID
	ListingTitle   string
	CounterpartyID uuid.UUID
	Unread         int
	LastMessageAt  *time.Time
	State          string
}

func (s *Service) Inbox(ctx context.Context, userID uuid.UUID, limit int) ([]InboxEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.listing_id, COALESCE(l.title, l.reference_no),
		       CASE WHEN c.buyer_id = $1 THEN c.seller_id ELSE c.buyer_id END,
		       CASE WHEN c.buyer_id = $1 THEN c.buyer_unread ELSE c.seller_unread END,
		       c.last_message_at, c.state
		  FROM conversations c
		  JOIN vehicle_listings l ON l.id = c.listing_id
		 WHERE (c.buyer_id = $1 OR c.seller_id = $1) AND c.state <> 'blocked'
		 ORDER BY c.last_message_at DESC NULLS LAST
		 LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, fmt.Errorf("load inbox: %w", err)
	}
	defer rows.Close()

	var out []InboxEntry
	for rows.Next() {
		var e InboxEntry
		if err := rows.Scan(&e.ConversationID, &e.ListingID, &e.ListingTitle,
			&e.CounterpartyID, &e.Unread, &e.LastMessageAt, &e.State); err != nil {
			return nil, fmt.Errorf("scan inbox: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Moderation
// ─────────────────────────────────────────────────────────────────────────────

type HeldMessage struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	ListingID      uuid.UUID
	SenderID       uuid.UUID
	Body           string
	Findings       []string
	Reason         string
	CreatedAt      time.Time
}

func (s *Service) Queue(ctx context.Context, limit int) ([]HeldMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.conversation_id, c.listing_id, m.sender_id, m.body,
		       m.findings, COALESCE(m.review_reason, ''), m.created_at
		  FROM messages m
		  JOIN conversations c ON c.id = m.conversation_id
		 WHERE m.state = 'held'
		 ORDER BY m.created_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("load held messages: %w", err)
	}
	defer rows.Close()

	var out []HeldMessage
	for rows.Next() {
		var m HeldMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.ListingID, &m.SenderID,
			&m.Body, &m.Findings, &m.Reason, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan held message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Decide releases or blocks a held message.
//
// Releasing delivers the *redacted* body, not the original. A moderator judging
// that a message is not a scam has not decided that the phone number in it
// should be published — those are two decisions and only one was asked for.
func (s *Service) Decide(ctx context.Context, messageID uuid.UUID, release bool) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var body, convID string
		if err := tx.QueryRow(ctx,
			`SELECT body, conversation_id::text FROM messages
			  WHERE id = $1 AND state = 'held' FOR UPDATE`,
			messageID).Scan(&body, &convID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: this message is no longer held", httpx.ErrConflict)
			}
			return fmt.Errorf("load held message: %w", err)
		}

		if !release {
			_, err := tx.Exec(ctx, `UPDATE messages SET state = 'blocked' WHERE id = $1`, messageID)
			return err
		}

		scan := Analyse(body)
		state := "sent"
		if scan.Verdict != VerdictDeliver {
			state = "redacted"
		}
		if _, err := tx.Exec(ctx,
			`UPDATE messages SET state = $2, delivered_body = $3 WHERE id = $1`,
			messageID, state, scan.Delivered); err != nil {
			return fmt.Errorf("release message: %w", err)
		}
		_, err := tx.Exec(ctx, `
			UPDATE conversations c
			   SET last_message_at = now(),
			       buyer_unread  = buyer_unread  + CASE WHEN m.sender_id = c.seller_id THEN 1 ELSE 0 END,
			       seller_unread = seller_unread + CASE WHEN m.sender_id = c.buyer_id  THEN 1 ELSE 0 END
			  FROM messages m
			 WHERE m.id = $1 AND c.id = m.conversation_id`, messageID)
		return err
	})
}
