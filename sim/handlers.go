package sim

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// goHandlers is what a Go device family answers in Go (#2829), registered on
// its device beside whatever its interfaces declare. The protocol servers call
// these where a YAML profile's routes and commands would render a template.
type goHandlers struct {
	model   spec.Model
	animate spec.Animator
	// stop ends what animate started.
	stop func()
}

// addHandlers registers a family's routes and commands on the device and
// holds its animator for StartBehaviors. model is the row of the product the
// device simulates, the zero Model for a device of the family itself.
func (d *Device) addHandlers(h spec.Handlers, model spec.Model) error {
	d.fam = &goHandlers{model: model, animate: h.Animate}
	for _, r := range h.HTTP {
		method, path, err := parseMatch(r.Match)
		if err != nil {
			return err
		}
		d.routes = append(d.routes, routeEntry{Method: method, Path: path, Go: r.Handle})
	}
	for _, c := range h.Commands {
		d.commands = append(d.commands, commandEntry{Match: c.Match, Go: c.Handle})
	}
	return nil
}

// startAnimator runs the family's animator, once.
func (d *Device) startAnimator() {
	d.behaviorMu.Lock()
	defer d.behaviorMu.Unlock()
	if d.fam == nil || d.fam.animate == nil || d.fam.stop != nil {
		return
	}
	stop := d.fam.animate(goDevice{d})
	if stop == nil {
		stop = func() {}
	}
	d.fam.stop = stop
}

// stopAnimator stops what the animator started.
func (d *Device) stopAnimator() {
	d.behaviorMu.Lock()
	var stop func()
	if d.fam != nil {
		stop, d.fam.stop = d.fam.stop, nil
	}
	d.behaviorMu.Unlock()
	if stop != nil {
		stop()
	}
}

// goDevice is the device as a family's handler sees it (spec.Device).
type goDevice struct{ d *Device }

func (g goDevice) DeviceID() string { return g.d.DeviceID }
func (g goDevice) Hostname() string { return g.d.Hostname }
func (g goDevice) IP() string       { return g.d.LoopbackIP.String() }
func (g goDevice) Model() spec.Model {
	if g.d.fam == nil {
		return spec.Model{}
	}
	return g.d.fam.model
}
func (g goDevice) Read(path string) any  { return g.d.ReadPath(path) }
func (g goDevice) State() map[string]any { return g.d.State() }
func (g goDevice) Write(path string, v any) error {
	d := g.d
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := SetPath(d.state, path, v); err != nil {
		return fmt.Errorf("setting %q: %w", path, err)
	}
	diffs := map[string]any{path: v}
	d.Version++
	// Published under the lock, as a route's mutations are, so diffs reach the
	// publisher in version order.
	if d.DiffPublisher != nil {
		d.DiffPublisher(StateChange{DeviceID: d.DeviceID, Version: d.Version, Diffs: diffs, Cause: "handler"})
	}
	d.fanoutToSessions(diffs)
	return nil
}

// serveGoRoute answers a matched route with its Go handler. The handler runs
// without the device lock, reading and writing state through its handle.
func (d *Device) serveGoRoute(w http.ResponseWriter, r *http.Request, h spec.HTTPHandler, rc requestContext) {
	req := spec.HTTPRequest{Method: rc.Method, Path: rc.Path, Query: rc.Query, Body: rc.Body}
	if r.Body != nil {
		req.Raw, _ = io.ReadAll(io.LimitReader(r.Body, requestBodyLimit))
	}
	res := h(goDevice{d}, req)
	status := res.Status
	if status == 0 {
		status = http.StatusOK
	}
	var body []byte
	ct := res.ContentType
	switch b := res.Body.(type) {
	case nil:
	case string:
		body = []byte(b)
		if ct == "" {
			ct = "text/plain; charset=utf-8"
		}
	case []byte:
		body = b
		if ct == "" {
			ct = "application/octet-stream"
		}
	default:
		enc, err := json.Marshal(b)
		if err != nil {
			slog.Error("handler response not encoded", "device", d.DeviceID, "route", r.Method+" "+r.URL.Path, "error", err)
			http.Error(w, "response error", http.StatusInternalServerError)
			return
		}
		body = enc
		if ct == "" {
			ct = "application/json"
		}
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// runGoCommand answers a matched command with its Go handler.
func (d *Device) runGoCommand(ce commandEntry) CommandResult {
	return CommandResult{Response: ce.Go(goDevice{d})}
}
