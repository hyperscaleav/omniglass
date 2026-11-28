//go:build linux

// The simulator serves each device on its own 127.250.x.y address, which Linux
// treats as local without configuration.

package collection_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/simfleet"
	"github.com/hyperscaleav/omniglass/sim/simtest"
	"github.com/hyperscaleav/omniglass/sim/simtest/shipped"
)

// contractKnownGaps are the (product, driver, poll, signal) rows a driver reads
// that its simulated family does not answer yet. Each is a defect in the
// family or the driver, named by the issue that tracks it; the test fails a
// row that has started to resolve, so the list only ever shrinks. It is empty:
// every signal of every polled driver resolves against its family.
var contractKnownGaps = map[string]string{}

// contractSkip is a driver face the contract leaves unpolled, and why.
type contractSkip struct {
	product, driver, reason string
}

// TestEveryShippedDriverPollResolvesAgainstItsSimulatedFamily is the driver
// contract: for every product the boot seed ships that has a driver and that
// the simulator's catalog serves, each poll the driver declares is run through
// the real collection path (the real SNMP, HTTP(S) and line clients, over
// loopback, with the credentials the simulated fleet's own secrets hold)
// against that product's simulated device, and every metric and property
// signal the poll declares must come back with a value. A family that stops
// answering a field a driver reads fails here naming the product, driver, poll
// and signal.
//
// Everything is generated: the products and their drivers come from the seed,
// the family that serves a product from the shipped simulator catalog, the
// inputs and secrets of an attach from the simulated fleet's own plan, and the
// signals from each driver's spec. A product added to the seed is covered the
// day it ships, with no list to extend. This replaces the golden wire
// transcripts (#2826).
func TestEveryShippedDriverPollResolvesAgainstItsSimulatedFamily(t *testing.T) {
	cat := shipped.Catalog(t)
	lanes := contractLanes(t)
	secrets := map[string]map[string]string{}
	for _, s := range devseed.SimSecretSpecs() {
		secrets[s.Name] = s.Fields
	}

	type face struct {
		product, driver string
		spec            *driver.Spec
	}
	var faces []face
	var skips []contractSkip
	skip := func(product, drv, reason string) { skips = append(skips, contractSkip{product, drv, reason}) }

	products := map[string]string{} // product to its profile, the ones served
	for _, p := range seed.ShippedProducts() {
		if p.Driver == "" {
			skip(p.Name, "", "the product has no driver")
			continue
		}
		res, served := cat.ForProduct(p.Name)
		if !served {
			skip(p.Name, p.Driver, "no simulator family serves the product")
			continue
		}
		names := []string{p.Driver}
		if ctl, ok := simfleet.Control[p.Driver]; ok {
			names = append(names, ctl) // the device's second face
		}
		for _, name := range names {
			sp, err := seed.ShippedDriverSpec(name)
			if err != nil {
				t.Fatalf("%s: %v", p.Name, err)
			}
			switch {
			case len(sp.Polls) == 0:
				skip(p.Name, name, "the driver declares no poll (it listens or receives pushes)")
			case transport.IsPush(sp.Transport):
				skip(p.Name, name, "a push transport: the device sends, nothing polls it")
			case simfleet.Attach[name] == nil:
				t.Errorf("%s: driver %s polls over %s and its product is simulated, but the simulated fleet has no attach for it (sim/simfleet.Attach)", p.Name, name, sp.Transport)
			default:
				products[p.Name] = res.Definition.ID()
				faces = append(faces, face{p.Name, name, sp})
			}
		}
	}
	if len(faces) == 0 {
		t.Fatal("the contract covers no driver face: the seed and the simulator catalog share no polled product")
	}

	// Every covered product is served at once, each on a loopback address of
	// its own and the kernel's ports.
	engine := shipped.Engine(t)
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	addrs := map[string]string{}
	var snaps []sim.DeviceSnapshot
	for _, name := range sortedKeys(products) {
		addrs[name] = simtest.ClaimLoopback(t)
		snaps = append(snaps, sim.DeviceSnapshot{DeviceID: name, Hostname: name, LoopbackIP: addrs[name],
			ProfileName: products[name], Product: name, Version: 1,
			// Every device sits in a room: a service that speaks for rooms
			// (booking, recording) lists the room it sits in.
			SiteCode: "contract-site", Building: "contract-building", Room: "contract-room"})
	}
	if err := engine.Sync(snaps); err != nil {
		t.Fatalf("serve the shipped products: %v", err)
	}
	served := map[string]*sim.Device{}
	for _, d := range engine.Devices() {
		served[d.DeviceID] = d
	}

	runner := &collection.Runner{
		SNMP:      collection.NewSNMPGetter(),
		Line:      collection.NewLineExchanger(),
		HTTPFetch: collection.NewHTTPFetcher(),
	}
	var covered struct{ products, drivers, polls, signals int }
	coveredProducts := map[string]bool{}
	coveredDrivers := map[string]bool{}

	for _, f := range faces {
		f := f
		t.Run(f.product+"/"+f.driver, func(t *testing.T) {
			if !contractSupported(f.spec.Transport) {
				skip(f.product, f.driver, fmt.Sprintf("a %s poll rides an authenticated stream the outpost holds, which the contract does not build", f.spec.Transport))
				t.Skipf("%s rides a held session", f.spec.Transport)
			}
			dev := served[f.product]
			if dev == nil {
				t.Fatalf("%s: the simulator started no device", f.product)
			}
			target, inputs, err := contractTarget(f.spec, f.driver, addrs[f.product], dev)
			if err != nil {
				t.Fatalf("%s/%s: %v", f.product, f.driver, err)
			}
			task, err := contractTask(f.spec, inputs, secrets, target)
			if err != nil {
				t.Fatalf("%s/%s: %v", f.product, f.driver, err)
			}
			if f.spec.Transport == "tcp" && len(f.spec.Listeners) > 0 {
				task.Session, task.SessionOnly = contractHoldSession(t, f.spec, target), true
			}
			coveredProducts[f.product], coveredDrivers[f.driver] = true, true
			for _, poll := range f.spec.Polls {
				fn, err := contractBake(f.driver, poll, lanes)
				if err != nil {
					t.Fatalf("%s/%s/%s: %v", f.product, f.driver, poll.Name, err)
				}
				pt := task
				pt.Fn = fn
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				samples, faults := runner.CollectDriverPoll(ctx, pt)
				cancel()
				covered.polls++
				for _, e := range faults {
					t.Errorf("product %s, driver %s, poll %s: %v", f.product, f.driver, poll.Name, e)
				}
				// A signal resolves when every record the poll read carries it:
				// a poll over many records (a room list) would otherwise pass on
				// the one record that still answers.
				records := map[int]bool{}
				got := map[int]map[int]bool{} // emit index to the records it resolved in
				for _, s := range samples {
					records[s.Record] = true
					if got[s.Emit] == nil {
						got[s.Emit] = map[int]bool{}
					}
					got[s.Emit][s.Record] = true
				}
				for i, em := range fn.Emits {
					if em.Lane != driver.LaneMetric && em.Lane != driver.LaneProperty {
						continue // an event or a log line fires when something happens
					}
					covered.signals++
					key := f.product + "/" + f.driver + "/" + poll.Name + "/" + em.Name
					var missing []int
					for r := range records {
						if !got[i][r] {
							missing = append(missing, r)
						}
					}
					sort.Ints(missing)
					resolved := len(records) > 0 && len(missing) == 0
					if issue, known := contractKnownGaps[key]; known {
						if resolved {
							t.Errorf("%s resolves now: delete it from contractKnownGaps (%s)", key, issue)
						}
						continue
					}
					switch {
					case len(records) == 0:
						t.Errorf("product %s, driver %s, poll %s, signal %s: the simulated %s family answered nothing the poll reads", f.product, f.driver, poll.Name, em.Name, products[f.product])
					case len(missing) > 0:
						t.Errorf("product %s, driver %s, poll %s, signal %s: the simulated %s family left it out of record(s) %v of %d", f.product, f.driver, poll.Name, em.Name, products[f.product], missing, len(records))
					}
				}
			}
		})
	}

	covered.products, covered.drivers = len(coveredProducts), len(coveredDrivers)
	sort.Slice(skips, func(i, j int) bool {
		if skips[i].product != skips[j].product {
			return skips[i].product < skips[j].product
		}
		return skips[i].driver < skips[j].driver
	})
	for _, s := range skips {
		t.Logf("not polled: product %q driver %q: %s", s.product, s.driver, s.reason)
	}
	t.Logf("driver contract: %d products, %d drivers, %d polls, %d metric and property signals resolved against their simulated families; %d driver faces skipped",
		covered.products, covered.drivers, covered.polls, covered.signals, len(skips))
	if covered.products == 0 || covered.polls == 0 || covered.signals == 0 {
		t.Fatalf("the contract covered nothing: %+v", covered)
	}
}

