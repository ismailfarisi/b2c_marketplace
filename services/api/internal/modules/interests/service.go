package interests

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service reads and rebuilds interest profiles.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Load returns a viewer's category affinities.
//
// Only the strongest few. A viewer with forty categories in their profile does
// not have forty preferences, they have three and a long tail of noise — and
// carrying the tail into the retrieval query turns an index lookup into a scan.
const loadLimit = 12

func (s *Service) Load(ctx context.Context, viewerKey string) (map[uuid.UUID]float64, error) {
	if viewerKey == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT category_id, affinity
		  FROM viewer_interests
		 WHERE viewer_key = $1
		 ORDER BY abs(affinity) DESC
		 LIMIT $2`, viewerKey, loadLimit)
	if err != nil {
		return nil, fmt.Errorf("interests: load: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]float64, loadLimit)
	for rows.Next() {
		var id uuid.UUID
		var affinity float64
		if err := rows.Scan(&id, &affinity); err != nil {
			return nil, fmt.Errorf("interests: scan: %w", err)
		}
		out[id] = affinity
	}
	return out, rows.Err()
}

// LinkDevice folds an anonymous device profile into a signed-in one.
//
// Called once, at login. This is the moment the entire viewer_key design exists
// for: a viewer browses anonymously, builds a real profile over three sessions,
// then signs in to place their first order — and without this call their new
// account starts with no history at all, so the first personalised feed they
// ever see is the worst one.
//
// Additive merge, capped. Two profiles that both like food should end up liking
// food, not liking it twice.
func (s *Service) LinkDevice(ctx context.Context, deviceKey, userKey string, userID uuid.UUID) error {
	if deviceKey == "" || userKey == "" || deviceKey == userKey {
		return nil
	}

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO viewer_interests
		    (viewer_key, category_id, user_id, affinity,
		     positive_signals, negative_signals, last_signal_at, updated_at)
		SELECT $2, d.category_id, $3,
		       greatest(-1, least(1, d.affinity)),
		       d.positive_signals, d.negative_signals, d.last_signal_at, now()
		  FROM viewer_interests d
		 WHERE d.viewer_key = $1
		ON CONFLICT (viewer_key, category_id) DO UPDATE
		   SET affinity = greatest(-1, least(1,
		           viewer_interests.affinity + EXCLUDED.affinity * 0.5)),
		       positive_signals = viewer_interests.positive_signals + EXCLUDED.positive_signals,
		       negative_signals = viewer_interests.negative_signals + EXCLUDED.negative_signals,
		       last_signal_at = greatest(viewer_interests.last_signal_at, EXCLUDED.last_signal_at),
		       user_id = $3,
		       updated_at = now()`,
		deviceKey, userKey, userID,
	); err != nil {
		return fmt.Errorf("interests: link device: %w", err)
	}

	// The device rows are left in place. The same device is very often the same
	// person browsing before they next sign in, and deleting the history would
	// re-create the cold start this function exists to remove. The staleness
	// reaper collects them when they genuinely go quiet.
	return nil
}

// RebuildBatch recomputes profiles for viewers who have been active since the
// last run.
//
// Batched by viewer rather than run as one enormous GROUP BY: the aggregation
// has to be re-done from scratch for a viewer whose signals decayed, and doing
// that for every viewer who has ever used the product would be a job whose cost
// grows with the install base rather than with the traffic.
func (s *Service) RebuildBatch(ctx context.Context, since time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}

	keys, err := s.activeViewers(ctx, since, limit)
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}

	signals, err := s.signals(ctx, keys)
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	rebuilt := 0
	for key, sigs := range signals {
		profiles := Fold(sigs, now)
		if len(profiles) == 0 {
			continue
		}
		if err := s.save(ctx, key, profiles); err != nil {
			return rebuilt, err
		}
		rebuilt++
	}
	return rebuilt, nil
}

