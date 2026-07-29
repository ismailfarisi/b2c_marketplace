package admin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
)

// Permissions — PLAN-ADMIN-CONSOLE §3.5.
//
// `RequireStaff` was a boolean and it was the right shape while the only staff
// surface was a moderation queue: the worst a moderator could do was judge a
// reel. It stops being right the moment a detail page exists, because the first
// one hands every moderator a customer's home address.
//
// The keys below mirror the `admin_permissions` seed in migration 000020. The
// table is the vocabulary; these constants are how the code names it, and
// `VerifyCatalogue` asserts the two have not drifted.

const (
	PermContentReview   = "content.review"
	PermContentStrike   = "content.strike"
	PermCommerceRead    = "commerce.read"
	PermCommerceContact = "commerce.contact"
	PermCommerceWrite   = "commerce.write"
	PermMoneyRead       = "money.read"
	PermMoneyWrite      = "money.write"
	PermIdentityReview  = "identity.review"
	PermConfigWrite     = "config.write"
	PermStaffAdmin      = "staff.admin"
	PermHealthRead      = "health.read"
)

// All is every permission the code knows about.
var All = []string{
	PermContentReview, PermContentStrike,
	PermCommerceRead, PermCommerceContact, PermCommerceWrite,
	PermMoneyRead, PermMoneyWrite,
	PermIdentityReview, PermConfigWrite, PermStaffAdmin, PermHealthRead,
}

// Grant is what a staff member holds, resolved.
type Grant struct {
	UserID      uuid.UUID
	Roles       []string
	Permissions map[string]bool
	// Sensitive names the permissions whose use is logged to `audit_log`. Read
	// once here rather than looked up per request — the set changes when a
	// migration runs, not while a process is alive.
	Sensitive map[string]bool
}

func (g Grant) Can(perm string) bool { return g.Permissions[perm] }

// Any reports whether the holder has at least one of the listed permissions.
//
// Used where two different roles legitimately reach the same screen from
// different directions — an order is visible to support and to finance, and
// requiring both would lock out each.
func (g Grant) Any(perms ...string) bool {
	for _, p := range perms {
		if g.Permissions[p] {
			return true
		}
	}
	return false
}

// cache holds resolved grants briefly.
//
// Thirty seconds, and the number is a deliberate trade against the rule Phase 2
// set when `platform_admins` was created: *revoking someone must take effect
// immediately*. A per-request join across three tables on every admin call is
// wasteful; a five-minute cache would mean a revoked contractor keeps working
// through an incident. Thirty seconds is short enough that "revoke and watch"
// is still a coherent action and long enough to collapse the burst of calls one
// page load makes.
const grantTTL = 30 * time.Second

type cachedGrant struct {
	grant   Grant
	expires time.Time
}

type permissionCache struct {
	mu sync.RWMutex
	m  map[uuid.UUID]cachedGrant
}

func (c *permissionCache) get(id uuid.UUID) (Grant, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.m[id]
	if !ok || time.Now().After(e.expires) {
		return Grant{}, false
	}
	return e.grant, true
}

func (c *permissionCache) put(id uuid.UUID, g Grant) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[uuid.UUID]cachedGrant{}
	}
	c.m[id] = cachedGrant{grant: g, expires: time.Now().Add(grantTTL)}
}

// Invalidate drops a user's cached grant.
//
// Called when a grant is changed through the console, so the person doing the
// revoking sees it take effect rather than being told to wait thirty seconds —
// which is the moment they would otherwise conclude the feature is broken.
func (s *Service) Invalidate(userID uuid.UUID) {
	s.perms.mu.Lock()
	defer s.perms.mu.Unlock()
	delete(s.perms.m, userID)
}

