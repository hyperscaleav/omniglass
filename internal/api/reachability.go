package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The reachability read BFF: a per-component, per-endpoint composition of the
// verdict property (endpoint-reachable), the layer signals (the raw icmp/tcp probe
// metrics), and the recent transition history the availability strip reads. It is
// a plain typed Huma GET (no ViewResult framework exists), gated by component:read
// and scope-injected through GetComponent: an out-of-scope component is a
// non-disclosing 404, so the sample reads below only ever run on a verified,
// in-scope component. Read-only: no engine, bus, or write-path is touched.

// reachHistoryWindow bounds the transition history returned for the strip.
const reachHistoryWindow = 24 * time.Hour

// verdictKey is the property sample that carries the per-endpoint reach
// verdict; responsiveKey and authKey are the layer-7 rungs beside it (#812).
// sessionStateKey and sessionReasonKey are a held session's state and why
// (ADR-0150, ADR-0155), and rejectionKey the gate a terminal refusal holds
// (ADR-0158); the console's session pill and ladder read them (#1059).
const (
	verdictKey       = "endpoint-reachable"
	responsiveKey    = "endpoint-responsive"
	authKey          = "endpoint-authenticated"
	sessionStateKey  = "session-state"
	sessionReasonKey = "session-reason"
	rejectionKey     = "endpoint-rejection"
)

// reachLayer describes one probe layer the panel gates on: its primary signal
// (0/1) and the optional companion metric that carries a timing detail.
type reachLayer struct {
	Layer     string // the layer word: "ping" (L3) or "port" (L4)
	SignalKey string // the primary metric key (icmp-reachable / tcp-open)
	TimingKey string // the companion timing metric (icmp-rtt-avg / tcp-connect-time)
}

// reachLayers is the fixed probe-layer order, L3 then L4.
var reachLayers = []reachLayer{
	{Layer: "ping", SignalKey: "icmp-reachable", TimingKey: "icmp-rtt-avg"},
	{Layer: "port", SignalKey: "tcp-open", TimingKey: "tcp-connect-time"},
}

type reachVerdictBody struct {
	Value string    `json:"value" doc:"The latest stored verdict value (up/down)"`
	TS    time.Time `json:"ts" doc:"When the verdict last changed: one is stored only on a transition, so an endpoint's freshness is last_seen, not this"`
}

type reachLayerBody struct {
	Layer  string    `json:"layer" doc:"The probe layer word (ping, port)"`
	Check  string    `json:"check" doc:"The property_type key of the primary signal"`
	Value  float64   `json:"value" doc:"The latest signal value (1 = reachable/open, 0 = not)"`
	Detail string    `json:"detail,omitempty" doc:"A human timing detail (rtt / connect time), when present"`
	TS     time.Time `json:"ts" doc:"When the signal was observed"`
}

type reachHistoryBody struct {
	TS    time.Time `json:"ts"`
	Value string    `json:"value"`
}

// reachSessionBody is an endpoint's held session (#1059): the latest
// session-state, when it changed, and the session-reason beside it.
type reachSessionBody struct {
	State  string    `json:"state" enum:"establishing,up,recovering,down,rejected" doc:"The held session's latest state (session-state): establishing, up, recovering, down, or rejected (a refusal retrying cannot fix; the node dials no more until the endpoint's connection fingerprint moves)"`
	TS     time.Time `json:"ts" doc:"When the session last changed state: a state is stored only on a transition, so a session is judged by the endpoint's last_seen, not by this"`
	Reason string    `json:"reason,omitempty" doc:"Why the session is in its state (session-reason), the reason of its latest transition; a rejected session's is its class and the component's own text (auth: auth rejected), scrubbed by the node of anything it sent. Cut to 500 characters; the property's series keeps the whole"`
}

// reachRejectionBody is the gate a terminal refusal holds (ADR-0158): the
// rung and the class of endpoint-rejection, never its fingerprint.
type reachRejectionBody struct {
	Rung  string `json:"rung" enum:"reachable,responsive,authenticated" doc:"The ladder rung the refusal holds: responsive for a host key, certificate or protocol refusal, authenticated for a credential. Nothing at or above it is attempted until the connection fingerprint moves"`
	Class string `json:"class" doc:"The refusal's class: auth, host-key, certificate or protocol"`
	// Pinned and Offered are a changed key's two keys (#1035), carried on
	// the gate as data so no surface parses them out of the reason.
	Pinned  string `json:"pinned,omitempty" doc:"For a refusal of a changed key (class certificate on a tls endpoint in pin mode): the public key the endpoint pins, a SHA256 fingerprint. Absent on every other refusal"`
	Offered string `json:"offered,omitempty" doc:"For a refusal of a changed key: the public key the component now offers, a SHA256 fingerprint. Trusting it is POST /endpoints/{id}:retry with certificate set to this value"`
}