// contractSupported reports the transports the contract polls: snmp, http and
// https, and a tcp line (over a held session where the driver listens, as the
// outpost holds one). A driver on a websocket rides an authenticated stream the
// outpost builds from its endpoint; it is skipped with that reason.
func contractSupported(tr string) bool {
	switch {
	case tr == "snmp", driver.IsHTTP(tr), tr == "tcp":
		return true
	}
	return false
}

// contractHoldSession holds the line session the outpost holds for a tcp
// driver that declares listeners (its poll borrows the held connection, as the
// sensor's banner would otherwise read as the answer), armed as its listeners
// ask, and waits for it to come up.
func contractHoldSession(t *testing.T, sp *driver.Spec, target string) *collection.Session {
	t.Helper()
	var arm []string
	for _, l := range sp.Listeners {
		arm = append(arm, l.Arm...)
	}
	sess := collection.StartSession(collection.SessionConfig{Target: target, Timeout: 5 * time.Second, Dialer: collection.NewLineSessionDialer(), Arm: arm})
	t.Cleanup(sess.Close)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := sess.State(); st == collection.SessionUp {
			return sess
		}
		time.Sleep(10 * time.Millisecond)
	}
	st, why := sess.State()
	t.Fatalf("the held line session to %s is %s (%s), want up", target, st, why)
	return nil
}

