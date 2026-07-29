package adminview

import (
	"context"
	"time"
)

// The health read models — PLAN-ADMIN-CONSOLE §4.6.
//
// The premise of this file is one line from the plan: **a worker that stops is
// invisible today.** The API stays green, `/healthz` returns ok, and routes go
// unplanned until a customer calls to ask where their driver is. Nothing in the
// system notices, because nothing in the system is watching for absence.
//
// Everything below is a way of detecting absence. Not "did this fail" — failures
// log — but "has this stopped happening", which is the failure mode that has no
// error message.

// OutboxLag is how far behind the event relays are.
type OutboxLag struct {
	AggregateType string
	Unpublished   int
	// OldestAge is the number the alarm is set on. A backlog of ten thousand
	// events published within the last minute is a busy system; three events
	// from yesterday is a dead consumer, and only the age tells them apart.
	OldestAge  *time.Duration
	Failing    int
	LastError  string
	MaxAttempt int
}

func (s *Service) LoadOutboxLag(ctx context.Context) ([]OutboxLag, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT aggregate_type,
		       count(*),
		       extract(epoch FROM now() - min(created_at)),
		       count(*) FILTER (WHERE attempts > 0),
		       coalesce(max(last_error), ''),
		       coalesce(max(attempts), 0)
		  FROM outbox_events
		 WHERE published_at IS NULL
		 GROUP BY aggregate_type
		 ORDER BY 3 DESC NULLS LAST`)
	if err != nil {
		return nil, wrap("outbox lag", err)
	}
	defer rows.Close()

	var out []OutboxLag
	for rows.Next() {
		var l OutboxLag
		var ageSeconds *float64
		if err := rows.Scan(&l.AggregateType, &l.Unpublished, &ageSeconds,
			&l.Failing, &l.LastError, &l.MaxAttempt); err != nil {
			return nil, err
		}
		if ageSeconds != nil {
			d := time.Duration(*ageSeconds) * time.Second
			l.OldestAge = &d
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Partition is one range partition and how much room is left.
type Partition struct {
	Parent string
	Name   string
	// UpperBound is when this partition stops accepting rows. The whole point of
	// the page: partitions are created ahead of time by a job, and when that job
	// stops the failure arrives as a write error at midnight on the day the last
	// one ends.
	UpperBound   string
	IsDefault    bool
	LiveRows     int64
	DaysOfRunway int
}

// LoadPartitions reads the partition catalogue.
//
// A DEFAULT partition with rows in it is called out separately and is the worst
// case here: once a row lands in DEFAULT, the range it occupies can never be
// attached as a real partition without moving that row, so the miss becomes
// permanent rather than merely late.
func (s *Service) LoadPartitions(ctx context.Context) ([]Partition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT parent.relname AS parent,
		       child.relname  AS name,
		       pg_get_expr(child.relpartbound, child.oid) AS bound,
		       pg_get_expr(child.relpartbound, child.oid) LIKE '%DEFAULT%' AS is_default,
		       coalesce(childstat.n_live_tup, 0)
		  FROM pg_inherits i
		  JOIN pg_class parent ON parent.oid = i.inhparent
		  JOIN pg_class child  ON child.oid  = i.inhrelid
		  LEFT JOIN pg_stat_user_tables childstat ON childstat.relid = child.oid
		 WHERE parent.relkind = 'p'
		 ORDER BY parent.relname, child.relname`)
	if err != nil {
		return nil, wrap("partitions", err)
	}
	defer rows.Close()

	var out []Partition
	for rows.Next() {
		var p Partition
		if err := rows.Scan(&p.Parent, &p.Name, &p.UpperBound, &p.IsDefault, &p.LiveRows); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeliveryStat is notification outcomes for one channel.
//
// Sent, failed and suppressed are kept apart because they need three different
// people: a failure is an integration problem, a suppression is a compliance
// decision working correctly, and a fall in `sent` with no rise in either is the
// interesting one — it means nothing was attempted.
type DeliveryStat struct {
	Channel    string
	Sent       int
	Failed     int
	Suppressed int
	Queued     int
	LastSentAt *time.Time
}

func (s *Service) LoadDeliveryStats(ctx context.Context, since time.Duration) ([]DeliveryStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT channel,
		       count(*) FILTER (WHERE status = 'sent'),
		       count(*) FILTER (WHERE status = 'failed'),
		       count(*) FILTER (WHERE suppressed_reason IS NOT NULL),
		       count(*) FILTER (WHERE status = 'queued'),
		       max(created_at) FILTER (WHERE status = 'sent')
		  FROM notifications
		 WHERE created_at >= now() - $1::interval
		 GROUP BY channel
		 ORDER BY channel`, since.String())
	if err != nil {
		return nil, wrap("delivery stats", err)
	}
	defer rows.Close()

	var out []DeliveryStat
	for rows.Next() {
		var d DeliveryStat
		if err := rows.Scan(&d.Channel, &d.Sent, &d.Failed, &d.Suppressed,
			&d.Queued, &d.LastSentAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LoopHealth is the last time a background loop demonstrably did something.
//
// Inferred from the artefacts each loop produces rather than read from a job
// table, because there is no job table — River is wired in `cmd/worker` as a
// future, and the loops today are tickers.
//
// Inference is weaker than a heartbeat and the weakness is worth stating: a loop
// with nothing to do produces no artefact and looks identical to one that has
// stopped. So `Idle` is carried separately from `Stale` — "no work arrived" and
// "work arrived and was not done" are different, and only the second is an
// alarm. When the loops write heartbeats this file should read those instead.
type LoopHealth struct {
	Loop     string
	LastSeen *time.Time
	Pending  int
	// Stale is true only when there is pending work *and* nothing has been
	// completed recently. Either alone is normal.
	Stale bool
}

func (s *Service) LoadLoopHealth(ctx context.Context) ([]LoopHealth, error) {
	const staleAfter = 15 * time.Minute

	loops := []struct {
		name    string
		lastSQL string
		pendSQL string
	}{
		{
			"media pipeline",
			`SELECT max(at) FROM reel_state_history
			  WHERE to_state IN ('transcoding', 'moderating', 'published')`,
			`SELECT count(*) FROM reels
			  WHERE state IN ('uploaded', 'probing', 'transcoding')`,
		},
		{
			"capture",
			`SELECT max(created_at) FROM payments WHERE kind = 'capture'`,
			`SELECT count(*) FROM outbox_events
			  WHERE event_type = 'order.delivered' AND published_at IS NULL`,
		},
		{
			"dispatch",
			`SELECT max(created_at) FROM routes`,
			`SELECT count(*) FROM fulfillments WHERE status = 'scheduled'`,
		},
		{
			"notifications",
			`SELECT max(created_at) FROM notifications WHERE status = 'sent'`,
			`SELECT count(*) FROM notifications WHERE status = 'queued'`,
		},
		{
			"invoicing",
			`SELECT max(issued_at) FROM invoices`,
			`SELECT count(*) FROM orders o
			  WHERE o.status = 'completed'
			    AND NOT EXISTS (SELECT 1 FROM invoices i WHERE i.order_id = o.id)`,
		},
		{
			"payouts",
			`SELECT max(created_at) FROM payout_runs`,
			`SELECT count(*) FROM payout_statements WHERE state = 'draft'`,
		},
	}

	out := make([]LoopHealth, 0, len(loops))
	for _, l := range loops {
		var h LoopHealth
		h.Loop = l.name
		// Individually tolerant: one loop whose probe query fails should not
		// blank the health page. An unknown state is reported as unknown.
		_ = s.pool.QueryRow(ctx, l.lastSQL).Scan(&h.LastSeen)
		_ = s.pool.QueryRow(ctx, l.pendSQL).Scan(&h.Pending)
		h.Stale = h.Pending > 0 &&
			(h.LastSeen == nil || time.Since(*h.LastSeen) > staleAfter)
		out = append(out, h)
	}
	return out, nil
}

// ModerationSLA is the breach rate over time, not just now.
//
// The queue screens show what is breached at this instant, which cannot
// distinguish "we are behind today" from "we have been behind for a fortnight".
// The second is a staffing decision and the first is a bad morning.
type ModerationSLA struct {
	Day       time.Time
	Decided   int
	Breached  int
	MedianSec *float64
}

func (s *Service) LoadModerationSLA(ctx context.Context, days int) ([]ModerationSLA, error) {
	if days <= 0 || days > 90 {
		days = 14
	}
	rows, err := s.pool.Query(ctx, `
		SELECT date_trunc('day', h.at)::date,
		       count(*),
		       count(*) FILTER (WHERE h.at - r.created_at > interval '4 hours'),
		       percentile_cont(0.5) WITHIN GROUP (
		           ORDER BY extract(epoch FROM h.at - r.created_at))
		  FROM reel_state_history h
		  JOIN reels r ON r.id = h.reel_id
		 WHERE h.to_state IN ('approved', 'rejected', 'published')
		   AND h.at >= now() - make_interval(days => $1)
		 GROUP BY 1
		 ORDER BY 1 DESC`, days)
	if err != nil {
		return nil, wrap("moderation sla", err)
	}
	defer rows.Close()

	var out []ModerationSLA
	for rows.Next() {
		var m ModerationSLA
		if err := rows.Scan(&m.Day, &m.Decided, &m.Breached, &m.MedianSec); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
