package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The push-ingest write BFF: a first-party route that accepts telemetry in OUR
// schema (a webhook, by contrast, is the case where we do not control the payload
// and something has to normalize it, which is driver work). It is the second ingest
// lane beside the node's, and the two differ in exactly one respect: where the owner
// comes from. A node cannot be trusted to name its own owner, so the server derives
// it from the task's interface; a scoped principal can be, because the scope check
// is the fence.
//
// The route authorizes, resolves and validates, then publishes onto the data lane
// (og.v1.api.telemetry) rather than writing to Postgres directly. That is deliberate:
// the rule engine is designed as a stream consumer, so anything skipping the stream
// would be invisible to every rule, alarm, and derivation. One lane for history,
// regardless of how a record arrived.

// pushBatchLimit bounds how many items one batch may carry, so a single request
// cannot pin the registry snapshot or the publish path for an unbounded time.
const pushBatchLimit = 1000

// TelemetryPublisher publishes an authorized batch onto the API telemetry lane. The
// bus server implements it; the API holds only this seam, so the route is testable
// with a fake and the handler never owns a NATS connection.
type TelemetryPublisher interface {
	PublishTelemetry(ctx context.Context, b *ogv1.TelemetryBatch) error
}

type pushSample struct {
	Name     string   `json:"name" minLength:"1" doc:"The canonical property name, or a registered event_type name"`
	Instance string   `json:"instance,omitempty" doc:"Discriminates many values of one name on one owner (three fan speeds, per-port counters)"`
	Number   *float64 `json:"number,omitempty" doc:"The value for a metric-kind property"`
	Text     *string  `json:"text,omitempty" doc:"The value for a state-kind property, or the message for an event_type"`
}

type pushLog struct {
	Message       string    `json:"message" minLength:"1" doc:"The raw log text"`
	Source        string    `json:"source,omitempty" doc:"The channel the line arrived on; falls back to the batch source"`
	Severity      string    `json:"severity,omitempty" doc:"The line's severity, when classified"`
	Facility      string    `json:"facility,omitempty" doc:"The line's facility, when classified"`
	CorrelationID string    `json:"correlation_id,omitempty" doc:"Threads related lines and their derived events"`
	TS            time.Time `json:"ts,omitempty" doc:"When the line arrived; defaults to the batch timestamp"`
}

type pushInput struct {
	Body struct {
		Owner struct {
			Kind string `json:"kind" enum:"component" doc:"The owner arc. Only component today; system and location arrive with #422"`
			Ref  string `json:"ref" minLength:"1" doc:"The owning entity, by name or id"`
		} `json:"owner"`
		Source  string       `json:"source,omitempty" doc:"Who observed this batch (recorded as the provenance source on every row)"`
		TS      time.Time    `json:"ts,omitempty" doc:"Batch timestamp; a per-item timestamp overrides it"`
		Samples []pushSample `json:"samples,omitempty" doc:"Registry-resolved observations. The registry decides which table each lands in"`
		Logs    []pushLog    `json:"logs,omitempty" doc:"Raw untyped log lines. No registry gate"`
	}
}

type pushRejection struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type pushOutput struct {
	Status int
	Body   struct {
		Accepted struct {
			Samples int `json:"samples"`
			Logs    int `json:"logs"`
		} `json:"accepted"`
		Rejected []pushRejection `json:"rejected,omitempty" doc:"Names dropped by reject-not-project. Reported synchronously so a caller learns about a typo"`
	}
}

// registerTelemetryRoutes wires the push-ingest write surface.
func registerTelemetryRoutes(api huma.API, a *authenticator, gw storage.Gateway, pub TelemetryPublisher) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "push-telemetry",
		Method:        http.MethodPost,
		Path:          "/telemetry:push",
		DefaultStatus: http.StatusAccepted,
		Summary:       "Push telemetry for an owner",
		Description: "Accepts samples and raw log lines for one owner and publishes them onto the ingest lane. " +
			"The registry decides where each sample lands (metric, state, or a caught event); an unregistered name is " +
			"rejected and reported in the response rather than silently dropped. Gated by telemetry:push, and the " +
			"caller's scope must cover the declared owner; an out-of-scope owner is a non-disclosing 404.",
	}, "telemetry", "push"), func(ctx context.Context, in *pushInput) (*pushOutput, error) {
		if pub == nil {
			return nil, huma.Error503ServiceUnavailable("telemetry ingest is unavailable (no bus)")
		}
		if in.Body.Owner.Kind != "component" {
			return nil, huma.Error422UnprocessableEntity("owner.kind must be component")
		}
		if len(in.Body.Samples) == 0 && len(in.Body.Logs) == 0 {
			return nil, huma.Error422UnprocessableEntity("a batch must carry at least one sample or log line")
		}
		if len(in.Body.Samples)+len(in.Body.Logs) > pushBatchLimit {
			return nil, huma.Error422UnprocessableEntity("batch exceeds the per-request item limit")
		}

		// Scope is the fence that makes a caller-declared owner trustworthy: reading
		// the component through the gateway with the caller's read scope means an
		// out-of-scope owner is a non-disclosing 404, exactly as it is everywhere else.
		comp, err := gw.GetComponent(ctx, in.Body.Owner.Ref, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}

		// Reject-not-project, applied HERE so rejection is synchronous. Publishing is
		// asynchronous, so a route that validated nothing would have to answer "202,
		// and good luck", which is useless to a human who mistyped a property name.
		// The registry is an in-memory snapshot, so this costs a map lookup per name.
		properties, err := gw.ListPropertyTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("read registry")
		}
		eventTypes, err := gw.ListEventTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("read registry")
		}
		reg := collection.NewRegistry(properties, eventTypes)

		out := &pushOutput{Status: http.StatusAccepted}
		batch := &ogv1.TelemetryBatch{
			Owner:  &ogv1.Owner{Kind: "component", Ref: comp.Name},
			Source: in.Body.Source,
		}
		if !in.Body.TS.IsZero() {
			batch.Ts = timestamppb.New(in.Body.TS.UTC())
		}

		for _, s := range in.Body.Samples {
			if _, ok := reg.Allows(s.Name); !ok {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Name: s.Name, Reason: "unregistered property or event type name",
				})
				continue
			}
			sample := &ogv1.Sample{Name: s.Name, Instance: s.Instance}
			switch {
			case s.Number != nil:
				sample.Value = &ogv1.Sample_DoubleValue{DoubleValue: *s.Number}
			case s.Text != nil:
				sample.Value = &ogv1.Sample_StringValue{StringValue: *s.Text}
			default:
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Name: s.Name, Reason: "no value: set number or text",
				})
				continue
			}
			batch.Samples = append(batch.Samples, sample)
		}

		for _, l := range in.Body.Logs {
			line := &ogv1.LogLine{
				Message: l.Message, Source: l.Source, Severity: l.Severity,
				Facility: l.Facility, CorrelationId: l.CorrelationID,
			}
			if !l.TS.IsZero() {
				line.Ts = timestamppb.New(l.TS.UTC())
			}
			batch.Logs = append(batch.Logs, line)
		}

		// Everything rejected means nothing to publish: report it and stop rather than
		// putting an empty batch on the lane.
		if len(batch.Samples) == 0 && len(batch.Logs) == 0 {
			return out, nil
		}
		if err := pub.PublishTelemetry(ctx, batch); err != nil {
			return nil, huma.Error503ServiceUnavailable("publish telemetry")
		}
		out.Body.Accepted.Samples = len(batch.Samples)
		out.Body.Accepted.Logs = len(batch.Logs)
		return out, nil
	})
}
