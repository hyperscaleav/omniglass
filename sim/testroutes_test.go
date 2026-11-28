package sim

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// serveRoute registers a route on a bare device as a family's handler would
// answer it, so a test of a face (auth, listeners, observation) needs no
// family behind the device.
func serveRoute(t testing.TB, d *Device, match string, h spec.HTTPHandler) {
	t.Helper()
	method, path, err := parseMatch(match)
	if err != nil {
		t.Fatal(err)
	}
	d.routes = append(d.routes, routeEntry{Method: method, Path: path, Go: h})
}

// serveText answers a route with a fixed body.
func serveText(t testing.TB, d *Device, match, contentType, body string) {
	t.Helper()
	serveRoute(t, d, match, func(spec.Device, spec.HTTPRequest) spec.HTTPResponse {
		return spec.HTTPResponse{ContentType: contentType, Body: body}
	})
}

// serveLine answers a line command with a fixed response.
func serveLine(d *Device, match, response string) {
	d.commands = append(d.commands, commandEntry{Match: match, Go: func(spec.Device) string { return response }})
}
