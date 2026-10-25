package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A driver emit lands on any lane (#1145), at the write gate and the attach:
// the event catalog joins the emit resolution (a reserved event type refused
// naming its writer, a payload schema holding the attributes), a log line
// emit bakes with no name, and the boot audit reads a spec carrying one.

const laneSpec = `{
	"version": 1,
	"transport": "tcp",
	"inputs": [{"name": "host", "kind": "string", "required": true}, {"name": "port", "kind": "number", "default": "51325"}],
	"unmatched": "log",
	"listeners": [{
		"name": "events",
		"arm": ["SUBSCRIBE INPUT"],
		"match": {"prefix": "EVT INPUT "},
		"emits": [
			{"name": "video-input", "extract": {"regex": "^EVT INPUT (\\S+)$"}},
			{"name": "input-changed", "message": {"value": "Input changed"}, "attributes": {"input": {"extract": {"regex": "^EVT INPUT (\\S+)$"}}}}
		]
	}, {
		"name": "logs",
		"arm": ["SUBSCRIBE LOG"],
		"match": {"prefix": "LOG "},
		"emits": [
			{"log": {"severity": {"extract": {"regex": "^LOG (\\S+)"}}, "source": "nvp"}, "message": {"extract": {"regex": "^LOG \\S+ (.*)$"}}}
		]
	}]
}`

func TestADriverSpecEmitsEventsAndLogLines(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "lane-proto", Label: "Lane Proto", Version: "1.0.0", Spec: []byte(laneSpec)}); err != nil {
		t.Fatalf("a spec emitting an event and a log line refused: %v", err)
	}

	// A reserved event type is refused naming it, as a reserved metric is.
	for _, reserved := range []string{"collection-failed", "command-issued"} {
		bad := strings.Replace(laneSpec, `"name": "input-changed"`, `"name": "`+reserved+`"`, 1)
		_, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "bad-" + reserved, Label: "Bad", Version: "1.0.0", Spec: []byte(bad)})
		if !errors.Is(err, storage.ErrSpecInvalid) || !strings.Contains(err.Error(), reserved) {
			t.Fatalf("emitting %s = %v, want a spec refusal naming it", reserved, err)
		}
	}

	// An event type whose payload schema closes its fields holds the
	// attributes to them at write.
	if _, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{Name: "lamp-fault", Label: "Lamp fault",
		PayloadSchema: []byte(`{"type":"object","properties":{"lamp":{"type":"string"}},"additionalProperties":false}`)}); err != nil {
		t.Fatalf("create event type: %v", err)
	}
	extra := strings.Replace(laneSpec, `{"name": "input-changed", "message": {"value": "Input changed"}, "attributes": {"input"`,
		`{"name": "lamp-fault", "message": {"value": "Lamp fault"}, "attributes": {"colour"`, 1)
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "schema-proto", Label: "Schema", Version: "1.0.0", Spec: []byte(extra)}); !errors.Is(err, storage.ErrSpecInvalid) || !strings.Contains(err.Error(), "colour") {
		t.Fatalf("an attribute the schema forbids = %v, want a refusal naming it", err)
	}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-7"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	comp, drv := "panel-7", "lane-proto"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Inputs: map[string]string{"host": "10.20.4.50"}}, all)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	tasks, err := gw.ListTasks(ctx, all)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	baked := map[string]*driver.BakedFunction{}
	for _, task := range tasks {
		if task.EndpointID != ep.ID || task.Mode != "listen" {
			continue
		}
		fn, err := driver.ParseBaked(task.Spec)
		if err != nil {
			t.Fatalf("parse baked: %v", err)
		}
		baked[fn.Function] = fn
	}
	ev := baked["events"]
	if ev == nil || len(ev.Emits) != 2 || ev.Emits[1].Lane != driver.LaneEvent || ev.Emits[1].Message == nil || ev.Emits[1].Attributes["input"].Extract == nil {
		t.Fatalf("events listener baked = %+v, want the event emit with its lane, message and attributes", ev)
	}
	if ev.Unmatched != driver.UnmatchedLog {
		t.Fatalf("unmatched baked = %q, want log", ev.Unmatched)
	}
	lg := baked["logs"]
	if lg == nil || len(lg.Emits) != 1 || lg.Emits[0].Lane != driver.LaneLog || lg.Emits[0].Name != "" || lg.Emits[0].Log == nil || lg.Emits[0].Log.Source != "nvp" {
		t.Fatalf("logs listener baked = %+v, want a nameless log line emit", lg)
	}
	raw, _ := json.Marshal(lg.Emits[0])
	if strings.Contains(string(raw), `"name"`) || strings.Contains(string(raw), `"extract":{}`) {
		t.Fatalf("a log emit baked as %s, want no name and no empty extract", raw)
	}

	// The boot audit reads a spec carrying a nameless log emit, and names
	// nothing in it.
	vs, err := gw.WriterRulingViolations(ctx)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	for _, v := range vs {
		if strings.Contains(v.Where, "lane-proto") {
			t.Fatalf("the audit named %+v in a spec it should admit", v)
		}
	}
}
