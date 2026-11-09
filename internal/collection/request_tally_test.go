package collection_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/nettest"
	"github.com/hyperscaleav/omniglass/internal/sim"
)

// The node's request tally is the node half of the closed loop the scale lab
// reconciles against what each simulated endpoint observed: the same counters
// in the same read order, counted where the bytes leave the node.

func TestRequestTallyEachOutcomeMovesOneCounter(t *testing.T) {
	for _, out := range []collection.TallyOutcome{collection.TallyAnswered, collection.TallyRefused, collection.TallyError, collection.TallyTimedOut} {
		t.Run(out.String(), func(t *testing.T) {
			tally := collection.NewRequestTally()
			tok := tally.Begin(collection.TallySNMP)
			if c := tally.Counters(collection.TallySNMP); c.Sent != 1 || c.InFlight != 1 {
				t.Fatalf("after begin: %+v", c)
			}
			tok.End(out)
			c := tally.Counters(collection.TallySNMP)
			if c.Sent != 1 || c.InFlight != 0 || c.Settled() != 1 || c.Outcome(out) != 1 {
				t.Fatalf("after end %s: %+v", out, c)
			}
		})
	}
}

// The tally's read order (sent, then in flight, then outcomes) against its
// write order (begin raises in flight before sent; end counts the outcome
// before lowering in flight) means no read shows more sent than in flight
// plus settled, whatever interleaving the writers take.
func TestRequestTallyReadOrderNeverShowsAFalseGap(t *testing.T) {
	tally := collection.NewRequestTally()
	outcomes := []collection.TallyOutcome{collection.TallyAnswered, collection.TallyRefused, collection.TallyError, collection.TallyTimedOut}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				tally.Begin(collection.TallyHTTP).End(outcomes[(g+i)%len(outcomes)])
			}
		}()
	}
	for i := range 10_000 {
		c := tally.Counters(collection.TallyHTTP)
		if c.Sent > c.InFlight+c.Settled() {
			close(stop)
			wg.Wait()
			t.Fatalf("read %d: sent %d > in flight %d + settled %d", i, c.Sent, c.InFlight, c.Settled())
		}
	}
	close(stop)
	wg.Wait()
}

func TestRequestTallyBeginEndAllocatesNothing(t *testing.T) {
	tally := collection.NewRequestTally()
	if n := testing.AllocsPerRun(1000, func() {
		tally.Connected(collection.TallyHTTP)
		tally.Begin(collection.TallyHTTP).End(collection.TallyAnswered)
	}); n != 0 {
		t.Fatalf("connected, begin and end allocate %v times, want 0", n)
	}
}

// A node built without a tally (every unit test's fake wiring) counts
// nothing and fails nothing.
func TestRequestTallyNilIsANoOp(t *testing.T) {
	var tally *collection.RequestTally
	tally.Connected(collection.TallyTCP)
	tally.Begin(collection.TallyHTTP).End(collection.TallyAnswered)
	tally.BatchAcknowledged()
	tally.BatchDropped()
	tally.SetQueueDepth(3)
	tally.SetBusyWorkers(2)
	if c := tally.Counters(collection.TallyHTTP); c.Sent != 0 {
		t.Fatalf("a nil tally counted %+v", c)
	}
	if s := tally.Snapshot(time.Now()); len(s.Transports) != 0 {
		t.Fatalf("a nil tally's snapshot = %+v", s)
	}
}

// A gauge no pool reports reads unset, not zero: the lab says "not
// reported" instead of a false "idle".
func TestRequestTallyGaugesReadUnsetUntilSet(t *testing.T) {
	tally := collection.NewRequestTally()
	s := tally.Snapshot(time.Now())
	if s.QueueDepth != nil || s.BusyWorkers != nil {
		t.Fatalf("unset gauges read %+v and %+v", s.QueueDepth, s.BusyWorkers)
	}
	tally.SetQueueDepth(7)
	tally.SetQueueDepth(2)
	tally.SetBusyWorkers(4)
	s = tally.Snapshot(time.Now())
	if s.QueueDepth == nil || s.QueueDepth.Current != 2 || s.QueueDepth.High(time.Now().Add(-time.Minute), time.Now()) != 7 {
		t.Fatalf("queue depth = %+v, want current 2 and high water 7", s.QueueDepth)
	}
	if s.BusyWorkers == nil || s.BusyWorkers.Current != 4 {
		t.Fatalf("busy workers = %+v", s.BusyWorkers)
	}
}

