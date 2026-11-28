package polarisdsp

import (
	"bytes"
	_ "embed"
	"html/template"
	"strings"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// dashboardTemplate is the page's markup: the data side is the typed view below.
//
//go:embed dashboard.html.tmpl
var dashboardTemplate string

// dashboardScript is the page's script, a static asset spliced in as written
// (html/template would otherwise drop its comments); the device's id, which
// keys where the page keeps the token an operator pastes, replaces its marker.
//
//go:embed dashboard.js
var dashboardScript string

var dashboard = template.Must(template.New("dashboard").Parse(dashboardTemplate))

// dashboardView is everything the page shows, read from the device when the
// page is asked for. The template names no state path.
type dashboardView struct {
	Hostname, IP, Model, Serial, Firmware string
	ActiveChannels                        int
	CPULoad, Temperature, FanRPM          float64
	Uptime                                int64
	Channels                              []dashboardChannel
	// Script is the page's own script, never device data.
	Script template.JS
}

// dashboardChannel is one row of the channel mixer.
type dashboardChannel struct {
	Index        int
	Name         string
	Gain, PeakDB float64
	Muted        bool
}

func viewOf(d spec.Device) dashboardView {
	v := dashboardView{
		Hostname: d.Hostname(), IP: d.IP(), Model: base().Model, Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware),
		ActiveChannels: spec.Int(d, pathActive), CPULoad: spec.Float(d, pathCPU), Temperature: spec.Float(d, pathTemperature), FanRPM: spec.Float(d, pathFan),
		Uptime: spec.Int64(d, pathUptime),
		Script: template.JS(strings.ReplaceAll(dashboardScript, "__DEVICE_ID__", template.JSEscapeString(d.DeviceID()))),
	}
	if r, ok := spec.Row[Row](d.Model()); ok {
		v.Model = r.Model
	}
	list, _ := d.Read(listChannels).([]any)
	for i := range list {
		v.Channels = append(v.Channels, dashboardChannel{
			Index: spec.Int(d, channelPath(i, "index")), Name: spec.String(d, channelPath(i, "name")),
			Gain: spec.Float(d, channelPath(i, "gain")), PeakDB: spec.Float(d, channelPath(i, "peak_db")), Muted: spec.Bool(d, channelPath(i, "mute")),
		})
	}
	return v
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
