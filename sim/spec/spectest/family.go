// Package spectest is a small Go device family for tests (#2829): a display
// whose two sizes are rows of one table, served over HTTP and Telnet by Go
// handlers. It proves the engine, the catalog and every consumer of the
// simulator catalog treat a family's product as they treat a YAML profile's,
// without converting a real profile. It is built only by tests that import it;
// the family registry (sim/device) never lists it.
package spectest

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/nvp"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// Name is the family's id, the profile name its devices carry.
const Name = "testfamily-display"

// Row is the family's own typed model row.
type Row struct {
	Product  string
	Diagonal int
	PowerW   int
}

// Rows is the family's model table.
var Rows = []Row{
	{Product: "tf-display-55", Diagonal: 55, PowerW: 120},
	{Product: "tf-display-65", Diagonal: 65, PowerW: 180},
}

// Info is what GET /api/info answers: a typed value the engine encodes.
type Info struct {
	Product  string `json:"product"`
	Diagonal int    `json:"diagonal"`
	PowerW   int    `json:"power_w"`
	Power    string `json:"power"`
	Host     string `json:"host"`
	Firmware string `json:"firmware"`
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Testfamily", Model: "Display", DeviceType: "display",
			Summary: "A display served by Go handlers",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{
				Product: r.Product, Model: fmt.Sprintf("Display %d", r.Diagonal),
				State: map[string]any{"configuration": map[string]any{"device": map[string]any{"diagonal": r.Diagonal}}},
			}
		}),
		Interfaces: []wire.Interface{
			{Name: "api", Protocol: "http", Port: 8080, Doc: wire.Doc{Summary: "Display API"}},
			{Name: "console", Protocol: "telnet", Port: 2323, Auth: wire.Auth{Permissive: true}},
			{Name: "panel", Protocol: "tcp", Port: 5000, Persistent: true},
		},
		State: map[string]any{
			"status": map[string]any{
				"device": map[string]any{"power": "on"},
				"video":  map[string]any{"input": "hdmi1"},
				"room":   map[string]any{"scene": "idle"},
			},
			"configuration": map[string]any{"firmware": map[string]any{"version": "1.2.3"}, "device": map[string]any{"diagonal": 0}},
		},
		HTTP: []spec.HTTPRoute{
			{Match: "GET /api/info", Summary: "The display and its state", Handle: info},
			{Match: "POST /api/power", Summary: "Switch the display on or off", Example: `{"power":"off"}`, Handle: power},
		},
		Surface: panelSurface,
		Commands: []spec.Command{{Match: "status", Summary: "The display's power", Handle: func(d spec.Device) string {
			return fmt.Sprintf("power=%v\n", d.Read("status.device.power"))
		}}},
	}
}

// panelSurface serves the shared NVP room surface (sim/surface/nvp) on the
// panel face: the family keeps no typed copy of its own, so the surface's
// accessors read the device's state and the engine applies what a command
// reports it changed.
func panelSurface(d spec.Device) spec.Surface {
	read := func(path string) func() string {
		return func() string { v, _ := d.Read(path).(string); return v }
	}
	room := nvp.Room{
		Inputs: nvp.DefaultInputs, Scenes: nvp.DefaultScenes,
		Input: nvp.Field[string]{Get: read(nvp.PathInput), Set: func(string) {}},
		Scene: nvp.Field[string]{Get: read(nvp.PathScene), Set: func(string) {}},
	}
	return spec.Surface{
		Commanders: []surface.Commander{room},
		Subjects:   map[string][]surface.Subject{"panel": nvp.Subjects()},
	}
}

func info(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	row, _ := spec.Row[Row](d.Model())
	fw, _ := d.Read("configuration.firmware.version").(string)
	pw, _ := d.Read("status.device.power").(string)
	return spec.JSON(Info{Product: row.Product, Diagonal: row.Diagonal, PowerW: row.PowerW, Power: pw, Host: d.Hostname(), Firmware: fw})
}

func power(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	want, _ := r.Body["power"].(string)
	if want != "on" && want != "off" {
		return spec.Failure(400, "power is on or off")
	}
	if err := d.Write("status.device.power", want); err != nil {
		return spec.Failure(500, err.Error())
	}
	return spec.JSON(map[string]string{"power": want})
}

// Catalog is the family alone, as the simulator holds it.
func Catalog() (*spec.Catalog, error) { return spec.NewCatalog(Spec()) }

// Products are the family's products, one per row of its table.
var Products = []string{"tf-display-55", "tf-display-65"}