func TestRequestTallySnapshotCarriesTouchedTransportsAndBatches(t *testing.T) {
	tally := collection.NewRequestTally()
	tally.Connected(collection.TallyTCP)
	tally.Begin(collection.TallyWebSocket)
	tally.BatchAcknowledged()
	tally.BatchAcknowledged()
	tally.BatchDropped()
	s := tally.Snapshot(time.Now())
	if len(s.Transports) != 2 || s.Transports[0].Transport != "tcp" || s.Transports[1].Transport != "websocket" {
		t.Fatalf("transports = %+v, want tcp then websocket, the untouched left out", s.Transports)
	}
	if s.Transports[1].InFlight != 1 || s.BatchesAcknowledged != 2 || s.BatchesDropped != 1 {
		t.Fatalf("snapshot = %+v", s)
	}
}

// --- against real simulated endpoints on loopback ---------------------------

func newTallySimDevice(t *testing.T, auth sim.HTTPAuth) *sim.Device {
	t.Helper()
	dev := sim.NewDevice("d", "tally-host", net.ParseIP("127.0.0.1"), "p", 1,
		map[string]any{"status": map[string]any{"device": map[string]any{"uptime": 1234}}})
	dev.HTTPAuth = auth
	if err := dev.AddRoute("GET /api/status", "status.json.gotempl", `{"status":"ok"}`, nil); err != nil {
		t.Fatal(err)
	}
	return dev
}

// A digest exchange is two requests on two connections (the challenge, then
// the answer): the node counts both where the sim receives both.
func TestTalliedDigestHTTPFetchCountsTwoSentAndTwoConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	dev := newTallySimDevice(t, sim.HTTPAuth{Mode: sim.HTTPAuthDigest, User: "admin", Password: "boreal", Realm: "Tally"})
	if err := dev.StartHTTP(0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopHTTP)
	tally := collection.NewRequestTally()
	f := collection.NewTalliedHTTPFetcher(tally)
	ans, err := f.Exchange(context.Background(), collection.HTTPExchange{Target: dev.HTTPAddrs()[0].String(), Timeout: 2 * time.Second,
		Request:    driver.Request{Path: "/api/status"},
		Credential: &driver.HTTPCredential{Scheme: driver.SchemeDigest, Username: "admin", Password: "boreal"}})
	if err != nil || ans.Status != 200 {
		t.Fatalf("digest exchange = %+v, %v", ans, err)
	}
	c := tally.Counters(collection.TallyHTTP)
	if c.Sent != 2 || c.Connections != 2 || c.Refused != 1 || c.Answered != 1 || c.InFlight != 0 {
		t.Fatalf("a digest exchange tallied %+v, want two sent on two connections, the challenge refused and the answer answered", c)
	}
	seen := simObservedEventually(t, dev, "http", func(o sim.ObservationCounters) bool { return o.Settled() == 2 && o.OpenConnections == 0 })
	if seen.Received != c.Sent || seen.Accepted != c.Connections || seen.Refused != c.Refused || seen.Answered != c.Answered {
		t.Fatalf("the sim observed %+v against the node's %+v", seen, c)
	}
}

func TestTalliedHTTPProbeCountsOneRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	dev := newTallySimDevice(t, sim.HTTPAuth{})
	if err := dev.StartHTTP(0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopHTTP)
	tally := collection.NewRequestTally()
	res, err := collection.NewTalliedHTTPProber(tally).Probe(context.Background(), collection.HTTPTask{Target: dev.HTTPAddrs()[0].String(), Timeout: 2 * time.Second})
	if err != nil || !res.Responded {
		t.Fatalf("probe = %+v, %v", res, err)
	}
	if c := tally.Counters(collection.TallyHTTP); c.Sent != 1 || c.Connections != 1 || c.Settled() != 1 {
		t.Fatalf("one probe tallied %+v", c)
	}
	seen := simObservedEventually(t, dev, "http", func(o sim.ObservationCounters) bool { return o.Settled() == 1 })
	if seen.Received != 1 || seen.Accepted != 1 {
		t.Fatalf("the sim observed %+v", seen)
	}
}

