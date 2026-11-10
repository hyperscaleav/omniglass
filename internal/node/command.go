package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/nats-io/nats.go"
)

// The node side of the command wire (#815): pull the pending queue (a
// request-reply mirroring the worklist), actuate each delivery over its
// transport, and report the outcome on the status subject. Delivery is
// at-least-once, so execution is idempotent per command id: `executed`
// remembers each command's outcome, and a redelivery repeats the report without
// touching the device again.

// commandMemoryTTL bounds how long a command's remembered outcome is kept. The
// server's delivery window is deliveryTTL (storage.CommandDeliveryWindow, 10
// minutes); once
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

// commandState is what the command loop carries from one tick to the next:
// each command's remembered outcome (the idempotence a redelivery relies on),
// and whether the last pull failed, so a pull that keeps failing is logged on
// the transition, not on every tick.
type commandState struct {
	executed    map[int64]commandOutcome
	pullFailing bool
}

func newCommandState() *commandState {
	return &commandState{executed: map[int64]commandOutcome{}}
}

// runCommands runs one pull-execute-report cycle. A pull failure is counted
// on the node's report (#1626), and logged at WARN on the transition into
// failing and at INFO on the first pull that succeeds after (#1630), never on
// every tick: the next tick re-pulls and the server redelivers, but a node
// that cannot reach its queue leaves every command it carries to time out,
// and its log must say why. A pull reply it cannot parse is counted as a
// failed pull and logged at WARN every time, as is a report it cannot encode
// or publish; a lost report is survivable, since the redelivered command
// finds its outcome remembered. Each command waits for its endpoint's lane on
// pool, ahead of the scheduled tasks queued there (#1634), so it never talks
// to a device a poll is talking to; a nil pool runs it at once.
func runCommands(ctx context.Context, nc *nats.Conn, name string, runner *collection.Runner, sessions *sessionManager, pool *lanePool, st *commandState, self *nodeReport) {
	now := time.Now()
	pruneCommandMemory(st.executed, now)
	msg, err := nc.Request(collection.CommandSubject(name), nil, worklistTimeout)
	if err != nil {
		self.commandPullFailed()
		if !st.pullFailing {
			slog.Warn("command pull failed", "facility", "command", "node", name, "error", err.Error())
		}
		st.pullFailing = true
		return
	}
	if st.pullFailing {
		slog.Info("command pull recovered", "facility", "command", "node", name)
		st.pullFailing = false
	}
	var reply collection.CommandPullReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		self.commandPullFailed()
		slog.Warn("command pull reply could not be parsed", "facility", "command", "node", name, "error", err.Error())
		return
	}
	for _, d := range reply.Commands {
		if commandWaits(d, sessions) {
			continue
		}
		o, done := st.executed[d.ID]
		outcome, reply := o.outcome, o.reply
		if !done {
			reply, outcome = deliverOnLane(ctx, runner, d, sessions, pool)
			st.executed[d.ID] = commandOutcome{outcome: outcome, reply: reply, at: now}
			if outcome == "" {
				slog.Info("command actuated", "facility", "command", "command", d.ID, "type", d.CommandType)
			} else {
				slog.Warn("command failed", "facility", "command", "command", d.ID, "type", d.CommandType, "error", outcome)
			}
		}
		report, err := json.Marshal(collection.CommandStatus{ID: d.ID, Error: outcome, Reply: reply})
		if err != nil {
			slog.Warn("command report could not be encoded", "facility", "command", "node", name, "command", d.ID, "error", err.Error())
			continue
		}
		if err := nc.Publish(collection.CommandStatusSubject(name), report); err != nil {
			slog.Warn("command report could not be published", "facility", "command", "node", name, "command", d.ID, "error", err.Error())
		}
	}
}

// commandLane is the lane a command runs on: its endpoint's.
func commandLane(d collection.CommandDelivery) string {
	if d.EndpointID != "" {
		return d.EndpointID
	}
	return "command:" + strconv.FormatInt(d.ID, 10)
}

// deliverOnLane actuates one delivery on its endpoint's lane (#1249), as
// operator work ahead of the scheduled tasks queued there and never beside
// another request to the endpoint, and waits for it. With no pool it
// actuates directly.
func deliverOnLane(ctx context.Context, runner *collection.Runner, d collection.CommandDelivery, sessions *sessionManager, pool *lanePool) (reply, outcome string) {
	actuate := func() { reply, outcome = executeDelivery(ctx, runner, d, sessions) }
	if pool == nil {
		actuate()
		return reply, outcome
	}
	pool.do(commandLane(d), laneOperator, actuate)
	return reply, outcome
}

