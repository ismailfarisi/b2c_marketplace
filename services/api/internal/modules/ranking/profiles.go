package ranking

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProfileTTL is how long a loaded profile is trusted.
//
// A minute, not a request. Reading ranking_profiles on every feed request would
// add a round-trip to the hottest read path in the product to fetch a row that
// changes twice a week. A minute of staleness after a weight change is
// invisible to everyone except the person who made it, and they can wait.
const ProfileTTL = time.Minute

// Provider supplies the active weights.
//
// An interface so the feed can be constructed without a database in tests, and
// so a future experiment framework can swap in per-viewer weight assignment
// without the feed learning what an experiment is.
type Provider interface {
	Weights(ctx context.Context) Weights
}

// StaticProvider always returns the same weights.
type StaticProvider struct{ W Weights }

func (p StaticProvider) Weights(context.Context) Weights { return p.W }

// DBProvider reads the active profile from ranking_profiles, cached.
type DBProvider struct {
	pool *pgxpool.Pool

	mu       sync.RWMutex
	cached   Weights
	name     string
	loadedAt time.Time
}

func NewDBProvider(pool *pgxpool.Pool) *DBProvider {
	return &DBProvider{pool: pool, cached: DefaultWeights()}
}

// Weights returns the active profile, refreshing at most once per TTL.
//
// It never returns an error and never returns zero weights. Every failure path
// — no row, malformed json, failed validation, database unreachable — falls
// back to the last good value and ultimately to DefaultWeights. That is a
// deliberate asymmetry: ranking running on stale or default weights is a
// product that works, and ranking that refuses to load is a blank front door.
func (p *DBProvider) Weights(ctx context.Context) Weights {
	p.mu.RLock()
	if time.Since(p.loadedAt) < ProfileTTL {
		w := p.cached
		p.mu.RUnlock()
		return w
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	// Re-check: several requests can arrive at an expired cache at once, and
	// there is no reason for all of them to query.
	if time.Since(p.loadedAt) < ProfileTTL {
		return p.cached
	}

	name, w, err := p.load(ctx)
	if err != nil {
		// Push the next attempt out by a TTL rather than retrying per request.
		// A ranking table that is unreadable is usually a database that is
		// unwell, and hammering it from the feed's request path makes it worse.
		p.loadedAt = time.Now()
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(ctx, "ranking profile unavailable, using previous weights",
				"error", err, "profile", p.name)
		}
		return p.cached
	}

	if name != p.name {
		slog.InfoContext(ctx, "ranking profile changed", "from", p.name, "to", name)
	}
	p.cached, p.name, p.loadedAt = w, name, time.Now()
	return p.cached
}

func (p *DBProvider) load(ctx context.Context) (string, Weights, error) {
	// Bounded independently of the caller's deadline. This runs inside a feed
	// request, and a slow config read must not eat the budget the actual feed
	// query needs.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var name string
	var raw []byte
	err := p.pool.QueryRow(ctx,
		`SELECT name, weights FROM ranking_profiles WHERE active LIMIT 1`).Scan(&name, &raw)
	if err != nil {
		return "", Weights{}, err
	}

	w, err := ParseWeights(raw)
	if err != nil {
		// ParseWeights already returned DefaultWeights, but a profile that fails
		// validation is a config error someone needs to fix, so it is logged at
		// the level that reaches them rather than swallowed.
		return "", Weights{}, err
	}
	return name, w, nil
}

// Active reports the name of the profile currently in force, for the staff
// endpoint that answers "what is ranking the feed right now".
func (p *DBProvider) Active() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.name == "" {
		return "default (compiled-in)"
	}
	return p.name
}
