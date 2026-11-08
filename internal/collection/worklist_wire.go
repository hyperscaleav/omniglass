package collection

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/nats-io/nats.go"
)

// The worklist pull's headers (#1577, ADR-1577). The request carries the
// node's budget; the reply carries how its body is encoded, or the reason the
// server could not build it.
const (
	// WorklistTimeoutHeader is the node's budget for one pull, a Go duration
	// ("5s"). It is a duration, not a deadline, so the node's clock and the
	// server's need not agree: the server's deadline is the request's arrival
	// plus the budget, the way gRPC's grpc-timeout header works.
	WorklistTimeoutHeader = "Omniglass-Timeout"
	// WorklistErrorHeader names why the server sent no worklist. A reply that
	// carries it has no body, and the node keeps the worklist it has.
	WorklistErrorHeader = "Omniglass-Error"
	// ContentEncodingHeader is "gzip" on a compressed reply.
	ContentEncodingHeader = "Content-Encoding"
)

// WorklistCompressAbove is the encoded size past which a worklist reply is
// gzipped: under it a reply is small enough that compressing costs more than
// it saves.
const WorklistCompressAbove = 64 * 1024

// worklistInflateLimit bounds what one compressed reply may inflate to, so a
// corrupt body cannot exhaust the node's memory.
const worklistInflateLimit = 256 << 20

// EncodeWorklistBody is the reply body for an encoded worklist, gzipped when
// it is larger than WorklistCompressAbove, with the header that says so (nil
// when it is sent as is).
func EncodeWorklistBody(raw []byte) ([]byte, nats.Header, error) {
	if len(raw) <= WorklistCompressAbove {
		return raw, nil, nil
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, nil, fmt.Errorf("collection: gzip worklist: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, nil, fmt.Errorf("collection: gzip worklist: %w", err)
	}
	return buf.Bytes(), nats.Header{ContentEncodingHeader: []string{"gzip"}}, nil
}

// WorklistErrorReply is the header of a reply that carries no worklist, only
// the reason.
func WorklistErrorReply(reason string) nats.Header {
	return nats.Header{WorklistErrorHeader: []string{reason}}
}

// ErrWorklistRefused wraps the server's reason for sending no worklist.
var ErrWorklistRefused = errors.New("the server sent no worklist")

// DecodeWorklistReply reads a worklist reply: the server's refusal as an
// error wrapping ErrWorklistRefused, else the body, inflated when its header
// says gzip.
func DecodeWorklistReply(hdr nats.Header, data []byte) (WorklistReply, error) {
	if reason := hdr.Get(WorklistErrorHeader); reason != "" {
		return WorklistReply{}, fmt.Errorf("%w: %s", ErrWorklistRefused, reason)
	}
	switch enc := hdr.Get(ContentEncodingHeader); enc {
	case "":
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return WorklistReply{}, fmt.Errorf("collection: inflate worklist: %w", err)
		}
		raw, err := io.ReadAll(io.LimitReader(zr, worklistInflateLimit+1))
		if err != nil {
			return WorklistReply{}, fmt.Errorf("collection: inflate worklist: %w", err)
		}
		if len(raw) > worklistInflateLimit {
			return WorklistReply{}, fmt.Errorf("collection: inflate worklist: over %d bytes", worklistInflateLimit)
		}
		data = raw
	default:
		return WorklistReply{}, fmt.Errorf("collection: worklist encoding %q is not gzip", enc)
	}
	var reply WorklistReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return WorklistReply{}, fmt.Errorf("collection: decode worklist: %w", err)
	}
	return reply, nil
}

// WorklistRequestTimeout is the budget a pull's header states, and false when
// it states none or one that does not parse as a positive duration.
func WorklistRequestTimeout(hdr nats.Header) (time.Duration, bool) {
	v := hdr.Get(WorklistTimeoutHeader)
	if v == "" {
		return 0, false
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}
