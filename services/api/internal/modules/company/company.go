// Package company is office lunch accounts: a company pays for its employees'
// lunches within a policy, and settles a statement by bank transfer.
//
// Employee lunches are ordinary subscriptions with company_id set. The company
// only changes who pays: the billing order is authorized on the company's
// account instead of a card, so kitchens, prep sheets, ledger and payouts are
// untouched.
package company

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
)

type Company struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	LegalName        string     `json:"legal_name"`
	TRN              string     `json:"trn"`
	BillingEmail     string     `json:"billing_email"`
	BillingPhone     string     `json:"billing_phone"`
	Status           string     `json:"status"`
	Currency         string     `json:"currency"`
	CreditLimitMinor int64      `json:"credit_limit_minor"`
	PaymentTermsDays int        `json:"payment_terms_days"`
	Policy           Policy     `json:"policy"`
	ReviewNote       string     `json:"review_note"`
	ReviewedAt       *time.Time `json:"reviewed_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

// Policy is what the company pays for. Sunday is weekday 0.
type Policy struct {
	Weekdays     []int    `json:"weekdays"`
	Slots        []string `json:"slots"`
	MealCapMinor int64    `json:"meal_cap_minor"`
}

type Member struct {
	ID       uuid.UUID  `json:"id"`
	Phone    string     `json:"phone"`
	Name     string     `json:"name"`
	Role     string     `json:"role"`
	Joined   bool       `json:"joined"`
	JoinedAt *time.Time `json:"joined_at"`
	// Active office lunches this employee has.
	Lunches int `json:"lunches"`
}

type Office struct {
	ID       uuid.UUID `json:"id"`
	Label    string    `json:"label"`
	Building string    `json:"building"`
	Area     string    `json:"area"`
	City     string    `json:"city"`
	Lat      float64   `json:"lat"`
	Lng      float64   `json:"lng"`
}

type Statement struct {
	ID            uuid.UUID  `json:"id"`
	StatementNo   string     `json:"statement_no"`
	PeriodEnd     string     `json:"period_end"`
	TotalMinor    int64      `json:"total_minor"`
	Currency      string     `json:"currency"`
	Status        string     `json:"status"`
	IssuedAt      time.Time  `json:"issued_at"`
	DueOn         string     `json:"due_on"`
	PaidAt        *time.Time `json:"paid_at"`
	PaidReference string     `json:"paid_reference"`
	Lines         []Charge   `json:"lines,omitempty"`
}

type Charge struct {
	ID             uuid.UUID `json:"id"`
	Kind           string    `json:"kind"`
	SubscriptionID uuid.UUID `json:"subscription_id"`
	SubscriptionNo string    `json:"subscription_no"`
	Employee       string    `json:"employee"`
	Kitchen        string    `json:"kitchen"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	CreatedAt      time.Time `json:"created_at"`
}

type Lunch struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	SubscriptionNo string    `json:"subscription_no"`
	Employee       string    `json:"employee"`
	Phone          string    `json:"phone"`
	Kitchen        string    `json:"kitchen"`
	Plan           string    `json:"plan"`
	Status         string    `json:"status"`
	StartDate      string    `json:"start_date"`
}

// Credit is the company's position against its limit.
type Credit struct {
	LimitMinor        int64 `json:"limit_minor"`
	UnbilledMinor     int64 `json:"unbilled_minor"`
	OutstandingMinor  int64 `json:"outstanding_minor"`
	AvailableMinor    int64 `json:"available_minor"`
	MonthSpendMinor   int64 `json:"month_spend_minor"`
	OverdueStatements int   `json:"overdue_statements"`
}

type Dashboard struct {
	Company    Company     `json:"company"`
	Role       string      `json:"role"`
	Credit     Credit      `json:"credit"`
	Members    []Member    `json:"members"`
	Offices    []Office    `json:"offices"`
	Lunches    []Lunch     `json:"lunches"`
	Statements []Statement `json:"statements"`
	Unbilled   []Charge    `json:"unbilled"`
}

