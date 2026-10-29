package company

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/payments"
	"github.com/marketplace/api/internal/modules/subscriptions"
	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct {
	pool *pgxpool.Pool
	subs *subscriptions.Service
	pay  *payments.Service
}

func NewService(pool *pgxpool.Pool, subs *subscriptions.Service, pay *payments.Service) *Service {
	return &Service{pool: pool, subs: subs, pay: pay}
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const companyCols = `c.id, c.name, c.legal_name, coalesce(c.trn,''), c.billing_email, coalesce(c.billing_phone,''),
	c.status, c.currency, c.credit_limit_minor, c.payment_terms_days,
	c.covered_weekdays, c.covered_slots, c.meal_cap_minor, coalesce(c.review_note,''), c.reviewed_at, c.created_at`

func scanCompany(row pgx.Row) (Company, error) {
	var c Company
	var days []int16
	err := row.Scan(&c.ID, &c.Name, &c.LegalName, &c.TRN, &c.BillingEmail, &c.BillingPhone,
		&c.Status, &c.Currency, &c.CreditLimitMinor, &c.PaymentTermsDays,
		&days, &c.Policy.Slots, &c.Policy.MealCapMinor, &c.ReviewNote, &c.ReviewedAt, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httpx.ErrNotFound
	}
	if err != nil {
		return c, fmt.Errorf("company: scan: %w", err)
	}
	c.Policy.Weekdays = make([]int, len(days))
	for i, d := range days {
		c.Policy.Weekdays[i] = int(d)
	}
	return c, nil
}

func userPhone(ctx context.Context, q querier, userID uuid.UUID) (string, error) {
	var phone string
	err := q.QueryRow(ctx, `SELECT phone FROM users WHERE id = $1`, userID).Scan(&phone)
	if err != nil {
		return "", fmt.Errorf("company: load user: %w", err)
	}
	return NormalizePhone(phone), nil
}

// link attaches invitations sent to the caller's phone number to their account.
func (s *Service) link(ctx context.Context, userID uuid.UUID) error {
	phone, err := userPhone(ctx, s.pool, userID)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE company_members SET user_id = $1, joined_at = coalesce(joined_at, now()), updated_at = now()
		 WHERE phone = $2 AND status = 'active' AND user_id IS NULL`, userID, phone)
	if err != nil {
		return fmt.Errorf("company: link membership: %w", err)
	}
	return nil
}

// role returns the caller's role in the company, or ErrNotFound if they are
// not a member: a company is invisible to outsiders.
func (s *Service) role(ctx context.Context, q querier, userID, companyID uuid.UUID) (string, error) {
	var role string
	err := q.QueryRow(ctx, `
		SELECT role FROM company_members
		 WHERE company_id = $1 AND user_id = $2 AND status = 'active'`, companyID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("company: load role: %w", err)
	}
	return role, nil
}

func (s *Service) requireAdmin(ctx context.Context, q querier, userID, companyID uuid.UUID) error {
	role, err := s.role(ctx, q, userID, companyID)
	if err != nil {
		return err
	}
	if role != "admin" {
		return httpx.Coded(httpx.ErrForbidden, "not_company_admin", "Only your company's lunch admins can do this.")
	}
	return nil
}

// Register creates a company awaiting staff approval, with the caller as its
// first admin.
func (s *Service) Register(ctx context.Context, userID uuid.UUID, in RegisterInput) (Company, error) {
	if err := in.validate(); err != nil {
		return Company{}, err
	}
	var out Company
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		phone, err := userPhone(ctx, tx, userID)
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO companies (name, legal_name, trn, billing_email, billing_phone, created_by)
			VALUES ($1, $2, NULLIF($3,''), $4, NULLIF($5,''), $6) RETURNING id`,
			strings.TrimSpace(in.Name), strings.TrimSpace(in.LegalName), strings.TrimSpace(in.TRN),
			strings.TrimSpace(in.BillingEmail), NormalizePhone(in.BillingPhone), userID).Scan(&id); err != nil {
			return fmt.Errorf("company: insert: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO company_members (company_id, phone, user_id, role, invited_by, joined_at)
			VALUES ($1, $2, $3, 'admin', $3, now())`, id, phone, userID); err != nil {
			return fmt.Errorf("company: insert admin: %w", err)
		}
		if err := insertOffice(ctx, tx, id, in.Office); err != nil {
			return err
		}
		out, err = scanCompany(tx.QueryRow(ctx, `SELECT `+companyCols+` FROM companies c WHERE c.id = $1`, id))
		return err
	})
	return out, err
}

func insertOffice(ctx context.Context, tx pgx.Tx, companyID uuid.UUID, o OfficeInput) error {
	if err := o.validate(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO company_offices (company_id, label, building, area, city, point)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), $5, ST_SetSRID(ST_MakePoint($7, $6), 4326)::geography)`,
		companyID, strings.TrimSpace(o.Label), strings.TrimSpace(o.Building), strings.TrimSpace(o.Area),
		strings.TrimSpace(o.City), o.Lat, o.Lng); err != nil {
		return fmt.Errorf("company: insert office: %w", err)
	}
	return nil
}

// Benefits lists the companies the caller belongs to, linking any invitation
// sent to their phone number first.
func (s *Service) Benefits(ctx context.Context, userID uuid.UUID) ([]Benefit, error) {
	if err := s.link(ctx, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+companyCols+`, m.role
		  FROM company_members m JOIN companies c ON c.id = m.company_id
		 WHERE m.user_id = $1 AND m.status = 'active' AND c.status <> 'rejected'
		 ORDER BY c.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("company: benefits: %w", err)
	}
	type row struct {
		c    Company
		role string
	}
	var found []row
	for rows.Next() {
		var r row
		var days []int16
		if err := rows.Scan(&r.c.ID, &r.c.Name, &r.c.LegalName, &r.c.TRN, &r.c.BillingEmail, &r.c.BillingPhone,
			&r.c.Status, &r.c.Currency, &r.c.CreditLimitMinor, &r.c.PaymentTermsDays,
			&days, &r.c.Policy.Slots, &r.c.Policy.MealCapMinor, &r.c.ReviewNote, &r.c.ReviewedAt, &r.c.CreatedAt,
			&r.role); err != nil {
			rows.Close()
			return nil, err
		}
		for _, d := range days {
			r.c.Policy.Weekdays = append(r.c.Policy.Weekdays, int(d))
		}
		found = append(found, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Benefit, 0, len(found))
	for _, r := range found {
		offices, err := s.offices(ctx, s.pool, r.c.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, Benefit{CompanyID: r.c.ID, Name: r.c.Name, Role: r.role, Status: r.c.Status,
			Policy: r.c.Policy, Offices: offices, Currency: r.c.Currency})
	}
	return out, nil
}

func (s *Service) offices(ctx context.Context, q querier, companyID uuid.UUID) ([]Office, error) {
	rows, err := q.Query(ctx, `
		SELECT id, label, coalesce(building,''), coalesce(area,''), city,
		       ST_Y(point::geometry), ST_X(point::geometry)
		  FROM company_offices WHERE company_id = $1 AND deleted_at IS NULL ORDER BY created_at`, companyID)
	if err != nil {
		return nil, fmt.Errorf("company: offices: %w", err)
	}
	defer rows.Close()
	out := []Office{}
	for rows.Next() {
		var o Office
		if err := rows.Scan(&o.ID, &o.Label, &o.Building, &o.Area, &o.City, &o.Lat, &o.Lng); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func credit(ctx context.Context, q querier, companyID uuid.UUID, limit int64) (Credit, error) {
	var c Credit
	err := q.QueryRow(ctx, `
		SELECT coalesce(sum(ch.amount_minor) FILTER (WHERE ch.statement_id IS NULL), 0),
		       coalesce(sum(ch.amount_minor) FILTER (WHERE st.status = 'issued'), 0),
		       coalesce(sum(ch.amount_minor) FILTER (WHERE ch.created_at >= date_trunc('month', now())), 0),
		       (SELECT count(*) FROM company_statements
		         WHERE company_id = $1 AND status = 'issued' AND due_on < CURRENT_DATE)
		  FROM company_charges ch
		  LEFT JOIN company_statements st ON st.id = ch.statement_id
		 WHERE ch.company_id = $1`, companyID).
		Scan(&c.UnbilledMinor, &c.OutstandingMinor, &c.MonthSpendMinor, &c.OverdueStatements)
	if err != nil {
		return c, fmt.Errorf("company: credit: %w", err)
	}
	return c.withLimit(limit), nil
}

// Dashboard is the company lunch page. Admins see everything; employees see
// the company and their own benefit only.
func (s *Service) Dashboard(ctx context.Context, userID, companyID uuid.UUID) (Dashboard, error) {
	if err := s.link(ctx, userID); err != nil {
		return Dashboard{}, err
	}
	role, err := s.role(ctx, s.pool, userID, companyID)
	if err != nil {
		return Dashboard{}, err
	}
	d, err := s.detail(ctx, companyID)
	if err != nil {
		return Dashboard{}, err
	}
	d.Role = role
	if role != "admin" {
		d.Members, d.Lunches, d.Statements, d.Unbilled = nil, nil, nil, nil
		d.Credit = Credit{}
	}
	return d, nil
}

func (s *Service) detail(ctx context.Context, companyID uuid.UUID) (Dashboard, error) {
	var d Dashboard
	c, err := scanCompany(s.pool.QueryRow(ctx, `SELECT `+companyCols+` FROM companies c WHERE c.id = $1`, companyID))
	if err != nil {
		return d, err
	}
	d.Company = c
	if d.Credit, err = credit(ctx, s.pool, companyID, c.CreditLimitMinor); err != nil {
		return d, err
	}
	if d.Offices, err = s.offices(ctx, s.pool, companyID); err != nil {
		return d, err
	}
	if d.Members, err = s.members(ctx, companyID); err != nil {
		return d, err
	}
	if d.Lunches, err = s.lunches(ctx, companyID); err != nil {
		return d, err
	}
	if d.Statements, err = s.statements(ctx, companyID); err != nil {
		return d, err
	}
	d.Unbilled, err = s.charges(ctx, `ch.company_id = $1 AND ch.statement_id IS NULL`, companyID)
	return d, err
}

func (s *Service) members(ctx context.Context, companyID uuid.UUID) ([]Member, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.phone, coalesce(m.name, u.display_name, ''), m.role, m.user_id IS NOT NULL, m.joined_at,
		       (SELECT count(*) FROM subscriptions s
		         WHERE s.company_id = m.company_id AND s.user_id = m.user_id
		           AND s.status IN ('active', 'paused', 'pending_payment'))
		  FROM company_members m LEFT JOIN users u ON u.id = m.user_id
		 WHERE m.company_id = $1 AND m.status = 'active'
		 ORDER BY m.role, m.created_at`, companyID)
	if err != nil {
		return nil, fmt.Errorf("company: members: %w", err)
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Phone, &m.Name, &m.Role, &m.Joined, &m.JoinedAt, &m.Lunches); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) lunches(ctx context.Context, companyID uuid.UUID) ([]Lunch, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.subscription_no, coalesce(m.name, u.display_name, ''), u.phone,
		       b.name, p.name, s.status, to_char(s.start_date, 'YYYY-MM-DD')
		  FROM subscriptions s
		  JOIN users u ON u.id = s.user_id
		  JOIN vendor_branches b ON b.id = s.branch_id
		  JOIN subscription_plans p ON p.id = s.plan_id
		  LEFT JOIN company_members m ON m.company_id = s.company_id AND m.user_id = s.user_id AND m.status = 'active'
		 WHERE s.company_id = $1 AND s.status IN ('active', 'paused', 'suspended', 'pending_payment')
		 ORDER BY s.created_at DESC LIMIT 500`, companyID)
	if err != nil {
		return nil, fmt.Errorf("company: lunches: %w", err)
	}
	defer rows.Close()
	out := []Lunch{}
	for rows.Next() {
		var l Lunch
		if err := rows.Scan(&l.SubscriptionID, &l.SubscriptionNo, &l.Employee, &l.Phone,
			&l.Kitchen, &l.Plan, &l.Status, &l.StartDate); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Service) statements(ctx context.Context, companyID uuid.UUID) ([]Statement, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, statement_no, to_char(period_end, 'YYYY-MM-DD'), total_minor, currency, status,
		       issued_at, to_char(due_on, 'YYYY-MM-DD'), paid_at, coalesce(paid_reference, '')
		  FROM company_statements WHERE company_id = $1 ORDER BY issued_at DESC LIMIT 60`, companyID)
	if err != nil {
		return nil, fmt.Errorf("company: statements: %w", err)
	}
	defer rows.Close()
	out := []Statement{}
	for rows.Next() {
		var st Statement
		if err := rows.Scan(&st.ID, &st.StatementNo, &st.PeriodEnd, &st.TotalMinor, &st.Currency, &st.Status,
			&st.IssuedAt, &st.DueOn, &st.PaidAt, &st.PaidReference); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Service) charges(ctx context.Context, where string, arg uuid.UUID) ([]Charge, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ch.id, ch.kind, ch.subscription_id, s.subscription_no, coalesce(u.display_name, u.phone),
		       b.name, ch.amount_minor, ch.currency, ch.created_at
		  FROM company_charges ch
		  JOIN subscriptions s ON s.id = ch.subscription_id
		  JOIN users u ON u.id = ch.user_id
		  JOIN vendor_branches b ON b.id = s.branch_id
		 WHERE `+where+`
		 ORDER BY ch.created_at`, arg)
	if err != nil {
		return nil, fmt.Errorf("company: charges: %w", err)
	}
	defer rows.Close()
	out := []Charge{}
	for rows.Next() {
		var c Charge
		if err := rows.Scan(&c.ID, &c.Kind, &c.SubscriptionID, &c.SubscriptionNo, &c.Employee,
			&c.Kitchen, &c.AmountMinor, &c.Currency, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Statement returns one statement with its lines, for a company admin.
func (s *Service) Statement(ctx context.Context, userID, companyID, statementID uuid.UUID) (Statement, error) {
	if err := s.requireAdmin(ctx, s.pool, userID, companyID); err != nil {
		return Statement{}, err
	}
	return s.statement(ctx, companyID, statementID)
}

func (s *Service) statement(ctx context.Context, companyID, statementID uuid.UUID) (Statement, error) {
	all, err := s.statements(ctx, companyID)
	if err != nil {
		return Statement{}, err
	}
	for _, st := range all {
		if st.ID == statementID {
			st.Lines, err = s.charges(ctx, `ch.statement_id = $1`, statementID)
			return st, err
		}
	}
	return Statement{}, httpx.ErrNotFound
}

func (s *Service) AddMember(ctx context.Context, userID, companyID uuid.UUID, phone, name, role string) error {
	if err := s.requireAdmin(ctx, s.pool, userID, companyID); err != nil {
		return err
	}
	phone = NormalizePhone(phone)
	if len(strings.TrimPrefix(phone, "+")) < 8 {
		return invalid("phone", "invalid", "enter the employee's mobile number")
	}
	if role == "" {
		role = "employee"
	}
	if role != "employee" && role != "admin" {
		return invalid("role", "invalid", "role is employee or admin")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO company_members (company_id, phone, user_id, name, role, invited_by, joined_at)
		SELECT $1, $2, u.id, NULLIF($3,''), $4, $5, CASE WHEN u.id IS NULL THEN NULL ELSE now() END
		  FROM (SELECT 1) one
		  LEFT JOIN users u ON u.phone = $2 AND u.deleted_at IS NULL`,
		companyID, phone, strings.TrimSpace(name), role, userID)
	if isUniqueViolation(err) {
		return httpx.Coded(httpx.ErrConflict, "already_member", "This number is already on your company's list.")
	}
	if err != nil {
		return fmt.Errorf("company: add member: %w", err)
	}
	return nil
}

