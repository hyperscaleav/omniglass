package simnode

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// subscriber points the simulated services at their webhook endpoints
// (ADR-1370). The server hands the simulator a device's subscription (the
// endpoint's path and how posts are signed) on the device list; the
// simulator calls the device's own subscribe route with the server URL it
// reaches plus that path, as the service's admin would. A device is
// subscribed once per instance and subscription: a re-pull with nothing
// changed sends nothing, and a restarted device, whose state began again
// from its profile's defaults, is subscribed afresh.
type subscriber struct {
	server  string
	store   *profile.Store
	held    map[*sim.Device]collection.SimWebhook
	operate func(*sim.Device, sim.Operation) sim.OperationResult
}

func newSubscriber(server string, store *profile.Store) *subscriber {
	return &subscriber{
		server: strings.TrimRight(server, "/"), store: store,
		held:    map[*sim.Device]collection.SimWebhook{},
		operate: func(d *sim.Device, op sim.Operation) sim.OperationResult { return d.Operate(op) },
	}
}

// reconcile subscribes each served device the list hands a subscription to,
// and forgets the devices no longer served. It returns what failed, so the
// caller logs it; a failed subscribe is retried on the next pull.
func (s *subscriber) reconcile(listed []collection.SimDevice, served []*sim.Device) []error {
	want := map[string]collection.SimWebhook{}
	for _, d := range listed {
		if d.Webhook != nil {
			want[d.ComponentID] = *d.Webhook
		}
	}
	live := map[*sim.Device]bool{}
	var errs []error
	for _, d := range served {
		live[d] = true
		hook, ok := want[d.DeviceID]
		if !ok {
			continue
		}
		if had, done := s.held[d]; done && had == hook {
			continue
		}
		p := s.store.Devices[d.ProfileName]
		if p == nil || p.Webhook == nil {
			errs = append(errs, fmt.Errorf("%s holds a webhook endpoint, but its profile %q declares no subscribe route", d.Hostname, d.ProfileName))
			continue
		}
		body, err := json.Marshal(map[string]string{"url": s.server + hook.Path, "scheme": hook.Scheme, "secret": hook.Secret})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		res := s.operate(d, sim.Operation{Kind: "http", Method: http.MethodPost, Path: p.Webhook.Subscribe, Body: string(body)})
		if res.Error != "" || res.Status >= 300 {
			errs = append(errs, fmt.Errorf("subscribe %s to its webhook endpoint: status %d %s", d.Hostname, res.Status, res.Error))
			continue
		}
		s.held[d] = hook
	}
	for d := range s.held {
		if !live[d] {
			delete(s.held, d)
		}
	}
	return errs
}
