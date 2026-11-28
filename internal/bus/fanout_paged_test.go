package bus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/simtest/shipped"
)

// pagedConduit serves the simulated UCC platform's room list over http and
// counts the requests (and, when failOn is set, answers that request 503).
type pagedConduit struct {
	srv    *httptest.Server
	hits   atomic.Int64
	failOn atomic.Int64
}

func newPagedConduit(t *testing.T) *pagedConduit {
	t.Helper()
	e := shipped.Engine(t)
	if err := e.LoadDevices([]sim.DeviceSnapshot{{DeviceID: "p", Hostname: "p", LoopbackIP: "127.0.0.1", ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	dev := e.Devices()[0]
	c := &pagedConduit{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := c.hits.Add(1)
		if c.failOn.Load() == n {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		dev.ServeHTTP(w, r)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// poll runs the shipped ucc-rooms driver once against the platform, two rooms
// to a page, and returns the batch an outpost would publish for it.
func (c *pagedConduit) poll(t *testing.T) (*ogv1.TelemetryBatch, []error) {
	t.Helper()
	sp, err := seed.ShippedDriverSpec("ucc-rooms")
	if err != nil {
		t.Fatal(err)
	}
	poll := sp.Polls[0]
	req := poll.Request
	pg := *req.Paginate
	pg.Size = 2
	req.Paginate = &pg
	fn := &driver.BakedFunction{Driver: "ucc-rooms", Function: poll.Name, Request: &req, Records: poll.Records, Identity: poll.Identity}
	lanes := map[string]string{"call-state": driver.LaneProperty, "participant-count": driver.LaneMetric}
	for _, e := range poll.Emits {
		fn.Emits = append(fn.Emits, driver.BakedEmit{Name: e.Name, Lane: lanes[e.Name], Extract: e.Extract, Transform: e.Transform})
	}
	task := collection.DriverPollTask{Fn: fn, Transport: "http", Target: strings.TrimPrefix(c.srv.URL, "http://"), Timeout: 5 * time.Second,
		Auth: sp.Session, Secrets: map[string]map[string]string{"token": {"key": "ucc-lab-token"}}}
	samples, faults := (&collection.Runner{HTTPFetch: collection.NewHTTPFetcher()}).CollectDriverPoll(context.Background(), task)
	ev := &ogv1.TelemetryBatch{}
	for _, s := range samples {
		switch {
		case s.Name == "call-state":
			v, _ := json.Marshal(s.Text)
			ev.Properties = append(ev.Properties, &ogv1.PropertySample{Name: s.Name, ValueJson: string(v), Identity: collection.WireIdentity(s.Identity)})
		case s.Name == "participant-count":
			ev.Metrics = append(ev.Metrics, &ogv1.MetricSample{Name: s.Name, Value: s.Value, Identity: collection.WireIdentity(s.Identity)})
		}
	}
	return ev, faults
}

// TestOnePollFansEveryPageOut is #2789's integration acceptance: one poll of
// the shipped ucc-rooms driver walks every page of the platform's room list
// (five rooms, two to a page) and fans each page's records out to the system
// its identity is bound to; the room on the third page nobody bound waits in
// the conduit's inbox; and a poll whose middle page fails lands nothing.
func TestOnePollFansEveryPageOut(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newFanIngest(t)
	f.bind(t, "zr-auditorium-av", "room-a") // page 1
	f.bind(t, "zr-boardroom-a", "room-c")   // page 1
	f.bind(t, "zr-boardroom-b", "room-b")   // page 2
	f.bind(t, "zr-briefing-av", "room-c")   // page 2
	// zr-huddle-north is the only room on page 3 and is not bound.
	c := newPagedConduit(t)

	c.failOn.Store(2)
	ev, faults := c.poll(t)
	if len(faults) == 0 || !strings.Contains(faults[0].Error(), "page 2 failed") || !strings.Contains(faults[0].Error(), "/api/rooms") {
		t.Fatalf("a failed middle page faulted %v, want it named", faults)
	}
	subject, tick := f.outpostLane(ev)
	f.deliver(t, subject, tick, "")
	if rows := f.landed(t); len(rows) != 0 {
		t.Errorf("a poll with a failed page landed %v", rows)
	}
	if inbox, _ := f.gw.ListUnboundIdentities(f.ctx, "meetings-cloud", scope.Set{All: true}); len(inbox) != 0 {
		t.Errorf("a poll with a failed page left %+v in the inbox", inbox)
	}

	c.failOn.Store(0)
	c.hits.Store(0)
	ev, faults = c.poll(t)
	if len(faults) != 0 {
		t.Fatalf("faults %v", faults)
	}
	if n := c.hits.Load(); n != 3 {
		t.Errorf("the walk sent %d requests, want 3 pages", n)
	}
	subject, tick = f.outpostLane(ev)
	f.deliver(t, subject, tick, "")
	rows := strings.Join(f.landed(t), "\n")
	for _, want := range []string{
		"room-a call-state idle [] meetings-cloud/http observed",
		"room-b call-state active [] meetings-cloud/http observed",
		"room-b participant-count 2 [] meetings-cloud/http observed",
		"room-c call-state idle [] meetings-cloud/http observed",
	} {
		if !strings.Contains(rows, want) {
			t.Errorf("landed rows lack %q:\n%s", want, rows)
		}
	}
	inbox, err := f.gw.ListUnboundIdentities(f.ctx, "meetings-cloud", scope.Set{All: true})
	if err != nil || len(inbox) != 1 || inbox[0].Value != "zr-huddle-north" {
		t.Errorf("inbox = %+v (%v), want the page-3 room zr-huddle-north", inbox, err)
	}
}
