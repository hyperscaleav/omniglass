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
	// Session is the endpoint's held session, when the node holds one
	// (#816): a line poll borrows it instead of dialing a throwaway
	// connection, and a session that is not up faults the poll rather than
	// the poll reconnecting on its own.
	Session HeldExchanger
	// SessionOnly forbids the throwaway connection (#1049): the driver
	// declares a session (a login a throwaway would skip, or retry once
	// rejected), or the transport has no stateless line.
	SessionOnly bool
	// Datagram is a udp endpoint's own socket (#1037), which the poll's
	// request rides, re-sent up to Retries times within the timeout.
	Datagram DatagramExchanger
	Retries  int
	// Auth is the endpoint's driver's session block, whose scheme an http
	// request's credential rides and whose terminal rules classify its
	// answer (#902); TLS is an https endpoint's trust (its mode and the pin
	// its probe shares), nil over plain http.
	Auth *driver.SessionSpec
	TLS  *TLSTrust
	// HTTPState is the endpoint's learned http auth (#1114), which its
	// polls and commands share: the cookie jar, the digest challenge, a
	// login's session. Nil runs each poll on a throwaway (a login per poll).
	HTTPState *HTTPAuthState
}

// HeldExchanger is the slice of a held session a poll borrows: one request,
// its answer. *Session satisfies it.
type HeldExchanger interface {
	Exchange(ctx context.Context, line string, reply *driver.Reply, timeout time.Duration) (string, error)
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
	var proven []Sample
	switch {
	case driver.IsHTTP(t.Transport):
		p, auth, faults := r.fetchHTTPPoll(ctx, t)
		if faults != nil {
			return auth, faults
		}
		payload, proven = p, auth
	case t.Transport == "snmp":
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
	case t.Transport == "udp":
		if t.Datagram == nil {
			return nil, []error{fmt.Errorf("collection: driver poll %s/%s: no udp socket is bound for this endpoint", t.Fn.Driver, t.Fn.Function)}
		}
		ans, err := t.Datagram.Exchange(ctx, t.Fn.Request.Line, t.Fn.Request.Reply, t.Timeout, t.Retries)
		if err != nil {
			return nil, []error{fmt.Errorf("collection: driver poll %s/%s: %w", t.Fn.Driver, t.Fn.Function, err)}
		}
		payload.Text = ans.Line
	case IsLineTransport(t.Transport):
		if t.Session != nil {
			answer, err := t.Session.Exchange(ctx, t.Fn.Request.Line, t.Fn.Request.Reply, t.Timeout)
			if err != nil {
				return nil, []error{fmt.Errorf("collection: driver poll %s/%s: %w", t.Fn.Driver, t.Fn.Function, err)}
			}
			payload.Text = answer
			break
		}
		if t.SessionOnly || t.Transport != "tcp" {
			return nil, []error{fmt.Errorf("collection: driver poll %s/%s: no session is held for this endpoint, and its %s line rides only a held session", t.Fn.Driver, t.Fn.Function, t.Transport)}
		}
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
	out := make([]Sample, 0, len(emitted)+len(proven))
	out = append(out, proven...)
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

// fetchHTTPPoll sends an http poll's request with the endpoint's credential
// and judges the answer (#902): the payload a 2xx carries in its request's
// format, the endpoint-authenticated rung a credentialed answer proves
// (decision 7 of #1113), and the fault an answer or a failed request is,
// classified so a refused credential gates (a TerminalError), a request's own
// fault waits its interval (a RequestFault) and a 5xx backs off.
func (r *Runner) fetchHTTPPoll(ctx context.Context, t DriverPollTask) (driver.Payload, []Sample, []error) {
	name := t.Fn.Driver + "/" + t.Fn.Function
	if r.HTTPFetch == nil {
		return driver.Payload{}, nil, []error{fmt.Errorf("collection: driver poll %s: no http fetcher wired", name)}
	}
	cred, err := driver.ResolveHTTPCredential(t.Auth, t.Secrets)
	if err != nil {
		return driver.Payload{}, nil, []error{fmt.Errorf("collection: driver poll %s: %w", name, err)}
	}
	x := HTTPExchange{Target: t.Target, Request: *t.Fn.Request, Credential: cred, Session: t.Auth, Timeout: t.Timeout, State: t.HTTPState}
	if t.Transport == "https" {
		if t.TLS == nil {
			return driver.Payload{}, nil, []error{fmt.Errorf("collection: driver poll %s: no tls trust for the https endpoint", name)}
		}
		x.TLS = t.TLS
	}
	ans, sx, err := r.DoHTTP(ctx, x, t.Secrets)
	if err != nil {
		// A login the component refused proves the credential wrong, as a
		// refused request does.
		var proven []Sample
		if terr, ok := AsTerminal(err); ok && terr.Class == driver.ClassAuth {
			proven = authenticatedSample("no", terr.Error())
		}
		return driver.Payload{}, proven, []error{fmt.Errorf("collection: driver poll %s: %w", name, err)}
	}
	judged := JudgeHTTPAnswer(ans, sx)
	var proven []Sample
	if verdict, ok := Authenticated(ans, judged); ok {
		reason := "the component accepted the credential"
		if verdict == "no" {
			reason = judged.Error()
		}
		proven = authenticatedSample(verdict, reason)
	}
	if judged != nil {
		return driver.Payload{}, proven, []error{fmt.Errorf("collection: driver poll %s: %w", name, judged)}
	}
	return driver.HTTPPayload(ans.Body, t.Fn.Request), proven, nil
}

// authenticatedSample is the endpoint-authenticated rung's sample.
func authenticatedSample(verdict, reason string) []Sample {
	return []Sample{{Name: SignalEndpointAuthenticated, Text: verdict, IsText: true, TS: time.Now().UTC(),
		Labels: map[string]string{ReasonLabel: reason}}}
}
