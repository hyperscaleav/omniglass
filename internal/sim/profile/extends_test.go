package profile

import (
	"strings"
	"testing"
	"testing/fstest"
)

const extendsBase = `name: base-panel
manufacturer: N
model: "Panel"
device_type: panel
slug_prefix: panel
interfaces:
  - name: api
    protocol: http
    port: 80
    routes:
      - { match: "GET /status", template: t/status }
  - name: line
    protocol: tcp
    port: 5000
    persistent: true
    subjects:
      - { match: "status.input", path: "status.input", publish_token: INPUT }
    commands:
      - { match: "GET INPUT", response: "INPUT x" }
behaviors:
  - { name: drift, schedule: { interval: 60s }, mutations: [{ path: status.level, value: "1" }] }
computed:
  - { path: status.device.uptime, kind: elapsed, since: status.device.boot_epoch }
defaults:
  status: { input: hdmi1, level: 1, device: { boot_epoch: 0 } }
  configuration: { device: { model: "Panel" } }
`

const extendsProcessor = `name: processor
extends: base-panel
manufacturer: N
model: "Core"
device_type: control-processor
slug_prefix: core
interfaces:
  - name: line
    commands:
      - { match: "GET PROGRAM", response: "PROGRAM running" }
    subjects:
      - { match: "status.program", path: "status.program", publish_token: PROGRAM }
  - name: ports
    protocol: tcp
    port: 6000
behaviors:
  - { name: load, schedule: { interval: 30s }, mutations: [{ path: status.cpu, value: "2" }] }
defaults:
  status: { program: running }
  configuration: { device: { model: "Core" } }
`

func loadExtends(t *testing.T, files map[string]string) (*Store, error) {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys["devices/"+name+".yaml"] = &fstest.MapFile{Data: []byte(body)}
	}
	return LoadStoreFS(fsys)
}

func ifaceNamed(p *DeviceProfile, name string) *Interface {
	for i := range p.Interfaces {
		if p.Interfaces[i].Name == name {
			return &p.Interfaces[i]
		}
	}
	return nil
}

// A profile that extends another carries the base's whole surface and adds
// its own: the base's interfaces in order, an interface it names again
// gaining its commands and subjects (its protocol and port kept), interfaces
// of its own after; behaviors and computed fields appended; defaults deep
// merged with its own winning; its identity its own.
func TestAProfileExtendsAnothersSurface(t *testing.T) {
	store, err := loadExtends(t, map[string]string{"base-panel": extendsBase, "processor": extendsProcessor})
	if err != nil {
		t.Fatal(err)
	}
	p := store.Devices["processor"]
	if p.Model != "Core" || p.DeviceType != "control-processor" {
		t.Errorf("identity = %q %q, want its own", p.Model, p.DeviceType)
	}
	var names []string
	for _, i := range p.Interfaces {
		names = append(names, i.Name)
	}
	if got := strings.Join(names, ","); got != "api,line,ports" {
		t.Fatalf("interfaces = %s, want the base's then its own", got)
	}
	if api := ifaceNamed(p, "api"); len(api.Routes) != 1 || api.Port != 80 {
		t.Errorf("inherited api = %+v", api)
	}
	line := ifaceNamed(p, "line")
	if line.Protocol != "tcp" || line.Port != 5000 || !line.Persistent {
		t.Errorf("the extended line face lost its own setup: %+v", line)
	}
	var cmds []string
	for _, c := range line.Commands {
		cmds = append(cmds, c.Match)
	}
	if got := strings.Join(cmds, ","); got != "GET INPUT,GET PROGRAM" {
		t.Errorf("line commands = %s, want the base's then its own", got)
	}
	if len(line.Subjects) != 2 {
		t.Errorf("line subjects = %+v, want both", line.Subjects)
	}
	if len(p.Behaviors) != 2 || len(p.Computed) != 1 {
		t.Errorf("behaviors %d computed %d, want 2 and 1", len(p.Behaviors), len(p.Computed))
	}
	st := p.Defaults["status"].(map[string]any)
	if st["input"] != "hdmi1" || st["program"] != "running" {
		t.Errorf("status defaults = %v, want the base's and its own", st)
	}
	if m := p.Defaults["configuration"].(map[string]any)["device"].(map[string]any)["model"]; m != "Core" {
		t.Errorf("model default = %v, want its own", m)
	}

	// The base is untouched by what extends it.
	base := store.Devices["base-panel"]
	if len(base.Interfaces) != 2 || len(ifaceNamed(base, "line").Commands) != 1 || len(base.Behaviors) != 1 {
		t.Errorf("the base changed: %+v", base)
	}
	if _, ok := base.Defaults["status"].(map[string]any)["program"]; ok {
		t.Error("the base gained its extender's defaults")
	}
}