// udpDropFirst relays datagrams to an agent, dropping the first one: the
// face that loses a request on the way in.
func udpDropFirst(t *testing.T, agent net.Addr) string {
	t.Helper()
	front, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	back, err := net.DialUDP("udp", nil, agent.(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = front.Close(); _ = back.Close() })
	var mu sync.Mutex
	var client *net.UDPAddr
	go func() {
		buf := make([]byte, 65535)
		for {
			n, err := back.Read(buf)
			if err != nil {
				return
			}
			mu.Lock()
			to := client
			mu.Unlock()
			_, _ = front.WriteToUDP(buf[:n], to)
		}
	}()
	go func() {
		buf := make([]byte, 65535)
		for first := true; ; first = false {
			n, from, err := front.ReadFromUDP(buf)
			if err != nil {
				return
			}
			mu.Lock()
			client = from
			mu.Unlock()
			if !first {
				_, _ = back.Write(buf[:n])
			}
		}
	}()
	return front.LocalAddr().String()
}

// The node sends two datagrams, the first lost before the agent: the tally
// counts two sent, one timed out and one answered, and the sim received one.
func TestTalliedSNMPGetPastADroppedDatagramCountsTwoSent(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	dev := newTallySimDevice(t, sim.HTTPAuth{})
	dev.OIDMap = map[string]string{"1.3.6.1.2.1.1.3.0": "status.device.uptime"}
	if err := dev.StartSNMP(0, "lab-ro"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopSNMP)
	target := udpDropFirst(t, dev.SNMPAddr())
	tally := collection.NewRequestTally()
	g := collection.NewTalliedSNMPGetter(tally)
	ctx := context.Background()
	if _, err := g.Get(ctx, target, "lab-ro", []string{"1.3.6.1.2.1.1.3.0"}, 300*time.Millisecond); err == nil {
		t.Fatal("the dropped datagram was answered")
	}
	if _, err := g.Get(ctx, target, "lab-ro", []string{"1.3.6.1.2.1.1.3.0"}, 2*time.Second); err != nil {
		t.Fatalf("the second get: %v", err)
	}
	c := tally.Counters(collection.TallySNMP)
	if c.Sent != 2 || c.TimedOut != 1 || c.Answered != 1 || c.InFlight != 0 || c.Connections != 0 {
		t.Fatalf("a get past a dropped datagram tallied %+v", c)
	}
	if seen := simObservedEventually(t, dev, "snmp", func(o sim.ObservationCounters) bool { return o.Settled() == 1 }); seen.Received != 1 || seen.Answered != 1 {
		t.Fatalf("the sim observed %+v, want the one datagram that reached it", seen)
	}
}

// A reachability probe opens a connection and sends no request.
func TestTalliedTCPProbeCountsAConnectionAndNoRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	dev := newTallySimDevice(t, sim.HTTPAuth{})
	if err := dev.StartHTTP(0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopHTTP)
	tally := collection.NewRequestTally()
	if _, reach, err := collection.NewTalliedTCPDialer(tally).Dial(context.Background(), dev.HTTPAddrs()[0].String(), 2*time.Second); err != nil || !reach.Up() {
		t.Fatalf("tcp probe = %v, %v", reach, err)
	}
	if c := tally.Counters(collection.TallyTCP); c.Connections != 1 || c.Sent != 0 {
		t.Fatalf("a tcp probe tallied %+v, want one connection and no request", c)
	}
	if seen := simObservedEventually(t, dev, "http", func(o sim.ObservationCounters) bool { return o.Accepted == 1 && o.OpenConnections == 0 }); seen.Received != 0 {
		t.Fatalf("the sim observed %+v, want a connection and no request", seen)
	}
}

// A connect the port refuses sends nothing, on every transport.
func TestTalliedRefusedConnectionCountsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	closed := nettest.RefusedAddr(t)
	tally := collection.NewRequestTally()
	ctx := context.Background()
	_, _, _ = collection.NewTalliedTCPDialer(tally).Dial(ctx, closed, time.Second)
	_, _ = collection.NewTalliedHTTPProber(tally).Probe(ctx, collection.HTTPTask{Target: closed, Timeout: time.Second})
	_, _ = collection.NewTalliedHTTPFetcher(tally).Exchange(ctx, collection.HTTPExchange{Target: closed, Timeout: time.Second, Request: driver.Request{Path: "/"}})
	if s := tally.Snapshot(time.Now()); len(s.Transports) != 0 {
		t.Fatalf("a refused connect tallied %+v", s.Transports)
	}
}

func simObservedEventually(t *testing.T, d *sim.Device, protocol string, ok func(sim.ObservationCounters) bool) sim.ObservationCounters {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c := d.Observed(protocol)
		if ok(c) {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s observation never settled: %+v", protocol, c)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
