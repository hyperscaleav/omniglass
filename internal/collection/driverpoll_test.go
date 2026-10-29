package collection_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The driver-poll collector over faked fetchers: the emission contract between
// a fetched payload and the samples that ride the batch. The real wires are the
// integration tests' job (snmp_integration_test, lineproto_integration_test).

type fakeSNMPGetter struct {
	res       map[string]string
	err       error
	community string
	oids      []string
	target    string
}

func (f *fakeSNMPGetter) Get(_ context.Context, target, community string, oids []string, _ time.Duration) (map[string]string, error) {
	f.target, f.community, f.oids = target, community, oids
	return f.res, f.err
}

type fakeLineExchanger struct {
	answer string
	err    error
	sent   string
}

func (f *fakeLineExchanger) Exchange(_ context.Context, _, line string, _ time.Duration) (string, error) {
	f.sent = line
	return f.answer, f.err
}

func snmpPollTask() collection.DriverPollTask {
	return collection.DriverPollTask{
		Transport: "snmp",
		Target:    "10.20.4.40:161",
		Secrets:   map[string]map[string]string{"community": {"community": "lab-public"}},
		Fn: &driver.BakedFunction{
			Driver: "snmp-generic", Function: "scalars",
			Request: &driver.Request{Get: []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0"}},
			Emits: []driver.BakedEmit{
				{Name: "model-number", Lane: "property", Extract: driver.Extract{OID: "1.3.6.1.2.1.1.1.0"}},
				{Name: "uptime", Lane: "metric", Extract: driver.Extract{OID: "1.3.6.1.2.1.1.3.0"}, Transform: &driver.Transform{Scale: 0.01}},
			},
		},
	}
}

func TestCollectDriverPollSNMP(t *testing.T) {
	getter := &fakeSNMPGetter{res: map[string]string{
		"1.3.6.1.2.1.1.1.0": "Boreal AirWall 3",
		"1.3.6.1.2.1.1.3.0": "8123456",
	}}
	r := &collection.Runner{SNMP: getter}
	dps, faults := r.CollectDriverPoll(context.Background(), snmpPollTask())
	if len(faults) != 0 {
		t.Fatalf("faults: %v", faults)
	}
	byName := map[string]collection.Sample{}
	for _, d := range dps {
		byName[d.Name] = d
	}
	if s := byName["model-number"]; !s.IsText || s.Text != "Boreal AirWall 3" {
		t.Fatalf("model-number = %+v", s)
	}
	if s := byName["uptime"]; s.IsText || s.Value != 81234.56 {
		t.Fatalf("uptime = %+v, want the scaled metric", s)
	}
	if getter.community != "lab-public" || getter.target != "10.20.4.40:161" || len(getter.oids) != 2 {
		t.Fatalf("fetch used %q %q %v, want the task's credential, target and request", getter.community, getter.target, getter.oids)
	}
}

func TestCollectDriverPollFetchFailureLandsNoSample(t *testing.T) {
	r := &collection.Runner{SNMP: &fakeSNMPGetter{err: errors.New("agent silent")}}
	dps, faults := r.CollectDriverPoll(context.Background(), snmpPollTask())
	if len(dps) != 0 {
		t.Fatalf("a failed fetch landed samples: %v", dps)
	}
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "agent silent") {
		t.Fatalf("faults = %v, want the fetch failure carried", faults)
	}
}

func TestCollectDriverPollMissingCredential(t *testing.T) {
	task := snmpPollTask()
	task.Secrets = nil
	getter := &fakeSNMPGetter{}
	r := &collection.Runner{SNMP: getter}
	dps, faults := r.CollectDriverPoll(context.Background(), task)
	if len(dps) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "community") {
		t.Fatalf("dps %v faults %v, want one fault naming the missing community", dps, faults)
	}
	if getter.target != "" {
		t.Fatal("a credential-less poll still went to the wire")
	}
}

