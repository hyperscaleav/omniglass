package storage

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

// realActorKey is the context key under which the API sets the real actor behind
// an impersonated request. It is exported (as RealActorContextKey) because the
// API's authn middleware sets it while the gateway's audit writer reads it, both
// on the request context; the real actor is a request-scoped ambient identity
// (like a trace id), so it rides the context rather than threading a parameter
// through every mutating gateway method.
type realActorKey struct{}

// RealActorContextKey is the context key carrying the real actor principal id when
// a request is impersonated. Set it via WithRealActor (or huma.WithValue in
// middleware); the audit writer records it as the row's real_actor_principal_id.
var RealActorContextKey = realActorKey{}

// WithRealActor returns a context carrying the real actor principal id behind an
// impersonated request. An empty id leaves the audit real_actor null.
func WithRealActor(ctx context.Context, realActorID string) context.Context {
	return context.WithValue(ctx, RealActorContextKey, realActorID)
}

// realActorFrom reads the real actor principal id from the context, empty if none.
func realActorFrom(ctx context.Context) string {
	id, _ := ctx.Value(RealActorContextKey).(string)
	return id
}

// AuditChannel names the channel an audited action arrived through (#1162,
// ADR-1162). A direct API call has none; an MCP tool call is AuditChannelMCP.
// The values are the audit_log.channel check's, so a channel the server does
// not know cannot be written.
type AuditChannel string

const (
	// AuditChannelMCP marks an action taken through a tool call on the MCP
	// server: the same route, gate and scope as a direct call, arrived by MCP.
	AuditChannelMCP AuditChannel = "mcp"
)

// auditChannelKey is the context key under which the MCP dispatch sets the
// channel and the declaring client. Unexported, with WithAuditChannel the only
// way to set it: no HTTP header or body reaches it, so a direct API caller
// cannot claim a channel. It rides the context like the real actor, read by
// the audit writers rather than threaded through every gateway method.
type auditChannelKey struct{}

type auditChannelValue struct {
	channel AuditChannel
	client  string
}

// WithAuditChannel returns a context whose audited writes record channel and
// the client the caller declared (sanitized, see SanitizeAuditClient). Set by
// the MCP dispatch on the request it runs a tool through.
func WithAuditChannel(ctx context.Context, channel AuditChannel, client string) context.Context {
	return context.WithValue(ctx, auditChannelKey{}, auditChannelValue{channel: channel, client: SanitizeAuditClient(client)})
}

// auditChannelFrom reads the channel and client for an audit row: SQL nulls
// when the context carries none (a direct API call).
func auditChannelFrom(ctx context.Context) (channel, client any) {
	v, ok := ctx.Value(auditChannelKey{}).(auditChannelValue)
	if !ok || v.channel == "" {
		return nil, nil
	}
	return string(v.channel), nullize(v.client)
}

// auditClientMax bounds a declared client name, matching the column's check.
const auditClientMax = 200

// SanitizeAuditClient holds a client's self-declared name to a printable,
// bounded string: control characters (a newline, an escape) are dropped so a
// hostile declaration cannot forge a log line or a terminal sequence, the
// result is trimmed, and it is cut at 200 characters.
func SanitizeAuditClient(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || !unicode.IsPrint(r) && r != ' ' {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > auditClientMax {
		out = string([]rune(out)[:auditClientMax])
	}
	return out
}
