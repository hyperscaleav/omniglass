package node

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

func snmpPreviewTask() collection.TaskSpec {
	return collection.TaskSpec{
		ID: "preview-1", Mode: "poll", Transport: "snmp", EndpointName: "preview",
		EndpointParams: json.RawMessage(`{"target":"10.20.4.40:161"}`),
		Spec: json.RawMessage(`{"driver": "preview", "function": "scalars",
			"request": {"get": ["1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0"]},
			"emits": [
				{"name": "model-number", "lane": "property", "extract": {"oid": "1.3.6.1.2.1.1.1.0"}},
				{"name": "uptime", "lane": "metric", "extract": {"oid": "1.3.6.1.9.9"}}
			]}`),
		Secrets: map[string]map[string]string{"community": {"community": "lab-public"}},
	}
}

// A preview reads its poll once and answers with what it read, beside the
// raw answer and each emit that did not locate, and holds no session of its
// own: nothing an endpoint holds is touched (#1163).
func TestPreviewRunAnswersWhatThePollRead(t *testing.T) {
	runner := &collection.Runner{SNMP: fakeGetter{res: map[string]string{"1.3.6.1.2.1.1.1.0": "Boreal Edge 86", "1.3.6.1.2.1.1.3.0": "812"}}}
	reply := previewRun(t.Context(), runner, []collection.TaskSpec{snmpPreviewTask()}, 5*time.Second)
	if reply.Error != "" || len(reply.Results) != 1 {
		t.Fatalf("reply = %+v", reply)
	}
	res := reply.Results[0]
	if res.Function != "scalars" || len(res.Samples) != 1 || res.Samples[0].Name != "model-number" || res.Samples[0].Text != "Boreal Edge 86" {
		t.Errorf("result samples = %+v", res)
	}
	if len(res.Faults) != 1 || !strings.Contains(res.Faults[0], "uptime") {
		t.Errorf("faults = %v, want the emit that did not locate", res.Faults)
	}
	if !strings.Contains(res.Excerpt, "1.3.6.1.2.1.1.1.0 = Boreal Edge 86") {
		t.Errorf("excerpt = %q, want the raw answer", res.Excerpt)
	}
}

// What a preview cannot run without an endpoint faults naming why, and a
// fetch that fails is a fault with no samples.
func TestPreviewRunRefusesWhatOnlyAnEndpointHolds(t *testing.T) {
	udp := snmpPreviewTask()
	udp.Transport = "udp"
	_, faults, _, _ := collectPreviewTask(t.Context(), &collection.Runner{}, udp)
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "udp") {
		t.Errorf("udp preview faults = %v", faults)
	}
	held := snmpPreviewTask()
	held.Transport = "tcp"
	held.Session = &driver.SessionSpec{}
	_, faults, _, _ = collectPreviewTask(t.Context(), &collection.Runner{}, held)
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "held session") {
		t.Errorf("held-session preview faults = %v", faults)
	}
	_, faults, excerpt, _ := collectPreviewTask(t.Context(), &collection.Runner{SNMP: fakeGetter{err: errTimeout{}}}, snmpPreviewTask())
	if len(faults) != 1 || excerpt != "" {
		t.Errorf("a failed fetch = %v %q", faults, excerpt)
	}
}

// A run now refuses a task that is not a driver poll, before anything runs.
func TestRunNowRefusesANonPoll(t *testing.T) {
	probe := snmpPreviewTask()
	probe.Mode = "probe"
	if r := runNowTask(t.Context(), nil, "edge", &collection.Runner{}, nil, probe, time.Second); r.Error == "" {
		t.Errorf("a non-poll run = %+v, want a refusal", r)
	}
}

type errTimeout struct{}

func (errTimeout) Error() string { return "i/o timeout" }