// commandWaits reports a command that waits a tick: an http command whose
// endpoint's stream is making its first attempt under this fingerprint
// (#1038) waits for the stream's verdict rather than sending the credential
// beside it, and the server redelivers it.
func commandWaits(d collection.CommandDelivery, sessions *sessionManager) bool {
	return driver.IsHTTP(d.Transport) && sessions.streamPending(d.EndpointID)
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
	if c, closed := sessions.consult(d.EndpointID, rungResponsive, true); closed {
		// A closed gate (#1056 ruling, #1650, #1651) fails the command fast
		// with its most causal reason on every path, never a dial the gate
		// says will not land. Asked here, as the endpoint's lane takes the
		// command, so a command queued behind a reboot reads the reboot.
		return "", commandRefusal(c)
	}
	if d.Disruptive {
		// A disruptive command (#1651) closes the gate of every endpoint of
		// its component on this node before it goes out, so nothing the
		// dispatcher starts meanwhile reads the device going down as a
		// failure. A send that fails leaves the gates closed: the device may
		// still have acted, and each reopens on its first up verdict after
		// the quiet period, or at the hold backstop.
		sessions.disrupt(d)
	}
	switch {
	case driver.IsHTTP(d.Transport):
		return executeHTTP(ctx, runner, d, sessions)
	case d.Transport == "udp":
		// A datagram from the endpoint's own socket (#1037), re-sent only
		// as often as the binding declares: a command sent twice may act
		// twice, so by default a lost reply fails it.
		sock := sessions.datagramOf(d.EndpointID)
		if sock == nil {
			return "", "node: no udp socket is bound for endpoint " + d.EndpointID + ": " + sessions.datagramRefusal(d.EndpointID)
		}
		ans, err := sock.Exchange(ctx, d.Line, d.Reply, sessions.datagramTimeout(d.EndpointID), d.Retries)
		if terr, ok := collection.AsTerminal(err); ok {
			sessions.refuseOver(d.Transport, d.EndpointID, sessions.currentFingerprint(d.EndpointID), terr)
			return "", err.Error()
		}
		var refused *collection.RefusedError
		if errors.As(err, &refused) {
			return refused.Line, err.Error()
		}
		if err != nil {
			return "", err.Error()
		}
		return ans.Line, ""
	case collection.IsLineTransport(d.Transport):
		if held := sessions.lookup(d.EndpointID); held != nil {
			line, shape := d.Line, d.Reply
			if driver.IsWebSocket(d.Transport) {
				// The frame takes a fresh id when its reply correlates
				// (#1034): minted here, at execute, so a redelivery never
				// reuses one.
				var err error
				if line, shape, err = d.Reply.Correlated(d.Line, driver.MintCorrelation()); err != nil {
					return "", err.Error()
				}
			}
			reply, err := held.Exchange(ctx, line, shape, 0)
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

// commandBodyCap bounds an http command's answer: a reply is read to settle
// the command and travels back on the status subject, so it is a small
// document, never a page.
const commandBodyCap = 64 << 10

// executeHTTP actuates an http command (#902): its rendered request, the
// endpoint's credential applied by the scheme its worklist delivered (the
// delivery never carries one), over plain http or the https trust its probe
// and polls share. The answer is judged by the same status table a poll is:
// a refused credential or certificate gates the endpoint, and the reply (the
// answer's body, scrubbed of the credential) goes back for the server to
// settle from.
func executeHTTP(ctx context.Context, runner *collection.Runner, d collection.CommandDelivery, sessions *sessionManager) (reply, outcome string) {
	if d.Request == nil {
		return "", "node: the http command delivery carries no request"
	}
	if runner.HTTPFetch == nil {
		return "", "node: no http actuator wired"
	}
	if gt, gated := sessions.refusedAt(d.EndpointID, rungAuthenticated); gated {
		// Every http request carries the credential, so a refused one holds
		// the command too (#1056 ruling), as it holds the poll.
		return "", "node: the endpoint is gated: " + gt.refusal().Error()
	}
	ep, ok := sessions.httpEndpointOf(d.EndpointID)
	if !ok {
		return "", "node: endpoint " + d.EndpointID + " carries no task on this node, so no credential or trust for its command"
	}
	cred, err := driver.ResolveHTTPCredential(ep.session, ep.secrets)
	if err != nil {
		return "", err.Error()
	}
	// The credential, the session and the trust are the worklist's, so the
	// address is too: a delivery naming another (the endpoint moved since
	// this node last read its worklist) is not sent, so a session opened
	// with one component never rides a request to another.
	target := ep.params.Target
	if d.Target != "" && target != "" && d.Target != target {
		return "", "node: the endpoint's address moved since this node last read its worklist, so the command was not sent to it"
	}
	if target == "" {
		target = d.Target
	}
	x := collection.HTTPExchange{Target: target, Request: *d.Request, Credential: cred, Session: ep.session, Timeout: ep.timeout, BodyCap: commandBodyCap,
		State: sessions.httpAuthOf(d.EndpointID, ep.task.Fingerprint)}
	if d.Transport == "https" {
		trust, err := tlsTrustOf(ep.params)
		if err != nil {
			return "", fmt.Sprintf("node: %v", err)
		}
		if trust.Mode == driver.TLSModePin {
			trust.Pin = sessions.certPinOf(ep.task)
		}
		x.TLS = &trust
	}
	// Over a login session (#1114) the command logs in when no session is
	// held, and once more on a lapse, sharing the session the polls keep.
	ans, sx, err := runner.DoHTTP(ctx, x, ep.secrets)
	if err != nil {
		if terr, ok := collection.AsTerminal(err); ok {
			sessions.refuseOver(d.Transport, d.EndpointID, sessions.currentFingerprint(d.EndpointID), terr)
		}
		return "", err.Error()
	}
	body := collection.Scrub(string(ans.Body), cred, sx.Redact...)
	if judged := collection.JudgeHTTPAnswer(ans, sx); judged != nil {
		if terr, ok := collection.AsTerminal(judged); ok {
			sessions.refuseOver(d.Transport, d.EndpointID, sessions.currentFingerprint(d.EndpointID), terr)
		}
		return body, judged.Error()
	}
	return body, ""
}