func TestCollectDriverPollPartialPayload(t *testing.T) {
	// sysUpTime missing: model-number still lands, uptime is a fault.
	r := &collection.Runner{SNMP: &fakeSNMPGetter{res: map[string]string{
		"1.3.6.1.2.1.1.1.0": "Boreal AirWall 3",
	}}}
	dps, faults := r.CollectDriverPoll(context.Background(), snmpPollTask())
	if len(dps) != 1 || dps[0].Name != "model-number" {
		t.Fatalf("dps = %v, want just model-number", dps)
	}
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "uptime") {
		t.Fatalf("faults = %v, want one naming uptime", faults)
	}
}

func TestCollectDriverPollLine(t *testing.T) {
	ex := &fakeLineExchanger{answer: "INPUT hdmi-2"}
	r := &collection.Runner{Line: ex}
	dps, faults := r.CollectDriverPoll(context.Background(), collection.DriverPollTask{
		Transport: "tcp",
		Target:    "10.20.4.50:51325",
		Fn: &driver.BakedFunction{
			Driver: "newtron-nvp", Function: "status",
			Request: &driver.Request{Line: "GET INPUT"},
			Emits: []driver.BakedEmit{
				{Name: "video-input", Lane: "property", Extract: driver.Extract{Regex: `^INPUT (\S+)$`}},
			},
		},
	})
	if len(faults) != 0 || len(dps) != 1 {
		t.Fatalf("dps %v faults %v", dps, faults)
	}
	if !dps[0].IsText || dps[0].Text != "hdmi-2" || ex.sent != "GET INPUT" {
		t.Fatalf("line poll = %+v (sent %q)", dps[0], ex.sent)
	}
}

// fakeHeld is a held session's exchange seam: what a poll borrows instead
// of dialing.
type fakeHeld struct {
	answer string
	err    error
	sent   string
	reply  *driver.Reply
}

func (f *fakeHeld) Exchange(_ context.Context, line string, reply *driver.Reply, _ time.Duration) (string, error) {
	f.sent, f.reply = line, reply
	return f.answer, f.err
}

// A line poll over a held session (#816) borrows the session and never
// dials a throwaway connection; a session that is not up faults the poll
// rather than falling back to a dial of its own.
func TestCollectDriverPollLineOverHeldSession(t *testing.T) {
	fn := &driver.BakedFunction{
		Driver: "newtron-nvp", Function: "status",
		Request: &driver.Request{Line: "GET INPUT", Reply: &driver.Reply{Prefix: "INPUT "}},
		Emits: []driver.BakedEmit{
			{Name: "video-input", Lane: "property", Extract: driver.Extract{Regex: `^INPUT (\S+)$`}},
		},
	}
	throwaway := &fakeLineExchanger{answer: "INPUT wrong"}
	r := &collection.Runner{Line: throwaway}
	held := &fakeHeld{answer: "INPUT hdmi3"}
	dps, faults := r.CollectDriverPoll(context.Background(), collection.DriverPollTask{
		Transport: "tcp", Target: "10.20.4.50:51325", Fn: fn, Session: held,
	})
	if len(faults) != 0 || len(dps) != 1 || dps[0].Text != "hdmi3" {
		t.Fatalf("dps %v faults %v, want the held session's answer", dps, faults)
	}
	if held.reply == nil || held.reply.Prefix != "INPUT " {
		t.Fatalf("the poll passed reply shape %+v, want its declared one", held.reply)
	}
	if held.sent != "GET INPUT" || throwaway.sent != "" {
		t.Fatalf("held sent %q, throwaway sent %q: the poll must borrow the session", held.sent, throwaway.sent)
	}

	notUp := &fakeHeld{err: errors.New("collection: session to 10.20.4.50:51325 is recovering: connection lost: EOF")}
	dps, faults = r.CollectDriverPoll(context.Background(), collection.DriverPollTask{
		Transport: "tcp", Target: "10.20.4.50:51325", Fn: fn, Session: notUp,
	})
	if len(dps) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "recovering") {
		t.Fatalf("dps %v faults %v, want one fault naming the session state", dps, faults)
	}
	if throwaway.sent != "" {
		t.Fatal("a poll over a session that is not up dialed a throwaway connection")
	}
}