// reachTLSBody is a tls endpoint's handshake and certificate (#1035): how it
// verifies, the key it pins, the latest handshake time, and the certificate's
// expiry and days left.
type reachTLSBody struct {
	Mode        string     `json:"mode" doc:"How the endpoint's connections judge the certificate (its driver's tls_mode input, verify when none is set): verify (against the node's system roots, the server name the host), pin (the public key trusted on first use, any other refused) or skip (nothing checked, the operator's explicit choice)"`
	Key         string     `json:"key,omitempty" doc:"The public key the endpoint pins (tls-certificate), a SHA256 fingerprint; absent before first use and in verify or skip mode"`
	HandshakeMS *float64   `json:"handshake_ms,omitempty" doc:"The latest probe's handshake time in milliseconds (tls-handshake-time); absent until the component answers one"`
	Expires     *time.Time `json:"expires,omitempty" doc:"The not-after of the certificate the latest probe accepted (tls-certificate-expiry); absent until one is accepted"`
	DaysLeft    *float64   `json:"days_left,omitempty" doc:"Days from the latest probe to that not-after (tls-certificate-days-left), negative once expired: the number a threshold rule reads"`
}

type reachEndpointBody struct {
	ID            string              `json:"id" doc:"The endpoint's uuid, the address its custom methods take (POST /endpoints/{id}:retry)"`
	Endpoint      string              `json:"endpoint" doc:"The endpoint's derived name (its transport)"`
	Label         string              `json:"label,omitempty" doc:"The friendly string an operator reads; absent when unset, and the row then reads the derived name verbatim"`
	Transport     string              `json:"transport" doc:"The transport (icmp, tcp, ...)"`
	Address       string              `json:"address,omitempty" doc:"The probed address (target[:port]) from the endpoint params"`
	Node          string              `json:"node,omitempty" doc:"The node that probes this endpoint"`
	LastSeen      *time.Time          `json:"last_seen,omitempty" doc:"When the endpoint last delivered a batch, by the server's clock and to within a few seconds, whatever it carried (any lane, any series, an unchanged value); absent until one lands"`
	Verdict       *reachVerdictBody   `json:"verdict" doc:"The latest reachability verdict, or null if none yet"`
	Responsive    *reachVerdictBody   `json:"responsive,omitempty" doc:"The layer-7 responds rung (the protocol itself answered), when a probe has climbed it"`
	Authenticated *reachVerdictBody   `json:"authenticated,omitempty" doc:"The auth rung (the probe's credential was accepted, or a held session's login passed), present only when a credential was tried"`
	Session       *reachSessionBody   `json:"session,omitempty" doc:"The endpoint's held session, while it is configured to hold one (a standing listen task, or a driver declaring a session): its latest state, when it changed, and why"`
	Rejection     *reachRejectionBody `json:"rejection,omitempty" doc:"The gate a terminal refusal holds on the endpoint, its rung and class; absent when none holds"`
	TLS           *reachTLSBody       `json:"tls,omitempty" doc:"A tls endpoint's handshake and certificate: its verification mode, pinned key, handshake time, expiry and days left; absent on every other transport"`
	Layers        []reachLayerBody    `json:"layers" doc:"The per-layer probe signals that compose the verdict"`
	History       []reachHistoryBody  `json:"history" doc:"The recent verdict transitions, oldest first, for the availability strip"`
}

type reachabilityOutput struct {
	Body struct {
		Component string              `json:"component"`
		Endpoints []reachEndpointBody `json:"endpoints"`
	}
}

