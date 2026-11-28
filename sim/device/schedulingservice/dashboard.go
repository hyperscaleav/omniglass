package schedulingservice

import (
	"bytes"
	_ "embed"
	"html/template"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// dashboardTemplate is the dashboard's markup only; the data side is the typed
// view below.
//
//go:embed dashboard.html.tmpl
var dashboardTemplate string

// dashboardScript is the page's script, a static asset spliced in as written
// (html/template would otherwise drop its comments).
//
//go:embed dashboard.js
var dashboardScript string

var dashboard = template.Must(template.New("dashboard").Parse(dashboardTemplate))

// dashboardView is everything the page shows, read from the device when the page
// is asked for. The template names no state path.
type dashboardView struct {
	Hostname, IP, Version                     string
	Today, Remaining, InProgress, RoomsServed int64
	SyncOK                                    bool
	Uptime                                    int64
	// Script is the page's own script, never device data.
	Script template.JS
}

func viewOf(d spec.Device) dashboardView {
	return dashboardView{
		Hostname: d.Hostname(), IP: d.IP(), Version: spec.String(d, pathFirmware),
		Today: spec.Int64(d, pathToday), Remaining: spec.Int64(d, pathRemaining), InProgress: spec.Int64(d, pathProgress),
		RoomsServed: spec.Int64(d, pathServed), SyncOK: spec.Bool(d, pathSyncOK), Uptime: spec.Int64(d, pathUptime),
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
