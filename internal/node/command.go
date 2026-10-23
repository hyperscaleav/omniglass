package node

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats.go"
)

// The node side of the command wire (#815): pull the pending queue (a
// request-reply mirroring the worklist), actuate each delivery over its
// transport, and report the outcome on the status subject. Delivery is
// at-least-once, so execution is idempotent per command id: `executed`
// remembers each command's outcome, and a redelivery repeats the report without
// touching the device again.

// commandMemoryTTL bounds how long a command's remembered outcome is kept. The
// server's redelivery window is deliveryTTL (10 minutes, internal/storage); once
// a command can no longer be redelivered its remembered outcome is dead weight,
// so the map is pruned past a margin over that window rather than growing for the
// life of the run.
const commandMemoryTTL = 15 * time.Minute

// commandOutcome is a remembered execution: the failure story (empty on success)
// and when it was recorded, so a stale entry can be pruned.
type commandOutcome struct {
	outcome string
	reply   string
	at      time.Time
}

// pruneCommandMemory drops outcomes older than commandMemoryTTL: they can no
// longer be redelivered, so keeping them only grows the map over a long run.
func pruneCommandMemory(executed map[int64]commandOutcome, now time.Time) {
	for id, o := range executed {
		if now.Sub(o.at) > commandMemoryTTL {
			delete(executed, id)
		}
	}
}

// runCommands runs one pull-execute-report cycle. A pull failure is silent
// (the next tick re-pulls, the server redelivers); a report publish failure is
// also survivable, since the redelivered command finds its outcome remembered.
func runCommands(ctx context.Context, nc *nats.Conn, name string, runner *collection.Runner, sessions *sessionManager, executed map[int64]commandOutcome) {
	now := time.Now()
	pruneCommandMemory(executed, now)
	msg, err := nc.Request(collection.CommandSubject(name), nil, worklistTimeout)
	if err != nil {
		return
	}
	var reply collection.CommandPullReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return
	}
	for _, d := range reply.Commands {
		o, done := executed[d.ID]
		outcome, reply := o.outcome, o.reply
		if !done {
			reply, outcome = executeDelivery(ctx, runner, d, sessions)
			executed[d.ID] = commandOutcome{outcome: outcome, reply: reply, at: now}
			if outcome == "" {
				slog.Info("command actuated", "facility", "command", "command", d.ID, "type", d.CommandType)
			} else {
				slog.Warn("command failed", "facility", "command", "command", d.ID, "type", d.CommandType, "error", outcome)
			}
		}
		st, err := json.Marshal(collection.CommandStatus{ID: d.ID, Error: outcome, Reply: reply})
		if err != nil {
			continue
		}
		_ = nc.Publish(collection.CommandStatusSubject(name), st)
	}
}

// executeDelivery actuates one rendered command over its transport, returning
// the device's reply and the failure story (empty on success). A command on
// an endpoint the node holds a session for rides that session (#817), its
// answer picked by the binding's reply shape; with none held it falls back
// to a throwaway connection, unless the command is session-only (#1049), when
// it fails rather than skip the auth step. Any answer from the device counts as an
// actuation; whether the device DID it is settlement's judgment, made on the
// server from the reply (when the binding settles from the response) or from
// what is observed next.
func executeDelivery(ctx context.Context, runner *collection.Runner, d collection.CommandDelivery, sessions *sessionManager) (reply, outcome string) {
	switch d.Transport {
	case "tcp", "telnet", "ssh":
		if held := sessions.lookup(d.EndpointID); held != nil {
			reply, err := held.Exchange(ctx, d.Line, d.Reply, 0)
			var refused *collection.RefusedError
			if errors.As(err, &refused) {
				// The device answered with its refusal: the execution error
				// carries its text, and the reply carries it too.
				return refused.Line, err.Error()
			}
			if err != nil {
				return "", err.Error()
			}
			return reply, ""
		}
		if d.SessionOnly || d.Transport != "tcp" {
			// The command rides the held session or fails: a throwaway
			// connection would skip the auth step (#1049).
			return "", "node: no session is held for endpoint " + d.EndpointID + ", and its commands ride only a held session"
		}
		if runner.Line == nil {
			return "", "node: no line actuator wired"
		}
		if d.Target == "" {
			return "", "node: command delivery carries no target"
		}
		reply, err := runner.Line.Exchange(ctx, d.Target, d.Line, 0)
		if err != nil {
			return "", err.Error()
		}
		return reply, ""
	default:
		return "", "node: no actuator for transport " + d.Transport
	}
}