// RemoveMember takes an employee off the company. Their office lunches are
// cancelled, which credits the unused days back to the company.
func (s *Service) RemoveMember(ctx context.Context, userID, companyID, memberID uuid.UUID) error {
	if err := s.requireAdmin(ctx, s.pool, userID, companyID); err != nil {
		return err
	}
	var memberUser *uuid.UUID
	var role string
	err := s.pool.QueryRow(ctx, `
		UPDATE company_members SET status = 'removed', removed_at = now(), updated_at = now()
		 WHERE id = $1 AND company_id = $2 AND status = 'active'
		   AND (role = 'employee' OR (SELECT count(*) FROM company_members
		         WHERE company_id = $2 AND role = 'admin' AND status = 'active') > 1)
		RETURNING user_id, role`, memberID, companyID).Scan(&memberUser, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.Coded(httpx.ErrConflict, "cannot_remove",
			"This person is not on the list, or is the company's last lunch admin.")
	}
	if err != nil {
		return fmt.Errorf("company: remove member: %w", err)
	}
	if memberUser == nil {
		return nil
	}
	return s.cancelLunches(ctx, companyID, memberUser, userID, "removed from company lunches")
}

func (s *Service) cancelLunches(ctx context.Context, companyID uuid.UUID, onlyUser *uuid.UUID, actorID uuid.UUID, reason string) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM subscriptions
		 WHERE company_id = $1 AND ($2::uuid IS NULL OR user_id = $2)
		   AND status IN ('active', 'paused', 'suspended', 'pending_payment')`, companyID, onlyUser)
	if err != nil {
		return fmt.Errorf("company: load lunches: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.subs.CancelByStaff(ctx, actorID, id, reason); err != nil {
			return fmt.Errorf("company: cancel lunch %s: %w", id, err)
		}
	}
	return rows.Err()
}

func (s *Service) SetPolicy(ctx context.Context, userID, companyID uuid.UUID, p Policy) error {
	if err := s.requireAdmin(ctx, s.pool, userID, companyID); err != nil {
		return err
	}
	if err := p.validate(); err != nil {
		return err
	}
	days := make([]int16, len(p.Weekdays))
	for i, d := range p.Weekdays {
		days[i] = int16(d)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE companies SET covered_weekdays = $2, covered_slots = $3, meal_cap_minor = $4, updated_at = now()
		 WHERE id = $1`, companyID, days, p.Slots, p.MealCapMinor)
	if err != nil {
		return fmt.Errorf("company: set policy: %w", err)
	}
	return nil
}

func (s *Service) AddOffice(ctx context.Context, userID, companyID uuid.UUID, o OfficeInput) error {
	if err := s.requireAdmin(ctx, s.pool, userID, companyID); err != nil {
		return err
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error { return insertOffice(ctx, tx, companyID, o) })
}

func (s *Service) RemoveOffice(ctx context.Context, userID, companyID, officeID uuid.UUID) error {
	if err := s.requireAdmin(ctx, s.pool, userID, companyID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE company_offices SET deleted_at = now()
		 WHERE id = $1 AND company_id = $2 AND deleted_at IS NULL
		   AND (SELECT count(*) FROM company_offices WHERE company_id = $2 AND deleted_at IS NULL) > 1`,
		officeID, companyID)
	if err != nil {
		return fmt.Errorf("company: remove office: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.Coded(httpx.ErrConflict, "cannot_remove", "A company needs at least one office.")
	}
	return nil
}

// endOfDay is used for statement periods, which close at the end of a day in
// the company's timezone.
func endOfDay(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, d.Location())
}