// registerReachabilityRoutes wires the per-component reachability read, the
// operator-facing surface over the collection verdict data.
func registerReachabilityRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-component-reachability",
		Method:      http.MethodGet,
		Path:        "/components/{name}/reachability",
		Summary:     "Read a component's per-endpoint reachability",
		Description: "Composes, per endpoint, the latest reachability verdict, the probe-layer signals that compose it, and the recent verdict transitions for the availability strip. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentPathInput) (*reachabilityOutput, error) {
		comp, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		ifaces, err := gw.ListComponentEndpoints(ctx, comp.ID)
		if err != nil {
			if refErr, ok := mapRefErr(err); ok {
				return nil, refErr
			}
			return nil, huma.Error500InternalServerError("read reachability")
		}
		out := &reachabilityOutput{}
		out.Body.Component = comp.Name
		out.Body.Endpoints = make([]reachEndpointBody, 0, len(ifaces))
		for i := range ifaces {
			row, err := composeEndpoint(ctx, gw, comp.ID, ifaces[i], reachHistoryWindow)
			if err != nil {
				// mapRefErr first: comp.ID is always a uuid (GetComponent
				// above already resolved it), so LatestProperty/
				// LatestMetricInstance/PropertyTransitions inside
				// composeEndpoint can never actually be ambiguous today,
				// but each runs through the same bare-name resolver every
				// other component route does, and a future caller must not
				// silently regress to a 500 (ruling 1, #627).
				if refErr, ok := mapRefErr(err); ok {
					return nil, refErr
				}
				return nil, huma.Error500InternalServerError("read reachability")
			}
			out.Body.Endpoints = append(out.Body.Endpoints, row)
		}
		return out, nil
	})
}

// composeEndpoint assembles one endpoint's reachability row from the property and
// metric sinks. All reads are keyed by the verified component name and the
// endpoint name (the sample instance).
func composeEndpoint(ctx context.Context, gw storage.Gateway, comp string, it storage.ComponentEndpoint, window time.Duration) (reachEndpointBody, error) {
	row := reachEndpointBody{
		ID:        it.ID,
		Endpoint:  it.Name,
		Label:     it.Label,
		Transport: it.Transport,
		Address:   addressFromParams(it.Params),
		Node:      it.NodeName,
		Layers:    []reachLayerBody{},
		History:   []reachHistoryBody{},
	}

	row.LastSeen = it.LastSeenAt

	verdict, err := gw.LatestProperty(ctx, comp, verdictKey, it.Name)
	if err != nil {
		return row, err
	}
	if verdict != nil {
		row.Verdict = &reachVerdictBody{Value: verdict.Value, TS: verdict.TS}
	}
	responsive, err := gw.LatestProperty(ctx, comp, responsiveKey, it.Name)
	if err != nil {
		return row, err
	}
	if responsive != nil {
		row.Responsive = &reachVerdictBody{Value: responsive.Value, TS: responsive.TS}
	}
	authed, err := gw.LatestProperty(ctx, comp, authKey, it.Name)
	if err != nil {
		return row, err
	}
	if authed != nil {
		row.Authenticated = &reachVerdictBody{Value: authed.Value, TS: authed.TS}
	}
	// A session is served only while the endpoint is configured to hold one:
	// teardown publishes no state (ADR-0150), so a session nobody holds keeps
	// its last one, and a rejected that outlived its listener would stand
	// beside a Retry that can change nothing.
	var state *storage.PropertySample
	if it.HoldsSession {
		state, err = gw.LatestProperty(ctx, comp, sessionStateKey, it.Name)
		if err != nil {
			return row, err
		}
	}
	if state != nil {
		row.Session = &reachSessionBody{State: state.Value, TS: state.TS}
		why, err := gw.LatestProperty(ctx, comp, sessionReasonKey, it.Name)
		if err != nil {
			return row, err
		}
		if why != nil {
			row.Session.Reason = capReason(why.Value)
		}
	}
	gate, err := gw.LatestProperty(ctx, comp, rejectionKey, it.Name)
	if err != nil {
		return row, err
	}
	if gate != nil {
		row.Rejection = parseRejection(gate.Value)
	}
	if it.Transport == "tls" {
		if row.TLS, err = composeTLS(ctx, gw, comp, it); err != nil {
			return row, err
		}
	}

	for _, l := range reachLayers {
		signal, err := gw.LatestMetricInstance(ctx, comp, l.SignalKey, it.Name)
		if err != nil {
			return row, err
		}
		if signal == nil {
			continue
		}
		lb := reachLayerBody{Layer: l.Layer, Check: l.SignalKey, Value: signal.Value, TS: signal.TS}
		timing, err := gw.LatestMetricInstance(ctx, comp, l.TimingKey, it.Name)
		if err != nil {
			return row, err
		}
		if timing != nil {
			lb.Detail = fmt.Sprintf("%.1f ms", timing.Value)
		}
		row.Layers = append(row.Layers, lb)
	}

	transitions, err := gw.PropertyTransitions(ctx, comp, verdictKey, it.Name, window)
	if err != nil {
		return row, err
	}
	for _, tr := range transitions {
		row.History = append(row.History, reachHistoryBody{TS: tr.TS, Value: tr.Value})
	}
	return row, nil
}

