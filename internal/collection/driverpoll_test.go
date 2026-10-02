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

func TestCollectDriverPollUnfetchableTransport(t *testing.T) {
	r := &collection.Runner{}
	task := snmpPollTask()
	task.Transport = "ssh"
	dps, faults := r.CollectDriverPoll(context.Background(), task)
	if len(dps) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "ssh") {
		t.Fatalf("dps %v faults %v, want one fault naming the transport", dps, faults)
	}
}
