package collection

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// groupedWorklist is a worklist shaped like a site's: endpoints running one
// driver, each with a reachability probe (no session, only its rung's
// credential) and driver tasks (the session block, every credential), the
// driver's specs shared across endpoints, and one endpoint reached through a
// path upstream. Its tasks are in id order, the order the server builds them in.
func groupedWorklist() WorklistReply {
	session := &driver.SessionSpec{Credential: "login"}
	ladder := &driver.EffectiveLadder{Ping: true, Transport: "handshake", Session: true}
	reply := WorklistReply{ConfigGeneration: 1731500000, ReportSeconds: 60}
	for e := range 4 {
		ep := fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", e)
		all := map[string]map[string]string{"login": {"user": "admin", "password": fmt.Sprintf("pw-%d", e)}}
		base := TaskSpec{EndpointID: ep, EndpointName: "https", Transport: "https",
			EndpointParams: json.RawMessage(fmt.Sprintf(`{"target":"10.0.0.%d:443","tls-mode":"pin"}`, e)),
			SessionRetry:   int64(e), CertificatePin: fmt.Sprintf("SHA256:pin-%d", e), Fingerprint: fmt.Sprintf("fp-%d", e),
			Ladder: ladder}
		if e == 2 {
			base.Rejection = "credential-rejected"
			base.ComponentID = "c-2"
			base.PathUpstreams = []PathUpstream{{ComponentID: "c-switch", ComponentName: "switch", EndpointIDs: []string{"e-switch"}}}
		}
		probe := base
		probe.ID, probe.Mode, probe.Spec = fmt.Sprintf("%d-probe", e), "probe", json.RawMessage(`{}`)
		reply.Tasks = append(reply.Tasks, probe)
		for m, mode := range []string{"poll", "stream", "poll"} {
			t := base
			t.ID, t.Mode = fmt.Sprintf("%d-task-%d", e, m), mode
			t.Spec = json.RawMessage(fmt.Sprintf(`{"driver":"projector","get":"/status/%d"}`, m))
			t.Session, t.Secrets = session, all
			reply.Tasks = append(reply.Tasks, t)
		}
	}
	return reply
}

// A worklist reads back through the grouped wire as the same tasks, ids and
// every field unchanged (#1583).
func TestAWorklistRoundTripsThroughTheGroupedWire(t *testing.T) {
	want := groupedWorklist()
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got WorklistReply
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read back\n%+v\nwant\n%+v", got, want)
	}
}

// The grouped wire carries each endpoint's fields once however many tasks it
// runs, and a spec or session block the endpoints share once for the reply.
func TestTheGroupedWireCarriesEachEndpointOnce(t *testing.T) {
	raw, err := json.Marshal(groupedWorklist())
	if err != nil {
		t.Fatal(err)
	}
	for _, once := range []string{`"SHA256:pin-1"`, `"fp-3"`, `"10.0.0.0:443`, `"credential-rejected"`, `"pw-2"`, `"/status/1"`, `{"credential":"login"}`} {
		if n := strings.Count(string(raw), once); n != 1 {
			t.Errorf("%s appears %d times on the wire, want once:\n%s", once, n, raw)
		}
	}
}

// Tasks of one endpoint that disagree on a field the wire carries per endpoint
// still read back exactly: the wire gives them a group each rather than
// dropping the difference.
func TestTasksOfOneEndpointThatDisagreeStillRoundTrip(t *testing.T) {
	want := groupedWorklist()
	want.Tasks[1].CertificatePin = "SHA256:another"
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got WorklistReply
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read back\n%+v\nwant\n%+v", got, want)
	}
}

// A task naming a dictionary entry the reply does not carry is a decode
// error, never a task with a missing spec.
func TestATaskNamingAMissingEntryDoesNotDecode(t *testing.T) {
	var got WorklistReply
	err := json.Unmarshal([]byte(`{"config_generation":1,"endpoints":[{"name":"tcp","transport":"tcp","tasks":[{"id":"a","mode":"poll","spec":2}]}],"specs":[{}]}`), &got)
	if err == nil || !strings.Contains(err.Error(), "spec 2") {
		t.Fatalf("err = %v, want the missing spec named", err)
	}
}