// Grants resolves everything a user holds.
//
// Expired grants are filtered here rather than by an index predicate — `now()`
// is not immutable and Postgres refuses it in one. A grant that has lapsed is
// simply not returned, which is the whole of what "time-boxed" means.
func (s *Service) Grants(ctx context.Context, userID uuid.UUID) (Grant, error) {
	if g, ok := s.perms.get(userID); ok {
		return g, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT g.role_key, rp.permission_key, p.is_sensitive
		  FROM admin_grants g
		  JOIN admin_role_permissions rp ON rp.role_key = g.role_key
		  JOIN admin_permissions p ON p.key = rp.permission_key
		 WHERE g.user_id = $1
		   AND (g.expires_at IS NULL OR g.expires_at > now())`, userID)
	if err != nil {
		return Grant{}, fmt.Errorf("admin: resolve grants: %w", err)
	}
	defer rows.Close()

	g := Grant{
		UserID:      userID,
		Permissions: map[string]bool{},
		Sensitive:   map[string]bool{},
	}
	seenRole := map[string]bool{}
	for rows.Next() {
		var role, perm string
		var sensitive bool
		if err := rows.Scan(&role, &perm, &sensitive); err != nil {
			return Grant{}, err
		}
		if !seenRole[role] {
			seenRole[role] = true
			g.Roles = append(g.Roles, role)
		}
		g.Permissions[perm] = true
		if sensitive {
			g.Sensitive[perm] = true
		}
	}
	if err := rows.Err(); err != nil {
		return Grant{}, err
	}

	s.perms.put(userID, g)
	return g, nil
}

// Require resolves the caller and checks one permission.
//
// Returns 403 with the permission named. Naming it is deliberate: a staff member
// who is told "forbidden" opens a ticket, and one who is told "this needs
// money.write, you hold support" asks the right person for the right thing.
// The permission vocabulary is not a secret — the *grants* are.
func (s *Service) Require(ctx context.Context, userID uuid.UUID, perm string) (Grant, error) {
	g, err := s.Grants(ctx, userID)
	if err != nil {
		return Grant{}, err
	}
	if !g.Can(perm) {
		held := "nothing"
		if len(g.Roles) > 0 {
			held = fmt.Sprintf("%v", g.Roles)
		}
		return g, fmt.Errorf("%w: this needs %s; you hold %s", httpx.ErrForbidden, perm, held)
	}
	return g, nil
}

// VerifyCatalogue asserts the constants above match the database.
//
// Called at startup. A permission that exists in Go and not in the table is a
// check that can never pass; one that exists in the table and not in Go is a
// permission nothing enforces — and both fail silently, which is the failure
// mode this exists to convert into a refusal to boot.
func (s *Service) VerifyCatalogue(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT key FROM admin_permissions`)
	if err != nil {
		return fmt.Errorf("admin: read permission catalogue: %w", err)
	}
	defer rows.Close()

	inDB := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return err
		}
		inDB[k] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	inGo := map[string]bool{}
	for _, p := range All {
		inGo[p] = true
		if !inDB[p] {
			return fmt.Errorf("admin: permission %q is enforced in code and missing from "+
				"admin_permissions — the check can never pass", p)
		}
	}
	for k := range inDB {
		if !inGo[k] {
			return fmt.Errorf("admin: permission %q exists in admin_permissions and is "+
				"enforced nowhere — a grant of it means nothing", k)
		}
	}
	return nil
}

// --- staff administration ---------------------------------------------------

// StaffMember is one person with access, as the staff page shows them.
type StaffMember struct {
	UserID      uuid.UUID
	Phone       string
	DisplayName string
	Roles       []string
	Permissions []string
	Locales     []string
	ExpiresAt   *time.Time
	GrantedAt   time.Time
}

