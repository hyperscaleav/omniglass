//go:build linux

package node_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// countingDevice is an http device that counts what it is asked and the most
// requests it ever held at once: more than one is two requests overlapping
// on its endpoint. A slow device holds each request for its delay.
type countingDevice struct {
	srv      *httptest.Server
	delay    time.Duration
	asked    atomic.Int64
	inFlight atomic.Int64
	mu       sync.Mutex
	most     int64
}

func newCountingDevice(t *testing.T, delay time.Duration) *countingDevice {
	t.Helper()
	d := &countingDevice{delay: delay}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := d.inFlight.Add(1)
		defer d.inFlight.Add(-1)
		d.mu.Lock()
		d.most = max(d.most, n)
		d.mu.Unlock()
		asked := d.asked.Add(1)
		if d.delay > 0 {
			select {
			case <-time.After(d.delay):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"uptime": %d}`, asked)
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *countingDevice) mostAtOnce() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.most
}

func (d *countingDevice) hostPort(t *testing.T) (string, string) {
	t.Helper()
	host, port, err := net.SplitHostPort(d.srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

// TestFiftyComponentsBesideASlowDeviceRunInTheirOwnLanes drives #1249 over
// the real wire: Postgres, the bus, a real node, and fifty components, each
// an http device answering two polls a second, one of them holding every
// request for three seconds. Every fast component is asked its rounds in a
// bounded time, the slow one is asked request after request, and no device ever holds two requests
// at once: one discrete request in flight per endpoint across the fleet,
// while the lanes run beside each other.
func TestFiftyComponentsBesideASlowDeviceRunInTheirOwnLanes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	const (
		fleet = 50
		slow  = 3 * time.Second
		want  = 6 // two polls, three rounds each
		bound = 30 * time.Second
	)
	s := newHTTPStack(t, "edge-lanes", nil, func(*profile.Store) []string { return nil })
	poll := func(name string) map[string]any {
		return map[string]any{"name": name, "schedule": map[string]any{"every": "1s"}, "request": map[string]any{"path": "/" + name},
			"emits": []any{map[string]any{"name": "uptime", "extract": map[string]any{"jsonpath": "uptime"}}}}
	}
	s.authorDriver("lane-json", map[string]any{
		"version":   1,
		"transport": "http",
		"inputs": []any{
			map[string]any{"name": "host", "kind": "string", "required": true},
			map[string]any{"name": "port", "kind": "number", "default": "80"},
			map[string]any{"name": "key", "kind": "secret", "secret_type": "api-key", "required": true},
		},
		"session": map[string]any{"credential": "key", "scheme": "api-key", "api_key": map[string]any{"in": "header", "name": "X-Key"}},
		"polls":   []any{poll("a"), poll("b")},
	})
	s.secret("lane-key", "api-key", map[string]string{"key": "lane-key"})
	devices := make([]*countingDevice, fleet)
	for i := range devices {
		delay := time.Duration(0)
		if i == 0 {
			delay = slow
		}
		devices[i] = newCountingDevice(t, delay)
		comp := fmt.Sprintf("display-%02d", i)
		s.component(comp)
		host, port := devices[i].hostPort(t)
		s.attachHTTP(comp, "lane-json", "edge-lanes", map[string]string{"host": host, "port": port, "key": "lane-key"})
	}

	runCtx, cancel := context.WithCancel(s.ctx)
	ran := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: s.apiURL, Name: "edge-lanes", Token: s.token,
			HeartbeatEvery: 500 * time.Millisecond})
		ran <- err
	}()
	t.Cleanup(func() {
		cancel()
		<-ran
	})

	fast := devices[1:]
	behind := func() int {
		n := 0
		for _, d := range fast {
			if d.asked.Load() < want {
				n++
			}
		}
		return n
	}
	for behind() > 0 && time.Since(start) < bound {
		time.Sleep(100 * time.Millisecond)
	}
	if n := behind(); n > 0 {
		t.Fatalf("%d of %d fast components were asked fewer than %d times in %v beside a device holding each request %v", n, len(fast), want, bound, slow)
	}
	t.Logf("every fast component was asked %d times within %v", want, time.Since(start).Round(time.Millisecond))
	// The slow device's two polls fall due while each request is held: they
	// wait their turn on its lane rather than overlap, so it is asked again
	// and again, one at a time.
	for devices[0].asked.Load() < 3 && time.Since(start) < bound {
		time.Sleep(100 * time.Millisecond)
	}
	if n := devices[0].asked.Load(); n < 3 {
		t.Errorf("the slow device was asked %d times in %v, want at least 3; the test proves nothing about its lane", n, bound)
	}
	for i, d := range devices {
		if most := d.mostAtOnce(); most > 1 {
			t.Errorf("display-%02d held %d requests at once, want one in flight per endpoint", i, most)
		}
	}
	for i := 1; i < fleet; i += 7 {
		comp := fmt.Sprintf("display-%02d", i)
		deadline := time.Now().Add(10 * time.Second)
		for {
			series, err := s.gw.ListMetricSeries(s.ctx, "component", comp, "uptime", 0, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(series) > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s landed no uptime sample in Postgres", comp)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}