func TestCollectDriverPollUnfetchableTransport(t *testing.T) {
	r := &collection.Runner{}
	task := snmpPollTask()
	task.Transport = "ssh"
	dps, faults := r.CollectDriverPoll(context.Background(), task)
	if len(dps) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "ssh") {
		t.Fatalf("dps %v faults %v, want one fault naming the transport", dps, faults)
	}
}

// A tls line poll (#1035) rides the held session like any line transport's,
// and never a throwaway connection: tls has no stateless line, since a
// throwaway handshake would bypass the pin the session holds.
func TestCollectDriverPollLineOverTLSRidesOnlyTheSession(t *testing.T) {
	fn := &driver.BakedFunction{
		Driver: "newtron-nvp-tls", Function: "status",
		Request: &driver.Request{Line: "GET INPUT", Reply: &driver.Reply{Prefix: "INPUT "}},
		Emits: []driver.BakedEmit{
			{Name: "video-input", Lane: "property", Extract: driver.Extract{Regex: `^INPUT (\S+)$`}},
		},
	}
	throwaway := &fakeLineExchanger{answer: "INPUT wrong"}
	r := &collection.Runner{Line: throwaway}
	held := &fakeHeld{answer: "INPUT hdmi2"}
	dps, faults := r.CollectDriverPoll(context.Background(), collection.DriverPollTask{
		Transport: "tls", Target: "10.20.4.50:51326", Fn: fn, Session: held,
	})
	if len(faults) != 0 || len(dps) != 1 || dps[0].Text != "hdmi2" {
		t.Fatalf("dps %v faults %v, want the held session's answer", dps, faults)
	}
	dps, faults = r.CollectDriverPoll(context.Background(), collection.DriverPollTask{
		Transport: "tls", Target: "10.20.4.50:51326", Fn: fn,
	})
	if len(dps) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "rides only a held session") {
		t.Fatalf("dps %v faults %v, want a fault: no session held", dps, faults)
	}
	if throwaway.sent != "" {
		t.Fatal("a tls poll dialed a throwaway connection")
	}
}

type fakeHTTPFetcher struct {
	ans  collection.HTTPAnswer
	err  error
	sent collection.HTTPExchange
}

func (f *fakeHTTPFetcher) Exchange(_ context.Context, x collection.HTTPExchange) (collection.HTTPAnswer, error) {
	f.sent = x
	a := f.ans
	a.Credentialed = x.Credential != nil
	return a, f.err
}

func httpPollTask() collection.DriverPollTask {
	return collection.DriverPollTask{
		Transport: "http",
		Target:    "10.20.4.41:80",
		Secrets:   map[string]map[string]string{"login": {"username": "admin", "password": "kestrel"}},
		Auth:      &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic},
		Fn: &driver.BakedFunction{
			Driver: "kestrel-api", Function: "status",
			Request: &driver.Request{Path: "/api/status"},
			Emits: []driver.BakedEmit{
				{Name: "power-state", Lane: "property", Extract: driver.Extract{JSONPath: "device.power"}},
				{Name: "volume", Lane: "metric", Extract: driver.Extract{JSONPath: "audio.volume"}},
			},
		},
	}
}

// An http poll (#902): the request leaves with the endpoint's credential,
// the JSON answer interprets into its lanes, and the credentialed answer
// proves the session rung (decision 7 of #1113).
func TestCollectDriverPollHTTP(t *testing.T) {
	f := &fakeHTTPFetcher{ans: collection.HTTPAnswer{Status: 200, Body: []byte(`{"device":{"power":"on"},"audio":{"volume":35}}`)}}
	r := &collection.Runner{HTTPFetch: f}
	dps, faults := r.CollectDriverPoll(context.Background(), httpPollTask())
	if len(faults) != 0 {
		t.Fatalf("faults %v", faults)
	}
	got := map[string]collection.Sample{}
	for _, d := range dps {
		got[d.Name] = d
	}
	if got["power-state"].Text != "on" || got["volume"].Value != 35 || got["endpoint-authenticated"].Text != "yes" {
		t.Fatalf("samples %v", dps)
	}
	if f.sent.Credential == nil || f.sent.Credential.Username != "admin" || f.sent.TLS != nil || f.sent.Request.Path != "/api/status" {
		t.Fatalf("sent %+v", f.sent)
	}
}

