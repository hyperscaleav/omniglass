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
//
// A platform account (#1747) also learns the rooms bound on its endpoint:
// the simulator adds each identity the list hands it through the profile's
// provision route, once per instance, so a newly bound room is added on the
// next pull and a restarted device has every room added again.
type subscriber struct {
	server  string
	store   *profile.Store
	held    map[*sim.Device]subscription
	rooms   map[*sim.Device]map[string]bool
	operate func(*sim.Device, sim.Operation) sim.OperationResult
}

// subscription is what a subscribe call set on a device: a changed one is
// sent again.
type subscription struct{ path, scheme, secret string }

func newSubscriber(server string, store *profile.Store) *subscriber {
	return &subscriber{
		server: strings.TrimRight(server, "/"), store: store,
		held:    map[*sim.Device]subscription{},
		rooms:   map[*sim.Device]map[string]bool{},
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
		p := s.store.Devices[d.ProfileName]
		if p == nil || p.Webhook == nil {
			errs = append(errs, fmt.Errorf("%s holds a webhook endpoint, but its profile %q declares no subscribe route", d.Hostname, d.ProfileName))
			continue
		}
		sub := subscription{hook.Path, hook.Scheme, hook.Secret}
		if had, done := s.held[d]; !done || had != sub {
			if err := s.subscribe(d, p.Webhook.Subscribe, hook); err != nil {
				errs = append(errs, err)
				continue
			}
			s.held[d] = sub
		}
		errs = append(errs, s.provision(d, p.Webhook.Provision, hook.Identities)...)
	}
	for d := range s.held {
		if !live[d] {
			delete(s.held, d)
		}
	}
	for d := range s.rooms {
		if !live[d] {
			delete(s.rooms, d)
		}
	}
	return errs
}

// subscribe calls the device's subscribe route with the endpoint's URL and
// signing.
func (s *subscriber) subscribe(d *sim.Device, route string, hook collection.SimWebhook) error {
	body, err := json.Marshal(map[string]string{"url": s.server + hook.Path, "scheme": hook.Scheme, "secret": hook.Secret})
	if err != nil {
		return err
	}
	res := s.operate(d, sim.Operation{Kind: "http", Method: http.MethodPost, Path: route, Body: string(body)})
	if res.Error != "" || res.Status >= 300 {
		return fmt.Errorf("subscribe %s to its webhook endpoint: status %d %s", d.Hostname, res.Status, res.Error)
	}
	return nil
}

// provision adds each bound identity the device has not been given yet
// through its profile's provision route. A failed one is retried on the
// next pull.
func (s *subscriber) provision(d *sim.Device, route string, ids []string) []error {
	if len(ids) == 0 {
		return nil
	}
	if route == "" {
		return []error{fmt.Errorf("%s has %d bound identities, but its profile %q declares no provision route", d.Hostname, len(ids), d.ProfileName)}
	}
	added := s.rooms[d]
	if added == nil {
		added = map[string]bool{}
		s.rooms[d] = added
	}
	var errs []error
	for _, id := range ids {
		if added[id] {
			continue
		}
		body, err := json.Marshal(map[string]string{"id": id})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		res := s.operate(d, sim.Operation{Kind: "http", Method: http.MethodPost, Path: route, Body: string(body)})
		if res.Error != "" || res.Status >= 300 {
			errs = append(errs, fmt.Errorf("add %s to %s: status %d %s", id, d.Hostname, res.Status, res.Error))
			continue
		}
		added[id] = true
	}
	return errs
}
