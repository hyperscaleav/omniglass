package collection

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The driver-poll path (#814): a poll task derived from a driver spec fetches
// over its transport, and the interpreter (internal/driver) locates and types
// each emit. The runner is the seam: fetchers are injected capabilities, and
// interpretation is pure.

// DriverPollTask is one parsed driver poll: the baked function, the transport
// it rides, the endpoint's target, and the unsealed secret inputs the worklist
// delivered (secret input name to its fields).
type DriverPollTask struct {
	Fn        *driver.BakedFunction
	Transport string
	Target    string
	Secrets   map[string]map[string]string
	Timeout   time.Duration
}

// snmpCommunity is the engine's credential contract for the snmp transport:
// the input named community (an snmp-community secret) supplies its community
// field. The same naming contract that maps host and port to the dial target.
func (t DriverPollTask) snmpCommunity() (string, bool) {
	c, ok := t.Secrets["community"]["community"]
	return c, ok && c != ""
}

// CollectDriverPoll runs one driver poll: fetch the response over the
// transport, interpret every emit against it. The returned faults are the
// collection-failed stories (a failed fetch, a missing credential, an emit
// that did not locate or type); samples and faults are independent, so a
// payload missing one emit still lands the rest, while a failed fetch lands
// nothing at all.
func (r *Runner) CollectDriverPoll(ctx context.Context, t DriverPollTask) ([]Sample, []error) {
	if t.Fn == nil || t.Fn.Request == nil {
		return nil, []error{fmt.Errorf("collection: driver poll carries no request")}
	}
	if t.Target == "" {
		return nil, []error{fmt.Errorf("collection: driver poll %s/%s: empty target", t.Fn.Driver, t.Fn.Function)}
	}

	var payload driver.Payload
	switch t.Transport {
	case "snmp":
		community, ok := t.snmpCommunity()
		if !ok {
			return nil, []error{fmt.Errorf("collection: driver poll %s/%s: no community credential was delivered for the snmp fetch", t.Fn.Driver, t.Fn.Function)}
		}
		if r.SNMP == nil {
			return nil, []error{fmt.Errorf("collection: driver poll %s/%s: no snmp fetcher wired", t.Fn.Driver, t.Fn.Function)}
		}
		values, err := r.SNMP.Get(ctx, t.Target, community, t.Fn.Request.Get, t.Timeout)
		if err != nil {
			return nil, []error{fmt.Errorf("collection: driver poll %s/%s: %w", t.Fn.Driver, t.Fn.Function, err)}
		}
		payload.Values = values
	case "tcp":
		if r.Line == nil {
			return nil, []error{fmt.Errorf("collection: driver poll %s/%s: no line fetcher wired", t.Fn.Driver, t.Fn.Function)}
		}
		answer, err := r.Line.Exchange(ctx, t.Target, t.Fn.Request.Line, t.Timeout)
		if err != nil {
			return nil, []error{fmt.Errorf("collection: driver poll %s/%s: %w", t.Fn.Driver, t.Fn.Function, err)}
		}
		payload.Text = answer
	default:
		return nil, []error{fmt.Errorf("collection: driver poll %s/%s: no fetcher for transport %q", t.Fn.Driver, t.Fn.Function, t.Transport)}
	}

	emitted, faults := t.Fn.Interpret(payload, time.Now().UTC())
	out := make([]Sample, 0, len(emitted))
	for _, e := range emitted {
		out = append(out, Sample{
			Name:   e.Name,
			Value:  e.Value,
			Text:   e.Text,
			IsText: e.IsText,
			TS:     e.TS,
		})
	}
	return out, faults
}