// An extends that names no profile, names itself through a cycle, or names
// again an interface of another protocol is refused at load.
func TestExtendsRefusesWhatItCannotResolve(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"missing", map[string]string{"processor": extendsProcessor}, `extends "base-panel", which is no profile`},
		{"cycle", map[string]string{
			"a": "name: a\nextends: b\ninterfaces: []\n",
			"b": "name: b\nextends: a\ninterfaces: []\n",
		}, "extends itself"},
		{"protocol", map[string]string{"base-panel": extendsBase, "processor": strings.Replace(extendsProcessor,
			"  - name: line\n    commands:", "  - name: line\n    protocol: udp\n    commands:", 1)}, `changes inherited interface "line"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadExtends(t, tc.files)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one saying %q", err, tc.want)
			}
		})
	}
}

// A chain resolves whole whatever order the store meets it in: C extends B
// extends A, and C carries A's faces, B's additions and its own, on every
// load.
func TestAnExtendsChainResolvesWhole(t *testing.T) {
	files := map[string]string{
		"base-panel": extendsBase,
		"processor":  extendsProcessor,
		"big-processor": `name: big-processor
extends: processor
model: "Core 5"
interfaces:
  - name: line
    commands:
      - { match: "GET RACK", response: "RACK 2" }
`,
	}
	for i := 0; i < 50; i++ {
		store, err := loadExtends(t, files)
		if err != nil {
			t.Fatal(err)
		}
		p := store.Devices["big-processor"]
		line := ifaceNamed(p, "line")
		if line == nil || line.Protocol != "tcp" {
			t.Fatalf("load %d: the chain's line face = %+v, want the base's tcp face", i, line)
		}
		var cmds []string
		for _, c := range line.Commands {
			cmds = append(cmds, c.Match)
		}
		if got := strings.Join(cmds, ","); got != "GET INPUT,GET PROGRAM,GET RACK" {
			t.Fatalf("load %d: line commands = %s, want each level's in order", i, got)
		}
		if ifaceNamed(p, "api") == nil || ifaceNamed(p, "ports") == nil {
			t.Fatalf("load %d: the chain lost a face: %+v", i, p.Interfaces)
		}
	}
}

// An inherited face named again may restate its doc, and a route or command
// it declares with an inherited match replaces that one in place, so the
// extender says what the face is on it; its protocol and port stay locked.
func TestAnExtenderRestatesAnInheritedFace(t *testing.T) {
	store, err := loadExtends(t, map[string]string{"base-panel": extendsBase, "processor": strings.Replace(extendsProcessor,
		"  - name: line\n    commands:\n      - { match: \"GET PROGRAM\", response: \"PROGRAM running\" }",
		"  - name: line\n    doc: { summary: \"the processor's line\" }\n    commands:\n      - { match: \"GET INPUT\", response: \"INPUT y\" }\n      - { match: \"GET PROGRAM\", response: \"PROGRAM running\" }", 1)})
	if err != nil {
		t.Fatal(err)
	}
	line := ifaceNamed(store.Devices["processor"], "line")
	if line.Doc.Summary != "the processor's line" {
		t.Errorf("doc = %q, want the extender's", line.Doc.Summary)
	}
	var got []string
	for _, c := range line.Commands {
		got = append(got, c.Match+"="+c.Response)
	}
	if s := strings.Join(got, ","); s != "GET INPUT=INPUT y,GET PROGRAM=PROGRAM running" {
		t.Errorf("commands = %s, want GET INPUT replaced in place", s)
	}
	if base := ifaceNamed(store.Devices["base-panel"], "line"); base.Commands[0].Response != "INPUT x" || base.Doc.Summary != "" {
		t.Errorf("the base changed: %+v", base)
	}
}

// A face named again that the base does not have (a typo, or a face the
// base renamed) is refused, not served as a face with no protocol; and a
// behavior that writes an inherited computed path is refused as one on the
// base would be.
func TestExtendsRefusesAnOrphanFaceAndAComputedWrite(t *testing.T) {
	_, err := loadExtends(t, map[string]string{"base-panel": extendsBase, "processor": strings.Replace(extendsProcessor,
		"  - name: line\n    commands:", "  - name: lien\n    commands:", 1)})
	if err == nil || !strings.Contains(err.Error(), `"lien"`) {
		t.Errorf("an orphan face: err = %v, want one naming it", err)
	}
	_, err = loadExtends(t, map[string]string{"base-panel": extendsBase, "processor": strings.Replace(extendsProcessor,
		"mutations: [{ path: status.cpu, value: \"2\" }]", "mutations: [{ path: status.device.uptime, value: \"2\" }]", 1)})
	if err == nil || !strings.Contains(err.Error(), "status.device.uptime") {
		t.Errorf("a write to an inherited computed path: err = %v", err)
	}
}
