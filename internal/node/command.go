package node

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats.go"
)

// The node side of the command wire (#815): pull the pending queue (a
// request-reply mirroring the worklist), actuate each delivery over its
// transport, and report the outcome on the status subject. Delivery is
// at-least-once, so execution is idempotent per command id: `executed`
// remembers each command's outcome for the life of the run, and a redelivery
// repeats the report without touching the device again.

// runCommands runs one pull-execute-report cycle. A pull failure is silent
// (the next tick re-pulls, the server redelivers); a report publish failure is
// also survivable, since the redelivered command finds its outcome remembered.
func runCommands(ctx context.Context, nc *nats.Conn, name string, runner *collection.Runner, executed map[int64]string) {
	msg, err := nc.Request(collection.CommandSubject(name), nil, worklistTimeout)
	if err != nil {
		return
	}
	var reply collection.CommandPullReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return
	}
	for _, d := range reply.Commands {
		outcome, done := executed[d.ID]
		if !done {
			outcome = executeDelivery(ctx, runner, d)
			executed[d.ID] = outcome
			if outcome == "" {
				slog.Info("command actuated", "facility", "command", "command", d.ID, "type", d.CommandType)
			} else {
				slog.Warn("command failed", "facility", "command", "command", d.ID, "type", d.CommandType, "error", outcome)
			}
		}
		st, err := json.Marshal(collection.CommandStatus{ID: d.ID, Error: outcome})
		if err != nil {
			continue
		}
		_ = nc.Publish(collection.CommandStatusSubject(name), st)
	}
}

// executeDelivery actuates one rendered command over its transport, returning
// the empty string on success and the failure story otherwise. Any answer from
// the device counts as an actuation; whether the device DID it is settlement's
// judgment, made against observed values, not the wire's.
func executeDelivery(ctx context.Context, runner *collection.Runner, d collection.CommandDelivery) string {
	switch d.Transport {
	case "tcp":
		if runner.Line == nil {
			return "node: no line actuator wired"
		}
		if d.Target == "" {
			return "node: command delivery carries no target"
		}
		if _, err := runner.Line.Exchange(ctx, d.Target, d.Line, 0); err != nil {
			return err.Error()
		}
		return ""
	default:
		return "node: no actuator for transport " + d.Transport
	}
}