func TestCollectDriverPollHTTPRefusedAndFaulted(t *testing.T) {
	// A refused credential: a terminal auth the node gates on, authenticated no.
	r := &collection.Runner{HTTPFetch: &fakeHTTPFetcher{ans: collection.HTTPAnswer{Status: 401}}}
	dps, faults := r.CollectDriverPoll(context.Background(), httpPollTask())
	if len(faults) != 1 {
		t.Fatalf("faults %v", faults)
	}
	if terr, ok := collection.AsTerminal(faults[0]); !ok || terr.Class != driver.ClassAuth {
		t.Fatalf("a 401 faulted %v, want a terminal auth", faults[0])
	}
	if len(dps) != 1 || dps[0].Name != "endpoint-authenticated" || dps[0].Text != "no" {
		t.Fatalf("samples %v, want authenticated no alone", dps)
	}
	// A 404: the request's own fault, no gate.
	r = &collection.Runner{HTTPFetch: &fakeHTTPFetcher{ans: collection.HTTPAnswer{Status: 404, Body: []byte("no such route")}}}
	_, faults = r.CollectDriverPoll(context.Background(), httpPollTask())
	if _, ok := collection.AsRequestFault(faults[0]); !ok || !strings.Contains(faults[0].Error(), "no such route") {
		t.Fatalf("a 404 faulted %v, want a request fault carrying the component's text", faults[0])
	}
	// A missing credential field names the reference, never a value.
	task := httpPollTask()
	task.Secrets = map[string]map[string]string{"login": {"username": "admin"}}
	_, faults = r.CollectDriverPoll(context.Background(), task)
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "login.password") {
		t.Fatalf("faults %v, want the missing login.password named", faults)
	}
	// https needs its trust; with it the exchange carries it.
	task = httpPollTask()
	task.Transport = "https"
	_, faults = r.CollectDriverPoll(context.Background(), task)
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "tls") {
		t.Fatalf("an https poll with no trust = %v", faults)
	}
	f := &fakeHTTPFetcher{err: errors.New("dial refused")}
	task.TLS = &collection.TLSTrust{Mode: driver.TLSModePin}
	_, faults = (&collection.Runner{HTTPFetch: f}).CollectDriverPoll(context.Background(), task)
	if len(faults) != 1 || f.sent.TLS == nil {
		t.Fatalf("faults %v sent %+v", faults, f.sent)
	}
}

// A polled room list (#1386): records "rooms" and identity ucc-room-id read
// each room as its own record, so one poll yields one identified sample per
// room, each carrying its identity to the batch.
func TestCollectDriverPollHTTPRecordsCarryIdentity(t *testing.T) {
	f := &fakeHTTPFetcher{ans: collection.HTTPAnswer{Status: 200, Body: []byte(`{"rooms":[{"id":"r1","state":"in-call"},{"id":"r2","state":"idle"}]}`)}}
	task := httpPollTask()
	task.Auth, task.Secrets = nil, nil
	task.Fn = &driver.BakedFunction{
		Driver: "ucc-api", Function: "rooms",
		Request:  &driver.Request{Path: "/rooms"},
		Records:  "rooms",
		Identity: &driver.IdentitySpec{Key: "ucc-room-id", Extract: driver.Extract{JSONPath: "id"}},
		Emits:    []driver.BakedEmit{{Name: "call-state", Lane: "property", Extract: driver.Extract{JSONPath: "state"}}},
	}
	dps, faults := (&collection.Runner{HTTPFetch: f}).CollectDriverPoll(context.Background(), task)
	if len(faults) != 0 {
		t.Fatalf("faults %v", faults)
	}
	got := map[string]string{}
	for _, d := range dps {
		if d.Name != "call-state" {
			continue
		}
		if d.Identity == nil || d.Identity.Key != "ucc-room-id" {
			t.Fatalf("sample %+v carries no identity", d)
		}
		got[d.Identity.Value] = d.Text
	}
	if len(got) != 2 || got["r1"] != "in-call" || got["r2"] != "idle" {
		t.Fatalf("call-state by room = %v (samples %+v)", got, dps)
	}
}