func (s *Service) Staff(ctx context.Context, actorID uuid.UUID) ([]StaffMember, error) {
	if _, err := s.Require(ctx, actorID, PermStaffAdmin); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.phone, coalesce(u.display_name, ''),
		       array_agg(DISTINCT g.role_key ORDER BY g.role_key),
		       array_agg(DISTINCT rp.permission_key ORDER BY rp.permission_key),
		       coalesce(pa.locales, ARRAY[]::text[]),
		       min(g.expires_at), min(g.granted_at)
		  FROM admin_grants g
		  JOIN users u ON u.id = g.user_id
		  LEFT JOIN platform_admins pa ON pa.user_id = g.user_id
		  LEFT JOIN admin_role_permissions rp ON rp.role_key = g.role_key
		 WHERE g.expires_at IS NULL OR g.expires_at > now()
		 GROUP BY u.id, u.phone, u.display_name, pa.locales
		 ORDER BY u.phone`)
	if err != nil {
		return nil, fmt.Errorf("admin: list staff: %w", err)
	}
	defer rows.Close()

	var out []StaffMember
	for rows.Next() {
		var m StaffMember
		if err := rows.Scan(&m.UserID, &m.Phone, &m.DisplayName, &m.Roles,
			&m.Permissions, &m.Locales, &m.ExpiresAt, &m.GrantedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GrantRole gives someone a role.
//
// Requires a reason, like every other destructive-or-privileged action in this
// console. "Why does this contractor hold finance" is a question asked during an
// audit, and the answer has to be in the row rather than in somebody's memory.
func (s *Service) GrantRole(ctx context.Context, actorID, userID uuid.UUID, role, reason string, expires *time.Time) error {
	if _, err := s.Require(ctx, actorID, PermStaffAdmin); err != nil {
		return err
	}
	if reason == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "a grant with no stated reason cannot be reviewed later",
		}}}
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO admin_grants (user_id, role_key, granted_by, reason, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, role_key)
		DO UPDATE SET granted_by = EXCLUDED.granted_by, granted_at = now(),
		              reason = EXCLUDED.reason, expires_at = EXCLUDED.expires_at`,
		userID, role, actorID, reason, expires)
	if err != nil {
		return fmt.Errorf("admin: grant role: %w", err)
	}

	// The console also needs a `platform_admins` row, because Phase 2's staff
	// checks still read it. Kept in step here rather than leaving two sources of
	// "is this person staff" to drift.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO platform_admins (user_id, role) VALUES ($1, 'ops')
		ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		return fmt.Errorf("admin: ensure staff row: %w", err)
	}

	s.Invalidate(userID)
	return nil
}

