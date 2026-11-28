package newtronpanel

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"html/template"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// dashboardTemplate is the panel's web page: markup only, the data side is the
// typed view below.
//
//go:embed dashboard.html.tmpl
var dashboardTemplate string

// dashboardScript is the page's script, a static asset spliced in as written
// (html/template would otherwise drop its comments).
//
//go:embed dashboard.js
var dashboardScript string

// dashboardNote is the markup comment that explains the page's one browser-only
// datapoint, spliced in as written for the same reason.
const dashboardNote = `<!-- WEB-DRIVER-ONLY teaching surface: room config version. The value
         is NOT in any API route, SNMP OID, or SSH ` + "`show`" + ` command, it
         ships ONLY as the base64 blob in data-cfgblob below. A plaintext
         ` + "`curl GET /`" + ` sees the encoded blob and the "loading…"
         placeholder, never the value; client JS atob()-decodes it into
         the DOM, so only a real (or headless-JS) browser renders it. -->`

var dashboard = template.Must(template.New("dashboard").Parse(dashboardTemplate))

// dashboardView is everything the page shows, read from the device when the
// page is asked for. The template names no state path.
type dashboardView struct {
	Hostname, IP, Model, Firmware, Serial string
	Scene, Source                         string
	Brightness, Occupancy                 string
	Uptime                                int64
	// ConfigBlob is the room's config version as the page carries it: base64,
	// decoded only by the page's script, so a plain fetch never sees the value.
	ConfigBlob string
	// Note and Script are the page's own markup and script, never device data.
	Note   template.HTML
	Script template.JS
}

func viewOf(d spec.Device) dashboardView {
	return dashboardView{
		Hostname: d.Hostname(), IP: d.IP(), Model: spec.String(d, pathModel), Firmware: spec.String(d, pathFirmware), Serial: spec.String(d, pathSerial),
		Scene: spec.String(d, pathScene), Source: spec.String(d, pathSource),
		Brightness: spec.String(d, pathBrightness), Occupancy: spec.String(d, pathOccupancy), Uptime: spec.Int64(d, pathUptime),
		ConfigBlob: base64.StdEncoding.EncodeToString([]byte(spec.String(d, pathConfigVersion))),
		Note:       template.HTML(dashboardNote), Script: template.JS(dashboardScript),
	}
}

// renderDashboard is the page for a view, or the error that kept it from
// rendering.
func renderDashboard(v dashboardView) ([]byte, error) {
	var buf bytes.Buffer
	if err := dashboard.Execute(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func dashboardRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	page, err := renderDashboard(viewOf(d))
	if err != nil {
		return spec.Failure(500, "template error")
	}
	return spec.HTTPResponse{ContentType: "text/html", Body: page}
}
