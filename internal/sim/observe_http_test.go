package sim

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// Each protocol's observation is proven on a real loopback device: a request
// the face answers moves accepted, received and answered once; a refusal
// moves refused; a client gone before the answer moves dropped; a stream is
// opened, never in flight.

// observedEventually waits for a device's counters on one protocol to
// satisfy ok, failing with the last read.
func observedEventually(t *testing.T, d *Device, protocol string, ok func(ObservationCounters) bool) ObservationCounters {
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

func oneShotClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 3 * time.Second}
}

func TestObservedHTTPAnsweredRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev := newAuthDevice(t, HTTPAuth{})
	if err := dev.StartHTTP(0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopHTTP)
	resp, err := oneShotClient().Get("http://" + dev.HTTPAddrs()[0].String() + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	c := observedEventually(t, dev, "http", func(c ObservationCounters) bool { return c.OpenConnections == 0 && c.Settled() == 1 })
	if c.Accepted != 1 || c.Received != 1 || c.Answered != 1 || c.InFlight != 0 {
		t.Fatalf("one answered request = %+v", c)
	}
	if dev.HTTPRequests() != 1 {
		t.Fatalf("HTTPRequests = %d, want 1", dev.HTTPRequests())
	}
}

func TestObservedHTTPRefusedRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev := newAuthDevice(t, HTTPAuth{Mode: HTTPAuthBasic, User: "admin", Password: "kestrel"})
	if err := dev.StartHTTP(0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopHTTP)
	resp, err := oneShotClient().Get("http://" + dev.HTTPAddrs()[0].String() + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	c := observedEventually(t, dev, "http", func(c ObservationCounters) bool { return c.Settled() == 1 })
	if c.Refused != 1 || c.Answered != 0 || c.Received != 1 {
		t.Fatalf("one refused request = %+v", c)
	}
}

func TestObservedHTTPClientHangUpIsDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev := newAuthDevice(t, HTTPAuth{})
	held, release := make(chan struct{}), make(chan struct{})
	dev.beforeRoute = func(r *http.Request) {
		close(held)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}
	defer close(release)
	if err := dev.StartHTTP(0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopHTTP)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+dev.HTTPAddrs()[0].String()+"/api/status", nil)
	go func() {
		<-held
		if c := dev.Observed("http"); c.InFlight != 1 {
			t.Errorf("a held request is not in flight: %+v", c)
		}
		cancel()
	}()
	if _, err := oneShotClient().Do(req); err == nil {
		t.Fatal("the cancelled request was answered")
	}
	c := observedEventually(t, dev, "http", func(c ObservationCounters) bool { return c.Settled() == 1 })
	if c.Dropped != 1 || c.InFlight != 0 {
		t.Fatalf("a client hang-up = %+v, want dropped", c)
	}
}

func TestObservedHTTPEventStreamIsOpenedNotInFlight(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev, addr := newEventsDevice(t, HTTPAuth{})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	c := observedEventually(t, dev, "http", func(c ObservationCounters) bool { return c.OpenStreams == 1 })
	if c.StreamsOpened != 1 || c.InFlight != 0 || c.Received != 0 {
		t.Fatalf("an open stream = %+v, want opened and not a request in flight", c)
	}
	cancel()
	_ = resp.Body.Close()
	c = observedEventually(t, dev, "http", func(c ObservationCounters) bool { return c.OpenStreams == 0 })
	if c.StreamsOpened != 1 {
		t.Fatalf("after the stream closed: %+v", c)
	}
	if dev.HTTPRequests() != 1 {
		t.Fatalf("HTTPRequests = %d, want the one stream", dev.HTTPRequests())
	}
}

// The engine's snapshot names the face that saw two requests at once.
func TestObservedHTTPOverlapNamesTheFace(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev := newAuthDevice(t, HTTPAuth{})
	held, release := make(chan struct{}, 2), make(chan struct{})
	dev.beforeRoute = func(*http.Request) { held <- struct{}{}; <-release }
	if err := dev.StartHTTP(0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopHTTP)
	url := "http://" + dev.HTTPAddrs()[0].String() + "/api/status"
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			if resp, err := oneShotClient().Get(url); err == nil {
				_ = resp.Body.Close()
			}
			done <- struct{}{}
		}()
	}
	<-held
	<-held
	close(release)
	<-done
	<-done
	c := observedEventually(t, dev, "http", func(c ObservationCounters) bool { return c.Settled() == 2 })
	if c.Overlapped != 1 {
		t.Fatalf("two requests at once on one face = %+v, want one overlap", c)
	}
	snap := dev.observe.engine.snapshot(time.Now())
	if len(snap.Faces) != 1 || snap.Faces[0].Protocol != "http" || snap.Faces[0].Overlapped != 1 {
		t.Fatalf("faces = %+v", snap.Faces)
	}
}
