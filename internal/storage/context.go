package storage

import (
	"context"
	"encoding/json"
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

// RealActorContextKey is the context key carrying the real actor user id when
// a request is impersonated. Set it via WithRealActor (or huma.WithValue in
// middleware); the audit writer records it as the row's real_actor_user_id.
var RealActorContextKey = realActorKey{}

// WithRealActor returns a context carrying the real actor user id behind an
// impersonated request. An empty id leaves the audit real_actor null.
func WithRealActor(ctx context.Context, realActorID string) context.Context {
	return context.WithValue(ctx, RealActorContextKey, realActorID)
}

// realActorFrom reads the real actor user id from the context, empty if none.
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
	// AuditChannelOmnibot marks an action omnibot, the built-in assistant,
	// took (#1166): a tool call on the MCP server made with an agent token
	// minted for the omnibot channel alone. The server stamps it from the
	// credential, never from the name a client declares.
	AuditChannelOmnibot AuditChannel = "omnibot"
)

// AuditChannels is every channel audit_log.channel can hold, in the check's
// order; the API's channel filter offers these plus AuditChannelDirect.
var AuditChannels = []AuditChannel{AuditChannelMCP, AuditChannelOmnibot}

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

// ReplaceAuditChannel returns a context whose audited writes record channel
// in place of the one ctx carries, keeping the declared client. The API sets
// it when a request's credential settles its channel (an omnibot token).
func ReplaceAuditChannel(ctx context.Context, channel AuditChannel) context.Context {
	v, _ := ctx.Value(auditChannelKey{}).(auditChannelValue)
	v.channel = channel
	return context.WithValue(ctx, auditChannelKey{}, v)
}

// AuditChannelOf returns the channel the context's request arrived through,
// empty for a direct API call. The API reads it to apply the agent ceiling
// (#1179) to every request an MCP tool call makes, whatever its credential.
func AuditChannelOf(ctx context.Context) AuditChannel {
	v, _ := ctx.Value(auditChannelKey{}).(auditChannelValue)
	return v.channel
}

// agentKey carries the agent an agent-token request acts as (#1179).
type agentKey struct{}

// WithAgent returns a context whose audited writes record that the named agent
// took them, on the actor's behalf. Set by the API's authn when the request
// authenticated with an agent token; empty leaves the column null.
func WithAgent(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, agentKey{}, name)
}

// agentFrom reads the agent for an audit row: a SQL null when there is none.
func agentFrom(ctx context.Context) any {
	name, _ := ctx.Value(agentKey{}).(string)
	return nullize(name)
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

// templateKey carries the template a planned create came from (#827).
type templateKey struct{}

// WithTemplate marks every audit row written under ctx with the template the
// rows came from: provenance lives in the audit detail and nowhere else (no
// column, no back-pointer, ADR-0071), so this is the one trace a row
// started from a template.
func WithTemplate(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, templateKey{}, name)
}

func templateFrom(ctx context.Context) string {
	name, _ := ctx.Value(templateKey{}).(string)
	return name
}

// templateDetailKey is the audit image key the plan's template lands under:
// the one provenance key every row created from a template carries. A row
// whose image already names its own template under it (a standard created
// from a standard template inside a system's plan) keeps its own.
const templateDetailKey = "from_template"

// withTemplateDetail adds the template to an audit image (a JSON object), so
// the detail names where the row came from. A nil image, or one that is not
// an object, is left as it is.
func withTemplateDetail(ctx context.Context, image any) any {
	name := templateFrom(ctx)
	b, ok := image.([]byte)
	if name == "" || !ok || len(b) < 2 || b[0] != '{' {
		return image
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return image
	}
	if _, has := m[templateDetailKey]; has {
		return image
	}
	m[templateDetailKey], _ = json.Marshal(name)
	out, err := json.Marshal(m)
	if err != nil {
		return image
	}
	return out
}
