package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The log read BFF: a per-component stream of raw log lines (ADR-0066), the raw
// arrival lane, distinct from the typed event sink. A log line is untyped text a
// rule may later derive an event from; most never do. A plain typed Huma GET,
// gated by component:read and scope-injected through GetComponent (an out-of-scope
// component is a non-disclosing 404). Read-only.

const (
	logHistoryWindow = 24 * time.Hour
	logReadLimit     = 200
)

type logBody struct {
	TS            time.Time       `json:"ts" doc:"When the line arrived"`
	Source        string          `json:"source,omitempty" doc:"The channel the line arrived on (e.g. syslog)"`
	Severity      string          `json:"severity,omitempty" doc:"The line's severity, when classified"`
	Facility      string          `json:"facility,omitempty" doc:"The line's facility, when classified"`
	Instance      string          `json:"instance,omitempty" doc:"The series discriminator, when set"`
	Message       string          `json:"message" doc:"The raw log text"`
	Attributes    json.RawMessage `json:"attributes,omitempty" doc:"Structured fields parsed from the line, when present"`
	Labels        json.RawMessage `json:"labels,omitempty" doc:"Freeform classification labels, when present"`
	CorrelationID string          `json:"correlation_id,omitempty" doc:"Threads related lines and their derived events"`
}

type logsOutput struct {
	Body struct {
		Component string    `json:"component"`
		Logs      []logBody `json:"logs"`
	}
}

type nodeLogsOutput struct {
	Body struct {
		Node string    `json:"node"`
		Logs []logBody `json:"logs"`
	}
}

// logBodies maps stored log rows to the wire body. Shared by the component and
// node reads: both are the same raw log line, only the owner differs.
func logBodies(rows []storage.LogLine) []logBody {
	out := make([]logBody, 0, len(rows))
	for _, l := range rows {
		out = append(out, logBody{
			TS:            l.TS,
			Source:        l.Source,
			Severity:      l.Severity,
			Facility:      l.Facility,
			Instance:      l.Instance,
			Message:       l.Message,
			Attributes:    json.RawMessage(l.Attributes),
			Labels:        json.RawMessage(l.Labels),
			CorrelationID: l.CorrelationID,
		})
	}
	return out
}

// registerLogRoutes wires the per-component raw-log read, the operator-facing
// surface over the ingest lane.
func registerLogRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-logs",
		Method:      http.MethodGet,
		Path:        "/components/{name}/logs",
		Summary:     "List a component's recent log lines",
		Description: "Returns the component's recent raw log lines (the ingest lane of ADR-0066, distinct from typed events), newest first, bounded to the last 24 hours. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentPathInput) (*logsOutput, error) {
		comp, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		since := time.Now().UTC().Add(-logHistoryWindow)
		rows, err := gw.ListComponentLogs(ctx, comp.ID, since, logReadLimit)
		if err != nil {
			// mapRefErr first: comp.ID is always a uuid (GetComponent above
			// already resolved it), so ListComponentLogs's own resolve can
			// never actually be ambiguous today, but this is the same
			// bare-name resolver every other component route runs through,
			// and a future caller that passes a name instead of comp.ID must
			// not silently regress to a 500 (ruling 1, #627).
			if refErr, ok := mapRefErr(err); ok {
				return nil, refErr
			}
			return nil, huma.Error500InternalServerError("read logs")
		}
		out := &logsOutput{}
		out.Body.Component = comp.Name
		out.Body.Logs = logBodies(rows)
		return out, nil
	})

	// The node self-log read (ADR-0066): a node's own operational log lines, the
	// same raw lane but owner-bound to the node. Gated by node:read and scope-injected
	// through GetNode; an out-of-scope node is a non-disclosing 404.
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-node-logs",
		Method:      http.MethodGet,
		Path:        "/nodes/{name}/logs",
		Summary:     "List a node's recent self-logs",
		Description: "Returns the node's own recent operational log lines (the raw ingest lane of ADR-0066, owner-bound to the node), newest first, bounded to the last 24 hours. Gated by node:read; an out-of-scope node is a non-disclosing 404.",
	}, "node", "read"), func(ctx context.Context, in *nodePathInput) (*nodeLogsOutput, error) {
		n, err := gw.GetNode(ctx, in.Name, a.scopeFor(ctx, "node", "read"))
		if err != nil {
			return nil, mapNodeErr(err)
		}
		since := time.Now().UTC().Add(-logHistoryWindow)
		rows, err := gw.ListNodeLogs(ctx, n.Name, since, logReadLimit)
		if err != nil {
			return nil, huma.Error500InternalServerError("read node logs")
		}
		out := &nodeLogsOutput{}
		out.Body.Node = n.Name
		out.Body.Logs = logBodies(rows)
		return out, nil
	})
}