// contractLanes is each catalog name's lane, from the seed's own metric,
// property and event types: what attach bakes into a task.
func contractLanes(t *testing.T) map[string]string {
	t.Helper()
	raw, err := seed.FactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var facts struct {
		Metrics    []struct{ Name string } `json:"metric_types"`
		Properties []struct{ Name string } `json:"property_types"`
		Events     []struct{ Name string } `json:"event_types"`
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	lanes := map[string]string{}
	for _, m := range facts.Metrics {
		lanes[m.Name] = driver.LaneMetric
	}
	for _, p := range facts.Properties {
		lanes[p.Name] = driver.LaneProperty
	}
	for _, e := range facts.Events {
		lanes[e.Name] = driver.LaneEvent
	}
	return lanes
}

// contractBake is a poll as attach bakes it into a task spec: the request, the
// records and identity, and each emit with its catalog lane.
func contractBake(drv string, p driver.Poll, lanes map[string]string) (*driver.BakedFunction, error) {
	fn := &driver.BakedFunction{Driver: drv, Function: p.Name, Request: &p.Request, Records: p.Records, Identity: p.Identity}
	sched := p.Schedule
	fn.Schedule = &sched
	for _, em := range p.Emits {
		if em.Log != nil {
			fn.Emits = append(fn.Emits, driver.BakedEmit{Lane: driver.LaneLog, Message: em.Message, Attributes: em.Attributes, Log: em.Log, TS: em.TS})
			continue
		}
		lane, ok := lanes[em.Name]
		if !ok {
			return nil, fmt.Errorf("emit %q is in no seeded metric, property or event type", em.Name)
		}
		fn.Emits = append(fn.Emits, driver.BakedEmit{Name: em.Name, Lane: lane, Extract: em.Extract, Transform: em.Transform,
			Message: em.Message, Attributes: em.Attributes, TS: em.TS})
	}
	return fn, nil
}

// contractTarget is where the driver's attach points on the device: the
// simulated fleet's inputs for the driver (the ports the profiles declare) with
// the spec's defaults beneath them, and the address the kernel bound the
// declared port to.
func contractTarget(sp *driver.Spec, drv, addr string, dev *sim.Device) (string, map[string]string, error) {
	inputs := map[string]string{}
	for _, in := range sp.Inputs {
		if in.Default != "" {
			inputs[in.Name] = in.Default
		}
	}
	for k, v := range simfleet.Attach[drv](addr, func(p int) int { return p }) {
		inputs[k] = v
	}
	declared, err := strconv.Atoi(inputs["port"])
	if err != nil {
		return "", nil, fmt.Errorf("the attach names no port (%q) and the spec has no default", inputs["port"])
	}
	var seen []string
	for _, f := range dev.Faces() {
		if f.Declared == declared {
			return net.JoinHostPort(addr, strconv.Itoa(int(f.Addr.Port()))), inputs, nil
		}
		seen = append(seen, fmt.Sprintf("%s:%d", f.Protocol, f.Declared))
	}
	return "", nil, fmt.Errorf("the driver reads port %d, which the device serves no face on (it serves %s)", declared, strings.Join(seen, ", "))
}

// contractTask is the task a poll rides: the transport, the target, the
// unsealed secrets the attach's inputs name, the driver's session block, and
// for https the trust the endpoint's tls-mode input chooses.
func contractTask(sp *driver.Spec, inputs map[string]string, secrets map[string]map[string]string, target string) (collection.DriverPollTask, error) {
	task := collection.DriverPollTask{Transport: sp.Transport, Target: target, Timeout: 10 * time.Second, Auth: sp.Session}
	for _, in := range sp.Inputs {
		if in.Kind != "secret" {
			continue
		}
		ref, ok := inputs[in.Name]
		if !ok {
			if in.Required {
				return task, fmt.Errorf("the attach supplies no secret for required input %q", in.Name)
			}
			continue
		}
		fields, ok := secrets[ref]
		if !ok {
			return task, fmt.Errorf("input %q references secret %q, which the simulated fleet's secrets do not hold", in.Name, ref)
		}
		if task.Secrets == nil {
			task.Secrets = map[string]map[string]string{}
		}
		task.Secrets[in.Name] = fields
	}
	if driver.IsHTTP(sp.Transport) {
		task.HTTPState = collection.NewHTTPAuthState()
		if sp.Transport == "https" {
			mode, err := driver.ParseTLSMode(inputs[driver.TLSModeInput])
			if err != nil {
				return task, err
			}
			host, _, _ := net.SplitHostPort(target)
			task.TLS = &collection.TLSTrust{Mode: mode, ServerName: host}
			if mode == driver.TLSModePin {
				task.TLS.Pin = collection.NewHostKeyPin("")
			}
		}
	}
	return task, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