func (s *Service) activeViewers(ctx context.Context, since time.Time, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT viewer_key FROM (
		    SELECT viewer_key FROM client_events
		     WHERE viewer_key IS NOT NULL AND occurred_at > $1
		     LIMIT $2 * 20
		    UNION
		    SELECT viewer_key FROM action_clicks
		     WHERE clicked_at > $1
		     LIMIT $2 * 20
		) v
		LIMIT $2`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("interests: active viewers: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("interests: scan viewer: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// signals pulls the raw interactions for a batch of viewers.
//
// Raw rows, aggregated in Go rather than in SQL. The decay weights and the
// squashing curve are the whole of the personalisation policy, and they belong
// somewhere a test can reach them — a CASE expression inside a GROUP BY is
// where tuning parameters go to become folklore.
//
// The per-viewer cap is applied in the window function so that one bot session
// with 200,000 events cannot make this batch unbounded.
func (s *Service) signals(ctx context.Context, viewerKeys []string) (map[string][]Signal, error) {
	rows, err := s.pool.Query(ctx, `
		WITH raw AS (
		    SELECT ce.viewer_key, r.category_id, ce.event_type AS kind, ce.occurred_at AS at
		      FROM client_events ce
		      JOIN reels r ON r.id = ce.reel_id
		     WHERE ce.viewer_key = ANY($1)
		       AND ce.occurred_at > now() - make_interval(secs => $2)
		       AND r.category_id IS NOT NULL
		    UNION ALL
		    -- The click log rather than the client's event copy: a CTA press is
		    -- the strongest signal this package uses, and the server-recorded
		    -- version is the one that cannot be dropped by a flaky network.
		    SELECT ac.viewer_key, r.category_id, 'action_click', ac.clicked_at
		      FROM action_clicks ac
		      JOIN reels r ON r.id = ac.reel_id
		     WHERE ac.viewer_key = ANY($1)
		       AND ac.clicked_at > now() - make_interval(secs => $2)
		       AND r.category_id IS NOT NULL
		),
		capped AS (
		    SELECT raw.*,
		           row_number() OVER (PARTITION BY viewer_key ORDER BY at DESC) AS rn
		      FROM raw
		)
		SELECT viewer_key, category_id, kind,
		       EXTRACT(EPOCH FROM (now() - at))::float8 AS age_seconds
		  FROM capped
		 WHERE rn <= $3`,
		viewerKeys, Window.Seconds(), PerViewerCap)
	if err != nil {
		return nil, fmt.Errorf("interests: signals: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]Signal, len(viewerKeys))
	for rows.Next() {
		var key, kind string
		var categoryID uuid.UUID
		var ageSeconds float64
		if err := rows.Scan(&key, &categoryID, &kind, &ageSeconds); err != nil {
			return nil, fmt.Errorf("interests: scan signal: %w", err)
		}
		out[key] = append(out[key], Signal{
			CategoryID: categoryID,
			Kind:       kind,
			Age:        time.Duration(ageSeconds * float64(time.Second)),
		})
	}
	return out, rows.Err()
}

// save replaces one viewer's profile.
//
// Replace, not accumulate. Fold recomputes from the full decayed history every
// time, so an accumulating upsert would add the same signals again on every
// run — and a profile that grows monotonically with how often the job happens
// to run is not a profile, it is a rebuild counter.
func (s *Service) save(ctx context.Context, viewerKey string, profiles []Profile) error {
	ids := make([]uuid.UUID, len(profiles))
	affinities := make([]float64, len(profiles))
	positives := make([]int64, len(profiles))
	negatives := make([]int64, len(profiles))
	lastSignals := make([]time.Time, len(profiles))

	for i, p := range profiles {
		ids[i] = p.CategoryID
		affinities[i] = p.Affinity
		positives[i] = p.Positive
		negatives[i] = p.Negative
		lastSignals[i] = p.LastSignal
	}

	// A viewer key of the form "u:<uuid>" carries the user id; a device key does
	// not. Parsed rather than passed so callers cannot get the two out of sync.
	var userID any
	if id, ok := UserIDFromKey(viewerKey); ok {
		userID = id
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO viewer_interests
		    (viewer_key, category_id, user_id, affinity,
		     positive_signals, negative_signals, last_signal_at, updated_at)
		SELECT $1, u.category_id, $2, u.affinity, u.positive, u.negative, u.last_signal, now()
		  FROM unnest($3::uuid[], $4::float8[], $5::bigint[], $6::bigint[], $7::timestamptz[])
		    AS u(category_id, affinity, positive, negative, last_signal)
		ON CONFLICT (viewer_key, category_id) DO UPDATE
		   SET affinity = EXCLUDED.affinity,
		       user_id = EXCLUDED.user_id,
		       positive_signals = EXCLUDED.positive_signals,
		       negative_signals = EXCLUDED.negative_signals,
		       last_signal_at = EXCLUDED.last_signal_at,
		       updated_at = now()`,
		viewerKey, userID, ids, affinities, positives, negatives, lastSignals)
	if err != nil {
		return fmt.Errorf("interests: save profile: %w", err)
	}
	return nil
}

// Reap drops profiles nobody has touched in a long time.
//
// Anonymous device keys accumulate forever otherwise — a device key is created
// by anyone who opens the app once, and the table would grow with installs
// rather than with users.
func (s *Service) Reap(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM viewer_interests
		 WHERE updated_at < now() - make_interval(secs => $1)
		   AND user_id IS NULL`, olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("interests: reap: %w", err)
	}
	return tag.RowsAffected(), nil
}

// UserIDFromKey extracts the user id from a "u:<uuid>" viewer key.
func UserIDFromKey(viewerKey string) (uuid.UUID, bool) {
	const prefix = "u:"
	if len(viewerKey) <= len(prefix) || viewerKey[:len(prefix)] != prefix {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(viewerKey[len(prefix):])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