// RevokeRole takes one away.
func (s *Service) RevokeRole(ctx context.Context, actorID, userID uuid.UUID, role, reason string) error {
	if _, err := s.Require(ctx, actorID, PermStaffAdmin); err != nil {
		return err
	}

	// The last superadmin cannot revoke themselves. Not paternalism: a console
	// with no superadmin has no way back except a migration, and the moment it
	// happens is always an incident.
	if role == "superadmin" {
		var others int
		if err := s.pool.QueryRow(ctx, `
			SELECT count(*) FROM admin_grants
			 WHERE role_key = 'superadmin' AND user_id <> $1
			   AND (expires_at IS NULL OR expires_at > now())`, userID).Scan(&others); err != nil {
			return fmt.Errorf("admin: count superadmins: %w", err)
		}
		if others == 0 {
			return fmt.Errorf("%w: this is the last superadmin — grant another first",
				httpx.ErrConflict)
		}
	}

	tag, err := s.pool.Exec(ctx,
		`DELETE FROM admin_grants WHERE user_id = $1 AND role_key = $2`, userID, role)
	if err != nil {
		return fmt.Errorf("admin: revoke role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}

	s.Invalidate(userID)
	return nil
}

// Roles lists what can be granted.
func (s *Service) Roles(ctx context.Context) ([]Role, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.key, r.name, r.description, r.is_builtin,
		       coalesce(array_agg(rp.permission_key ORDER BY rp.permission_key)
		                FILTER (WHERE rp.permission_key IS NOT NULL), ARRAY[]::text[])
		  FROM admin_roles r
		  LEFT JOIN admin_role_permissions rp ON rp.role_key = r.key
		 GROUP BY r.key, r.name, r.description, r.is_builtin
		 ORDER BY r.key`)
	if err != nil {
		return nil, fmt.Errorf("admin: list roles: %w", err)
	}
	defer rows.Close()

	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.Key, &r.Name, &r.Description, &r.IsBuiltin, &r.Permissions); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type Role struct {
	Key         string
	Name        string
	Description string
	IsBuiltin   bool
	Permissions []string
}

// --- PII access logging -----------------------------------------------------

// RecordAccess writes an `audit_log` row for a read of personal data.
//
// PLAN-ADMIN-CONSOLE §3.4. The rule most likely to be cut as gold-plating, and
// it should not be: the question a regulator asks after a breach is not "was it
// encrypted", it is **who looked at it**, and a system that cannot answer has no
// answer.
//
// Scoped to routes that return contact details or identity documents rather than
// every GET. An audit log that records everything is one nobody reads, and the
// signal — "this agent opened forty customer records in an hour" — is only
// visible when the log is mostly quiet.
//
// Best-effort by design: failing a support agent's page load because an audit
// row could not be written helps nobody, and the write is on the same database
// the read just succeeded against, so a failure here means something larger is
// already wrong.
func (s *Service) RecordAccess(ctx context.Context, in Access) {
	// Both casts are load-bearing and both were missing.
	//
	// `ip` is `inet`, and pgx sent a Go string as untyped text — Postgres refuses
	// the assignment, so *every* audited read failed. Silently, because this
	// function logs rather than returns: the console reported that it was
	// recording who read what, the warn line went to a log nobody greps, and the
	// table stayed empty. Found by looking for a row that should have been there
	// and was not, which is the only way this class of bug is ever found.
	//
	// `request_id` was cast to uuid against a `text` column. That worked only
	// because every request id happens to be a uuid today; the first component
	// that sets a non-uuid correlation id would have broken auditing again, in
	// exactly the same invisible way.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (actor_type, actor_id, action, target_type, target_id,
		                       changes, ip, user_agent, request_id)
		VALUES ('admin', $1, $2, $3, $4, $5,
		        nullif($6, '')::inet, nullif($7, ''), nullif($8, ''))`,
		in.ActorID, "read:"+in.Permission, in.TargetType, in.TargetID,
		nil, in.IP, in.UserAgent, in.RequestID); err != nil {
		// Logged, not returned. See the doc comment.
		slog.WarnContext(ctx, "pii access not audited",
			"target_type", in.TargetType, "target_id", in.TargetID, "error", err)
	}
}

type Access struct {
	ActorID    uuid.UUID
	Permission string
	TargetType string
	TargetID   uuid.UUID
	IP         string
	UserAgent  string
	RequestID  string
}

// AccessHistory answers "who looked at this record".
func (s *Service) AccessHistory(ctx context.Context, actorID uuid.UUID, targetType string, targetID uuid.UUID) ([]AccessEntry, error) {
	if _, err := s.Require(ctx, actorID, PermStaffAdmin); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT a.occurred_at, a.action, coalesce(u.display_name, u.phone, '')
		  FROM audit_log a
		  LEFT JOIN users u ON u.id = a.actor_id
		 WHERE a.actor_type = 'admin' AND a.target_type = $1 AND a.target_id = $2
		 ORDER BY a.occurred_at DESC
		 LIMIT 200`, targetType, targetID)
	if err != nil {
		return nil, fmt.Errorf("admin: access history: %w", err)
	}
	defer rows.Close()

	var out []AccessEntry
	for rows.Next() {
		var e AccessEntry
		if err := rows.Scan(&e.At, &e.Action, &e.Actor); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type AccessEntry struct {
	At     time.Time
	Action string
	Actor  string
}

// IsStaff reports whether a user holds any live grant.
//
// Used by view-as to refuse impersonating a colleague. Reads the table rather
// than the permission cache: the cache is keyed on the *caller* and this asks
// about somebody else, and a cache miss answering "no" would be a security
// answer derived from a performance structure.
func (s *Service) IsStaff(ctx context.Context, userID uuid.UUID) (bool, error) {
	var yes bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM admin_grants
			 WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > now()))`,
		userID).Scan(&yes)
	if err != nil {
		return false, fmt.Errorf("check staff: %w", err)
	}
	return yes, nil
}
