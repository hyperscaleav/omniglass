package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// errNoCommunity is an snmp rung with no community to send: v2c has no
// unauthenticated request, so the get cannot be formed.
var errNoCommunity = errors.New("no community credential was delivered for the snmp get")

// collectSNMPRung runs an snmp endpoint's request rung (#821, ADR-0158): a
// get of a standard OID (sysUpTime.0 unless the driver names another), sent
// with the endpoint's community, answered, is the transport proven. Any
// response is an answer, a noSuchObject included: the rung reads responsive,
// lands one snmp-response-time, and, since a v2c agent answers only a
// community it accepts, endpoint-authenticated yes. An authorizationError is
// the agent refusing a community it knows: responsive, timed, authenticated
// no, and a terminal auth refusal that gates the session rung, beneath which
// no get is sent under that fingerprint (every v2c request carries the
// community). Silence reads timeout and says nothing of the community: a
// wrong v2c community is dropped silently and cannot be told from a stopped
// agent. No tcp-open lands, and the rung decides the verdict.
func collectSNMPRung(ctx context.Context, runner *collection.Runner, task collection.TaskSpec, req *driver.Request, sessions *sessionManager) ([]collection.Sample, error) {
	var p endpointParams
	if len(task.EndpointParams) > 0 {
		if err := json.Unmarshal(task.EndpointParams, &p); err != nil {
			return nil, fmt.Errorf("node: bad endpoint params for task %s: %w", task.ID, err)
		}
	}
	if p.Target == "" {
		return nil, fmt.Errorf("node: task %s: empty snmp target", task.ID)
	}
	now := time.Now().UTC()
	authenticated := func(verdict, reason string) collection.Sample {
		return collection.Sample{Name: collection.SignalEndpointAuthenticated, Text: verdict, IsText: true, TS: now,
			Labels: map[string]string{collection.ReasonLabel: reason}}
	}
	if gt, ok := sessions.blocked(task.EndpointID, rungAuthenticated); ok {
		// Held: the community is refused under this fingerprint, and v2c
		// has no request without it, so nothing is sent.
		return []collection.Sample{authenticated("no", "held: "+gt.class)}, nil
	}
	community, ok := task.Secrets["community"]["community"]
	if !ok || community == "" {
		return nil, fmt.Errorf("node: task %s: %w", task.ID, errNoCommunity)
	}
	if runner.SNMP == nil {
		return nil, fmt.Errorf("node: task %s: no snmp getter wired", task.ID)
	}
	var timeout time.Duration
	if p.Timeout != "" {
		d, err := time.ParseDuration(p.Timeout)
		if err != nil {
			return nil, fmt.Errorf("node: task %s: bad timeout %q: %w", task.ID, p.Timeout, err)
		}
		timeout = d
	}
	responsive := func(verdict, reason string) collection.Sample {
		return collection.Sample{Name: collection.SignalEndpointResponsive, Text: verdict, IsText: true, TS: now,
			Labels: map[string]string{collection.ReasonLabel: reason}}
	}
	start := time.Now()
	_, err := runner.SNMP.Get(ctx, p.Target, community, req.Get, timeout)
	took := collection.Sample{Name: collection.SignalSNMPResponseTime, Value: float64(time.Since(start).Microseconds()) / 1000, TS: now}
	if err == nil {
		sessions.pass(task.EndpointID)
		return []collection.Sample{responsive("up", string(collection.Responded)), took,
			authenticated("yes", "the agent answered the community")}, nil
	}
	if terr, ok := collection.AsTerminal(err); ok {
		sessions.refuseOver(task.Transport, task.EndpointID, task.Fingerprint, terr)
		return []collection.Sample{responsive("up", string(collection.Responded)), took,
			authenticated("no", terr.Error())}, nil
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("node: task %s: %w", task.ID, ctx.Err())
	}
	reason, ok := collection.Classify(err)
	if !ok {
		return nil, fmt.Errorf("node: task %s: %w", task.ID, err)
	}
	return []collection.Sample{responsive("down", string(reason))}, nil
}
