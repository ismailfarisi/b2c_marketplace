package httpx

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
)

// KeysetCursor is an opaque "show older" cursor for lists ordered by a
// timestamp, newest first, with the row id as the tie-break.
type KeysetCursor struct {
	At time.Time
	ID uuid.UUID
}

func (c KeysetCursor) Encode() string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.At.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()))
}

// DecodeKeysetCursor parses a cursor from a query parameter. An empty string
// is the first page (nil, nil); anything malformed is a 422 on `cursor`.
func DecodeKeysetCursor(s string) (*KeysetCursor, error) {
	if s == "" {
		return nil, nil
	}
	bad := &ValidationError{Fields: []FieldError{{Field: "cursor", Code: "invalid", Message: "not a cursor from this list"}}}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, bad
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, bad
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, bad
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, bad
	}
	return &KeysetCursor{At: t, ID: u}, nil
}
