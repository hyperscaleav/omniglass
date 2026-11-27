package spectest

import (
	"testing/fstest"

	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// TwinName is the YAML profile written to be the family's twin: the same
// identity, interfaces, ports, routes, commands, state and model sizes, as a
// profile. A consumer that resolves the two kinds alike returns the same
// answer for a product of each.
const TwinName = "testfamily-display-yaml"

// Pair is a product of the family and its twin on the YAML profile.
type Pair struct{ Go, YAML string }

// Pairs are the products of each kind, row for row.
var Pairs = []Pair{
	{Go: "tf-display-55", YAML: "tf-yaml-55"},
	{Go: "tf-display-65", YAML: "tf-yaml-65"},
}

const twinYAML = `
name: testfamily-display-yaml
manufacturer: Testfamily
model: Display
device_type: display
doc:
  summary: A display served by Go handlers
interfaces:
  - name: api
    protocol: http
    port: 8080
    doc: { summary: Display API }
    routes:
      - match: "GET /api/info"
        template: api/info.json.gotempl
        doc: { summary: The display and its state }
      - match: "POST /api/power"
        template: api/power.json.gotempl
        doc: { summary: Switch the display on or off }
        example: '{"power":"off"}'
  - name: console
    protocol: telnet
    port: 2323
    auth: { permissive: true }
    commands:
      - match: status
        response: "power={{ .State.status.device.power }}"
        doc: { summary: "The display's power" }
  - name: panel
    protocol: tcp
    port: 5000
    persistent: true
defaults:
  status:
    device: { power: "on" }
    video: { input: hdmi1 }
    room: { scene: idle }
  configuration:
    firmware: { version: "1.2.3" }
    device: { diagonal: 0 }
products:
  - name: tf-yaml-55
    defaults: { configuration: { device: { diagonal: 55 } } }
  - name: tf-yaml-65
    defaults: { configuration: { device: { diagonal: 65 } } }
`

// Store is the family's YAML twin as the loader reads it.
func Store() (*profile.Store, error) {
	return profile.LoadStoreFS(fstest.MapFS{"devices/" + TwinName + ".yaml": {Data: []byte(twinYAML)}})
}

// Catalog is the family and its YAML twin together, as the simulator holds
// the two kinds during the migration.
func Catalog() (*spec.Catalog, error) {
	store, err := Store()
	if err != nil {
		return nil, err
	}
	return spec.NewCatalog(store, Spec())
}
