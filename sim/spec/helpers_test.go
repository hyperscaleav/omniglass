package spec_test

import (
	"testing"
	"testing/fstest"

	"github.com/hyperscaleav/omniglass/sim/profile"
)

// loadYAML is the YAML half of the catalog as the loader builds it, with the
// product index a profile's products: list makes.
func loadYAML(t *testing.T) *profile.Store {
	t.Helper()
	fsys := fstest.MapFS{"devices/yaml-display.yaml": {Data: []byte(`
name: yaml-display
manufacturer: Acme
model: Y1
device_type: display
doc: { summary: "A YAML display" }
interfaces:
  - name: api
    protocol: http
    port: 80
defaults:
  status: { power: "on" }
products:
  - name: yaml-display-big
    defaults: { size: 65 }
`)}}
	s, err := profile.LoadStoreFS(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
