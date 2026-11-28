package uccplatform

import (
	"bytes"
	_ "embed"
	"html/template"
	"sort"

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

// dashboardRoom is one row of the page: a room's id, name and state.
type dashboardRoom struct{ ID, Name, State string }

// dashboardView is everything the page shows, read from the device when the page is
// asked for. The template names no state path.
type dashboardView struct {
	Hostname string
	Rooms    []dashboardRoom
	// Script is the page's own script, never device data.
	Script template.JS
}

func viewOf(d spec.Device) dashboardView {
	v := dashboardView{Hostname: d.Hostname(), Script: template.JS(dashboardScript)}
	rooms, _ := d.Read(pathRooms).(map[string]any)
	ids := make([]string, 0, len(rooms))
	for id := range rooms {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r, _ := rooms[id].(map[string]any)
		v.Rooms = append(v.Rooms, dashboardRoom{ID: id, Name: orText(r["name"], id), State: orText(r["state"], "idle")})
	}
	return v
}

// renderDashboard is the page for a view, or the error that kept it from rendering.
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
