package views

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The page cursor: an opaque token carrying the keyset of the last row a page
// returned. Keyset, not offset, so a row written between two requests cannot
// shift a page boundary and hide its neighbour. Opaque by encoding, not by
// secrecy: it holds no data the caller could not already see, and it is
// validated on the way back in, so a tampered token is a clean 400 rather than
// a query built from a caller's string.
type cursor struct {
	TS time.Time
	ID int64
}

// encodeCursor renders a keyset as the page token a client hands back.
func encodeCursor(c cursor) string {
	raw := strconv.FormatInt(c.TS.UTC().UnixNano(), 10) + ":" + strconv.FormatInt(c.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a page token. Any malformed token is ErrInvalidParam, so
// the route answers 400 rather than silently starting from the top, which would
// walk the caller back through rows they already paged past.
func decodeCursor(token string) (cursor, error) {
	if token == "" {
		return cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cursor{}, fmt.Errorf("%w: page_token is not a valid cursor", ErrInvalidParam)
	}
	nanos, id, ok := strings.Cut(string(raw), ":")
	if !ok {
		return cursor{}, fmt.Errorf("%w: page_token is not a valid cursor", ErrInvalidParam)
	}
	n, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil {
		return cursor{}, fmt.Errorf("%w: page_token is not a valid cursor", ErrInvalidParam)
	}
	i, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return cursor{}, fmt.Errorf("%w: page_token is not a valid cursor", ErrInvalidParam)
	}
	return cursor{TS: time.Unix(0, n).UTC(), ID: i}, nil
}