// The tls facts a probe and a session land (#1035).
const (
	tlsKeyKey       = "tls-certificate"
	tlsExpiryKey    = "tls-certificate-expiry"
	tlsDaysLeftKey  = "tls-certificate-days-left"
	tlsHandshakeKey = "tls-handshake-time"
)

// composeTLS reads a tls endpoint's facts: the mode off its params (verify
// when none is set), the pinned key, and the latest handshake, expiry and
// days left.
func composeTLS(ctx context.Context, gw storage.Gateway, comp string, it storage.ComponentEndpoint) (*reachTLSBody, error) {
	var p struct {
		Mode string `json:"tls_mode"`
	}
	_ = json.Unmarshal(it.Params, &p)
	out := &reachTLSBody{Mode: p.Mode}
	if out.Mode == "" {
		out.Mode = "verify"
	}
	key, err := gw.LatestProperty(ctx, comp, tlsKeyKey, it.Name)
	if err != nil {
		return nil, err
	}
	if key != nil {
		out.Key = key.Value
	}
	expiry, err := gw.LatestProperty(ctx, comp, tlsExpiryKey, it.Name)
	if err != nil {
		return nil, err
	}
	if expiry != nil {
		if at, err := time.Parse(time.RFC3339, expiry.Value); err == nil {
			out.Expires = &at
		}
	}
	for key, into := range map[string]**float64{tlsHandshakeKey: &out.HandshakeMS, tlsDaysLeftKey: &out.DaysLeft} {
		m, err := gw.LatestMetricInstance(ctx, comp, key, it.Name)
		if err != nil {
			return nil, err
		}
		if m != nil {
			v := m.Value
			*into = &v
		}
	}
	return out, nil
}

// maxReasonRunes bounds the session reason the read serves (#1059): a
// refusal carries the component's own text, and a line answer may run to
// 64 KiB, which the console would render on every row showing it. The full
// text stays on the property's own series.
const maxReasonRunes = 500

// capReason cuts a reason past maxReasonRunes on a rune boundary and says so.
func capReason(s string) string {
	if utf8.RuneCountInString(s) <= maxReasonRunes {
		return s
	}
	return string([]rune(s)[:maxReasonRunes]) + "…"
}

// parseRejection reads an endpoint-rejection value (`<rung> <class>
// <fingerprint>`, then for a changed key the pinned and offered keys, or
// none) into the rung and class it holds at, and the two keys. The
// fingerprint is dropped: a digest of the connection's inputs, it is the
// node's to compare and says nothing an operator acts on. Anything else (none,
// a malformed value, a rung the ladder does not have) is no gate.
func parseRejection(v string) *reachRejectionBody {
	parts := strings.Fields(v)
	if len(parts) != 3 && len(parts) != 5 {
		return nil
	}
	switch parts[0] {
	case "reachable", "responsive", "authenticated":
		out := &reachRejectionBody{Rung: parts[0], Class: parts[1]}
		if len(parts) == 5 {
			out.Pinned, out.Offered = parts[3], parts[4]
		}
		return out
	}
	return nil
}

// addressFromParams renders the probed address from an endpoint's params
// jsonb: target, with :port appended when the params carry one. An empty or
// unparseable params yields an empty address (real field only, never invented).
func addressFromParams(params []byte) string {
	if len(params) == 0 {
		return ""
	}
	var p struct {
		Target string          `json:"target"`
		Port   json.RawMessage `json:"port"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Target == "" {
		return ""
	}
	if port := portString(p.Port); port != "" {
		return p.Target + ":" + port
	}
	return p.Target
}

// portString normalizes a port that may be a JSON number or string.
func portString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil && n.String() != "" {
		return n.String()
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}
