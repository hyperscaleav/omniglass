package borealedge

import (
	"bytes"
	_ "embed"
	"html/template"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// dashboardTemplate is the web admin's page: markup only, the data side is the
// typed view below.
//
//go:embed dashboard.html.tmpl
var dashboardTemplate string

// dashboardScript is the page's script, a static asset spliced in as written
// (html/template would otherwise drop its comments).
//
//go:embed dashboard.js
var dashboardScript string

var dashboard = template.Must(template.New("dashboard").Parse(dashboardTemplate))

// dashboardView is everything the page shows, read from the device when the
// page is asked for. The template names no state path.
type dashboardView struct {
	Hostname, IP, Model, Serial, Firmware string
	Power, Input                          string
	Brightness, Volume, Temperature       float64
	Resolution                            string
	Muted                                 bool
	Uptime                                int64
	// Script is the page's own script, never device data.
	Script template.JS
}

func viewOf(d spec.Device) dashboardView {
	return dashboardView{
		Hostname: d.Hostname(), IP: d.IP(), Model: spec.String(d, pathModel), Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware),
		Power: spec.String(d, pathPower), Input: spec.String(d, pathInput),
		Brightness: spec.Float(d, pathBrightness), Volume: spec.Float(d, pathVolume), Temperature: spec.Float(d, pathTemperature),
		Resolution: spec.String(d, pathResolution), Muted: spec.Bool(d, pathMute), Uptime: spec.Int64(d, pathUptime),
		Script: template.JS(dashboardScript),
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
