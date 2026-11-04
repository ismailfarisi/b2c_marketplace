package httpx

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestKeysetCursorRoundTrip(t *testing.T) {
	in := KeysetCursor{At: time.Date(2026, 11, 4, 21, 30, 0, 123456000, time.UTC), ID: uuid.New()}
	out, err := DecodeKeysetCursor(in.Encode())
	if err != nil || out == nil || !out.At.Equal(in.At) || out.ID != in.ID {
		t.Fatalf("round trip: %+v %v", out, err)
	}
	if c, err := DecodeKeysetCursor(""); c != nil || err != nil {
		t.Fatalf("empty cursor: %+v %v", c, err)
	}
	for _, s := range []string{"nope", "bm9waXBl", KeysetCursor{}.Encode()[:5]} {
		if _, err := DecodeKeysetCursor(s); err == nil {
			t.Fatalf("%q accepted", s)
		}
	}
}
