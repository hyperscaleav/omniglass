//go:build linux

package outpost_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/outpost"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// keyDevice is an http device that records the api key each request carried.
type keyDevice struct {
	srv  *httptest.Server
	mu   sync.Mutex
	keys map[string]bool
}

func newKeyDevice(t *testing.T) *keyDevice {
	t.Helper()
	d := &keyDevice{keys: map[string]bool{}}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.keys[r.Header.Get("X-Key")] = true
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"uptime": 1}`)
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *keyDevice) sent(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.keys[key]
}

// TestARotatedSecretReachesTheOutpostAtOnce drives #1997 over the real wire:
// Postgres, the bus, and a real outpost that pulls its worklist once an hour.
// Rotating the secret an endpoint's input names announces that endpoint's
// outpost on commit, the bus nudges it, and the device's next request carries
// the new key within a second, not an hour.
func TestARotatedSecretReachesTheOutpostAtOnce(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	s := newHTTPStack(t, "edge-rotate", nil, func(*profile.Store) []string { return nil })
	s.authorDriver("keyed-json", map[string]any{
		"version":   1,
		"transport": "http",
		"inputs": []any{
			map[string]any{"name": "host", "kind": "string", "required": true},
			map[string]any{"name": "port", "kind": "number", "default": "80"},
			map[string]any{"name": "key", "kind": "secret", "secret_type": "api-key", "required": true},
		},
		"session": map[string]any{"credential": "key", "scheme": "api-key", "api_key": map[string]any{"in": "header", "name": "X-Key"}},
		"polls": []any{map[string]any{"name": "status", "schedule": map[string]any{"every": "100ms"}, "request": map[string]any{"path": "/status"},
			"emits": []any{map[string]any{"name": "uptime", "extract": map[string]any{"jsonpath": "uptime"}}}}},
	})
	sec := s.secret("rotating-key", "api-key", map[string]string{"key": "before"})
	device := newKeyDevice(t)
	host, port, err := net.SplitHostPort(device.srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s.component("keyed-display")
	s.attachHTTP("keyed-display", "keyed-json", "edge-rotate", map[string]string{"host": host, "port": port, "key": "rotating-key"})

	runCtx, cancel := context.WithCancel(s.ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := outpost.Run(runCtx, outpost.Config{ServerURL: s.apiURL, Name: "edge-rotate", Token: s.token,
			HeartbeatEvery: 500 * time.Millisecond, WorklistEvery: time.Hour})
		ran <- err
	}()
	t.Cleanup(func() {
		cancel()
		<-ran
	})

	pollUntil(t, 20*time.Second, "the device was never asked with the first key", func() bool { return device.sent("before") })
	if _, err := s.gw.UpdateSecret(s.ctx, "", sec.ID, storage.SecretPatch{Fields: map[string]string{"key": "after"}}, s.all, s.all, true, true); err != nil {
		t.Fatal(err)
	}
	pollUntil(t, time.Second, "a rotated secret did not reach an outpost pulling hourly within a second", func() bool { return device.sent("after") })
}
