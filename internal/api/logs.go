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
		rows, err := gw.ListComponentLogs(ctx, comp.Name, since, logReadLimit)
		if err != nil {
			return nil, huma.Error500InternalServerError("read logs")
		}
		out := &logsOutput{}
		out.Body.Component = comp.Name
		out.Body.Logs = make([]logBody, 0, len(rows))
		for _, l := range rows {
			out.Body.Logs = append(out.Body.Logs, logBody{
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
		return out, nil
	})
}