// Benefit is what an employee sees at checkout.
type Benefit struct {
	CompanyID uuid.UUID `json:"company_id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	Policy    Policy    `json:"policy"`
	Offices   []Office  `json:"offices"`
	Currency  string    `json:"currency"`
}

type RegisterInput struct {
	Name         string      `json:"name"`
	LegalName    string      `json:"legal_name"`
	TRN          string      `json:"trn"`
	BillingEmail string      `json:"billing_email"`
	BillingPhone string      `json:"billing_phone"`
	Office       OfficeInput `json:"office"`
}

type OfficeInput struct {
	Label    string  `json:"label"`
	Building string  `json:"building"`
	Area     string  `json:"area"`
	City     string  `json:"city"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

type ReviewInput struct {
	Status           string `json:"status"`
	CreditLimitMinor *int64 `json:"credit_limit_minor"`
	PaymentTermsDays *int   `json:"payment_terms_days"`
	Note             string `json:"note"`
}

var validSlots = []string{"breakfast", "lunch", "dinner", "snack"}

func invalid(field, code, msg string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: code, Message: msg}}}
}

// NormalizePhone keeps a leading + and digits, matching how sign-in stores
// phone numbers.
func NormalizePhone(raw string) string {
	var b strings.Builder
	for i, r := range strings.TrimSpace(raw) {
		if r == '+' && i == 0 {
			b.WriteRune(r)
		} else if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (in RegisterInput) validate() error {
	var f []httpx.FieldError
	if strings.TrimSpace(in.Name) == "" {
		f = append(f, httpx.FieldError{Field: "name", Code: "required", Message: "company name is required"})
	}
	if strings.TrimSpace(in.LegalName) == "" {
		f = append(f, httpx.FieldError{Field: "legal_name", Code: "required", Message: "legal name is required"})
	}
	if e := strings.TrimSpace(in.BillingEmail); !strings.Contains(e, "@") || strings.ContainsAny(e, " ") {
		f = append(f, httpx.FieldError{Field: "billing_email", Code: "invalid", Message: "a billing email is required"})
	}
	if t := strings.TrimSpace(in.TRN); t != "" && (len(t) != 15 || strings.Trim(t, "0123456789") != "") {
		f = append(f, httpx.FieldError{Field: "trn", Code: "invalid", Message: "a TRN is 15 digits"})
	}
	if err := in.Office.validate(); err != nil {
		var ve *httpx.ValidationError
		if v, ok := err.(*httpx.ValidationError); ok {
			ve = v
			f = append(f, ve.Fields...)
		}
	}
	if len(f) > 0 {
		return &httpx.ValidationError{Fields: f}
	}
	return nil
}

func (o OfficeInput) validate() error {
	var f []httpx.FieldError
	if strings.TrimSpace(o.Label) == "" {
		f = append(f, httpx.FieldError{Field: "office.label", Code: "required", Message: "office name is required"})
	}
	if strings.TrimSpace(o.City) == "" {
		f = append(f, httpx.FieldError{Field: "office.city", Code: "required", Message: "city is required"})
	}
	if o.Lat < -90 || o.Lat > 90 || o.Lng < -180 || o.Lng > 180 || (o.Lat == 0 && o.Lng == 0) {
		f = append(f, httpx.FieldError{Field: "office.location", Code: "invalid", Message: "pick the office location"})
	}
	if len(f) > 0 {
		return &httpx.ValidationError{Fields: f}
	}
	return nil
}

func (p Policy) validate() error {
	if len(p.Weekdays) == 0 {
		return invalid("weekdays", "required", "choose at least one covered day")
	}
	for _, d := range p.Weekdays {
		if d < 0 || d > 6 {
			return invalid("weekdays", "invalid", "weekdays are 0 (Sunday) to 6")
		}
	}
	if len(p.Slots) == 0 {
		return invalid("slots", "required", "choose at least one covered meal")
	}
	for _, s := range p.Slots {
		if !slices.Contains(validSlots, s) {
			return invalid("slots", "invalid", "unknown meal "+s)
		}
	}
	if p.MealCapMinor <= 0 {
		return invalid("meal_cap_minor", "invalid", "the per-meal cap must be more than zero")
	}
	return nil
}

func (p Policy) covers(weekday int, slot string) bool {
	return slices.Contains(p.Weekdays, weekday) && slices.Contains(p.Slots, slot)
}

func (c Credit) withLimit(limit int64) Credit {
	c.LimitMinor = limit
	c.AvailableMinor = max(limit-c.UnbilledMinor-c.OutstandingMinor, 0)
	return c
}
