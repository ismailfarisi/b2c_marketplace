// Package fulfillment defines the downstream seam: the boundary between what a
// vertical *is* and the physical work it produces.
//
// A Fulfillment is the atomic unit of physical work — someone goes somewhere at
// a time to do a thing. Logistics, dispatch, routing and the driver app operate
// on this shape and this shape only. None of them ever learns what a laundry
// bag or a meal box is.
//
// A laundry order produces two fulfillments (pickup, delivery). A meal
// subscription produces one per delivery day. A home visit produces one. The
// engine downstream cannot tell them apart, which is exactly the point.
//
// Health metric: if adding a vertical requires editing anything in the
// logistics module, this seam has leaked.
package fulfillment

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Kind string

const (
	KindPickup   Kind = "pickup"
	KindDelivery Kind = "delivery"
	KindVisit    Kind = "visit"
)

type Status string

const (
	StatusScheduled  Status = "scheduled"
	StatusDispatched Status = "dispatched"
	StatusEnRoute    Status = "en_route"
	StatusArrived    Status = "arrived"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

// Fulfillment is the full seam contract. Everything downstream reads these
// fields; PayloadRef is deliberately opaque — an identifier the owning vertical
// can resolve, and that logistics passes through without interpreting.
type Fulfillment struct {
	ID       uuid.UUID
	OrderID  uuid.UUID
	BranchID uuid.UUID

	Kind     Kind
	Status   Status
	Sequence int // 1-based ordering within the parent order

	ScheduledDate time.Time // date only, in the branch timezone
	WindowID      uuid.UUID

	// Address is snapshotted, not referenced. A customer editing their saved
	// address must not silently redirect a driver already en route.
	AddressSnapshot Address

	PayloadRef string
}

type Address struct {
	Line      string
	Landmark  string
	Area      string
	City      string
	Emirate   string
	Country   string
	Lat, Lng  float64
	Notes     string
	Recipient string
	Phone     string
}

// Request is what a vertical asks for. The count and kinds are decided by the
// service type's Pattern, not by the caller.
type Request struct {
	Kind          Kind
	Sequence      int
	ScheduledDate time.Time
	WindowID      uuid.UUID
	// ResourceKey names the capacity pool this fulfillment consumes, e.g.
	// "pickup_slot" or "kitchen_covers". Two verticals sharing a branch can
	// contend for the same pool or not, and that is a data decision.
	ResourceKey string
	PayloadRef  string
}

// Pattern is the per-vertical strategy that turns an order into fulfillment
// requests. This is one of the four strategies bound by a `service_types` row.
type Pattern interface {
	// Key matches `service_types.fulfillment_pattern`.
	Key() string
	// Plan expands an order into the fulfillments it requires. Returning two
	// requests for laundry and one for a single visit is the entire difference
	// between those verticals as far as logistics is concerned.
	Plan(ctx context.Context, order OrderContext) ([]Request, error)
}

// OrderContext is the slice of an order a Pattern is allowed to see. Keeping it
// narrow is what stops patterns growing into a second order service.
type OrderContext struct {
	OrderID     uuid.UUID
	BranchID    uuid.UUID
	ServiceType string
	Slots       []Slot
	Options     map[string]any
}

type Slot struct {
	Kind     Kind
	Date     time.Time
	WindowID uuid.UUID
}
