package profile

import (
	"strings"
	"testing"
	"testing/fstest"
)

const familyProfile = `name: family-a
model: "A"
interfaces: []
products:
  - name: family-b
    defaults:
      configuration: { device: { model: "B" } }
defaults:
  configuration: { device: { model: "A", serial: "" } }
`

// A profile serves its own name and the products it lists, each listed
// product with an overlay on its defaults.
func TestStoreResolvesAProductToItsProfile(t *testing.T) {
	store, err := LoadStoreFS(fstest.MapFS{"devices/family-a.yaml": {Data: []byte(familyProfile)}})
	if err != nil {
		t.Fatal(err)
	}
	if name, overlay, ok := store.ForProduct("family-a"); !ok || name != "family-a" || overlay != nil {
		t.Fatalf("own name: %q %v %v", name, overlay, ok)
	}
	name, overlay, ok := store.ForProduct("family-b")
	if !ok || name != "family-a" {
		t.Fatalf("listed product: %q %v", name, ok)
	}
	if got := overlay["configuration"].(map[string]any)["device"].(map[string]any)["model"]; got != "B" {
		t.Fatalf("overlay model = %v", got)
	}
	if _, _, ok := store.ForProduct("family-c"); ok {
		t.Fatal("an unlisted product resolved")
	}
}

// A product resolves to exactly one profile: listing it twice, or listing a
// name another profile already is, fails to load.
func TestStoreRejectsAnAmbiguousProduct(t *testing.T) {
	other := "name: other\nmodel: O\ninterfaces: []\nproducts:\n  - name: %s\n"
	for _, dup := range []string{"family-b", "family-a"} {
		_, err := LoadStoreFS(fstest.MapFS{
			"devices/family-a.yaml": {Data: []byte(familyProfile)},
			"devices/other.yaml":    {Data: []byte(strings.Replace(other, "%s", dup, 1))},
		})
		if err == nil || !strings.Contains(err.Error(), dup) {
			t.Errorf("product %q claimed twice: err %v", dup, err)
		}
	}
}

const surfaceFragment = `subjects:
  - { match: "status.video.input", path: "status.video.input", publish_token: "INPUT", push_format: "EVT INPUT {{ .Raw }}" }
commands:
  - { match: "GET INPUT", response: "INPUT x" }
`

func includingProfile(name, own string) string {
	return "name: " + name + "\nmodel: M\ninterfaces:\n  - name: nvp\n    protocol: tcp\n    port: 51325\n    include: [nvp-room]\n" + own
}

// A surface is defined once, in surfaces/, and an interface that includes it
// carries its subjects and commands ahead of its own: two profiles share one
// definition rather than each copying it.
func TestInterfaceIncludesASharedSurface(t *testing.T) {
	store, err := LoadStoreFS(fstest.MapFS{
		"surfaces/nvp-room.yaml": {Data: []byte(surfaceFragment)},
		"devices/small.yaml":     {Data: []byte(includingProfile("small", ""))},
		"devices/large.yaml":     {Data: []byte(includingProfile("large", "    commands:\n      - { match: \"GET PROGRAM\", response: \"PROGRAM running\" }\n"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	matches := func(name string) (cmds, subs []string) {
		iface := store.Devices[name].Interfaces[0]
		for _, c := range iface.Commands {
			cmds = append(cmds, c.Match)
		}
		for _, s := range iface.Subjects {
			subs = append(subs, s.Match)
		}
		return
	}
	if cmds, subs := matches("small"); strings.Join(cmds, ",") != "GET INPUT" || strings.Join(subs, ",") != "status.video.input" {
		t.Errorf("small: commands %v, subjects %v, want the shared surface alone", cmds, subs)
	}
	if cmds, _ := matches("large"); strings.Join(cmds, ",") != "GET INPUT,GET PROGRAM" {
		t.Errorf("large: commands %v, want the shared surface, then its own", cmds)
	}
}

// An include that names no surface, or an interface that restates a command
// the surface already defines, fails to load: the second definition would be
// a copy that can drift.
func TestInterfaceIncludeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		files      fstest.MapFS
	}{
		{"an unknown surface", "nvp-room", fstest.MapFS{
			"devices/a.yaml": {Data: []byte(includingProfile("a", ""))},
		}},
		{"a restated command", "GET INPUT", fstest.MapFS{
			"surfaces/nvp-room.yaml": {Data: []byte(surfaceFragment)},
			"devices/a.yaml":         {Data: []byte(includingProfile("a", "    commands:\n      - { match: \"GET INPUT\", response: \"INPUT y\" }\n"))},
		}},
		{"a restated subject", "status.video.input", fstest.MapFS{
			"surfaces/nvp-room.yaml": {Data: []byte(surfaceFragment)},
			"devices/a.yaml":         {Data: []byte(includingProfile("a", "    subjects:\n      - { match: \"status.video.input\", path: \"status.video.input\" }\n"))},
		}},
	} {
		_, err := LoadStoreFS(tc.files)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming %q", tc.name, err, tc.want)
		}
	}
}
