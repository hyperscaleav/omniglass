package views

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// TestCursorRoundTrip proves a keyset survives the token unchanged, to the
// nanosecond: a lossy round trip would re-serve or skip the boundary row on
// every page turn.
func TestCursorRoundTrip(t *testing.T) {
	want := cursor{TS: time.Date(2026, 8, 3, 12, 0, 0, 123456789, time.UTC), ID: 4242}
	got, err := decodeCursor(encodeCursor(want))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.TS.Equal(want.TS) || got.ID != want.ID {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// TestCursorEmpty proves an absent token is the start of the feed, not an
// error: the first page carries no cursor.
func TestCursorEmpty(t *testing.T) {
	got, err := decodeCursor("")
	if err != nil {
		t.Fatalf("empty token: %v", err)
	}
	if !got.TS.IsZero() || got.ID != 0 {
		t.Errorf("empty token = %+v, want the zero cursor", got)
	}
}

// TestCursorRejectsMalformed proves every malformed token is ErrInvalidParam
// (a clean 400), never a silent restart from the newest row, which would walk
// the caller back through rows they already paged past.
func TestCursorRejectsMalformed(t *testing.T) {
	for _, tok := range []string{
		"not base64 !!",
		encodeToken("no-colon"),
		encodeToken("abc:1"),
		encodeToken("1:xyz"),
		encodeToken(":"),
	} {
		if _, err := decodeCursor(tok); !errors.Is(err, ErrInvalidParam) {
			t.Errorf("decodeCursor(%q) = %v, want ErrInvalidParam", tok, err)
		}
	}
}

// TestCursorIsOpaque proves the token is not the raw keyset: a client cannot
// hand-build one from a timestamp it saw, so the format stays ours to change.
func TestCursorIsOpaque(t *testing.T) {
	tok := encodeCursor(cursor{TS: time.Unix(0, 1), ID: 2})
	if tok == "1:2" {
		t.Errorf("cursor is the plain keyset: %q", tok)
	}
}

// encodeToken base64s a raw cursor payload, so a test can build a
// well-encoded but ill-formed token.
func encodeToken(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}
