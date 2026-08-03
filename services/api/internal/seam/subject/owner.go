package subject

import (
	"context"

	"github.com/google/uuid"
)

// Owner answers "does this person own this thing".
//
// # Why the seam has to answer it at all
//
// Because something outside every vertical needs to know. Trust documents are
// the case that forced it: a mulkiya is attached to a car and a Trakheesi
// permit to a property advert, and the endpoint that accepts one must refuse to
// attach it to somebody else's subject — which for property means refusing to
// let a stranger push an advert live under a permit they supplied.
//
// The handler cannot check that itself without importing vehicles and property
// and growing a `switch subjectType`, which is the one thing this whole package
// exists to prevent. So the vertical answers, as it does for everything else
// about its own rows.
//
// # Why it is not part of Provider
//
// Same reason [Gate] is not. Provider is about rendering a subject to a viewer
// and every vertical needs all of it; this is about authorship, and a vertical
// with nothing a seller can attach to it has no reason to implement it.
//
// A type with no Owner answers false rather than true — see [Registry.OwnedBy]
// — which is the opposite default to Gate's, and deliberately so: a missing
// gate should not stop the feed, and a missing ownership check should not grant
// access.
type Owner interface {
	// OwnedBy reports whether userID is the author of subjectID.
	//
	// "Author" rather than "owner" in spirit: it is the person entitled to
	// attach things to it and change it, which for a dealership listing is the
	// account that created it rather than whoever the car belongs to.
	OwnedBy(ctx context.Context, subjectID, userID uuid.UUID) (bool, error)
}

// RegisterOwner attaches an ownership check to an already-registered type.
//
// Panics on a duplicate or an unknown type, matching Register, RegisterComposer
// and RegisterGate. An ownership check silently not applied is an authorisation
// hole, which is the strongest case in this file for failing at boot.
func (r *Registry) RegisterOwner(t Type, o Owner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[t]; !ok {
		panic("subject: owner check for " + string(t) + " registered before its provider")
	}
	if r.owners == nil {
		r.owners = make(map[Type]Owner)
	}
	if _, exists := r.owners[t]; exists {
		panic("subject: duplicate owner check for " + string(t))
	}
	r.owners[t] = o
}

// OwnedBy asks the type's owner check.
//
// Answers **false** for an unknown type, a nil registry, or a type with no
// check registered. That is the opposite default to [Registry.Ready] and the
// asymmetry is the point: being permissive about a publish gate costs a reel
// shown early, and being permissive here costs somebody else's listing. A
// vertical that wants documents attached to it registers a check; one that does
// not, refuses them.
func (r *Registry) OwnedBy(ctx context.Context, t Type, subjectID, userID uuid.UUID) (bool, error) {
	if r == nil || t == "" || subjectID == uuid.Nil || userID == uuid.Nil {
		return false, nil
	}
	r.mu.RLock()
	o, ok := r.owners[t]
	r.mu.RUnlock()
	if !ok {
		return false, nil
	}
	return o.OwnedBy(ctx, subjectID, userID)
}
