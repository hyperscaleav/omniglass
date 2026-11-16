package node

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/redact"
)

// What a node collects never carries the credentials it collected with: a
// component that echoes its password back (in a property, a label, an
// identity, an event, a log line, or a failure's reason) has the echo
// masked on the node, before the group is queued, so it is never on the
// stream, never stored and never served. Every lane rides the spool, so the
// spool is the one place that masks.

const echoed = "hunter2-S3cret"

// echoingSamples is one sample on each lane, each carrying the secret in a
// form a component could echo it: raw, upper-cased, url-escaped, base64.
func echoingSamples() []collection.Sample {
	return []collection.Sample{
		{Name: "config", Text: "user=admin pass=" + echoed, IsText: true,
			Identity: &driver.Identity{Key: "account", Value: echoed}},
		{Name: "sessions", Value: 1, Labels: map[string]string{"auth": strings.ToUpper(echoed)}},
		{Name: "login-changed", Lane: driver.LaneEvent, Text: "login with " + echoed,
			Attributes: json.RawMessage(`{"secret":"` + echoed + `","port":4}`)},
		{Name: "line", Lane: driver.LaneLog, Text: "GET /?p=hunter2-S3cret", Source: "web " + echoed,
			Attributes: json.RawMessage(`{"basic":"` + redact.BasicAuth("admin", echoed) + `"}`)},
	}
}

// echoingTask is a task whose worklist marks the password secret and whose
// session presents it as basic authorization.
func echoingTask(id string) collection.TaskSpec {
	return collection.TaskSpec{ID: id, Mask: []string{echoed},
		Secrets: map[string]map[string]string{"login": {"username": "admin", "password": echoed}},
		Session: &driver.SessionSpec{Scheme: driver.SchemeBasic, Credential: "login"}}
}

func shippedText(t *testing.T, pub *capturePublisher) string {
	t.Helper()
	pub.mu.Lock()
	defer pub.mu.Unlock()
	var b strings.Builder
	for _, batch := range pub.sent {
		j, err := protojson.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		for _, g := range batch.GetGroups() {
			for _, e := range g.GetEvents() {
				b.Write(e.GetPayload())
			}
			for _, l := range g.GetTaskLogs() {
				b.Write(l.GetAttributes())
			}
		}
	}
	return b.String()
}

func TestSpoolMasksATasksSecretsOnEveryLane(t *testing.T) {
	t.Parallel()
	pub := &capturePublisher{}
	s := newTelemetrySpool("og.v1.node.telemetry", "edge", pub.publish)
	s.setMasks([]collection.TaskSpec{echoingTask("task-1")})

	if err := s.add(groupFor("task-1", echoingSamples())); err != nil {
		t.Fatal(err)
	}
	if err := s.add(collectionFailedGroup("task-1", errors.New("login refused for "+echoed))); err != nil {
		t.Fatal(err)
	}
	s.flush()
	waitIdle(t, s)

	got := shippedText(t, pub)
	for _, form := range []string{echoed, strings.ToUpper(echoed), "aHVudGVyMi1TM2NyZXQ", redact.BasicAuth("admin", echoed)} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(form)) {
			t.Errorf("the shipped batch carries the secret as %q:\n%s", form, got)
		}
	}
	if !strings.Contains(got, redact.Mask) {
		t.Errorf("nothing shipped reads as masked:\n%s", got)
	}

	g := pub.sent[0].GetGroups()[0]
	for _, e := range g.GetEvents() {
		if len(e.GetPayload()) > 0 && !json.Valid(e.GetPayload()) {
			t.Errorf("event %s payload is not JSON once masked: %s", e.GetName(), e.GetPayload())
		}
	}
	for _, p := range g.GetProperties() {
		var s string
		if err := json.Unmarshal([]byte(p.GetValueJson()), &s); err != nil {
			t.Errorf("property %s value is not a JSON string once masked: %s", p.GetName(), p.GetValueJson())
		}
	}
}

// A task with no secrets, and another task's secret, leave a group as it was.
func TestSpoolMasksOnlyTheTasksOwnSecrets(t *testing.T) {
	t.Parallel()
	pub := &capturePublisher{}
	s := newTelemetrySpool("og.v1.node.telemetry", "edge", pub.publish)
	s.setMasks([]collection.TaskSpec{echoingTask("task-2")})

	if err := s.add(groupFor("task-1", echoingSamples())); err != nil {
		t.Fatal(err)
	}
	s.flush()
	waitIdle(t, s)

	want := groupFor("task-1", echoingSamples())
	got := pub.sent[0].GetGroups()[0]
	if got.GetProperties()[0].GetValueJson() != want.GetProperties()[0].GetValueJson() {
		t.Errorf("an unmasked task's property changed: %s", got.GetProperties()[0].GetValueJson())
	}
	if string(got.GetEvents()[0].GetPayload()) != string(want.GetEvents()[0].GetPayload()) {
		t.Errorf("an unmasked task's event payload changed: %s", got.GetEvents()[0].GetPayload())
	}
}
