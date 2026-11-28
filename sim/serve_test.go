package sim

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// TestServeHTTP_ConcurrentPublishIsVersionOrdered stresses the race
// that the first draft of this PR had: Publish called after the lock
// released meant two concurrent requests could hand events to the
// publisher in the wrong order, producing version gaps at the
// consumer. The fix moved Publish inside the lock. This test asserts
// the ordering invariant by firing N concurrent mutating POSTs and
// demanding the published Versions be strictly 1..N in order.
func TestServeHTTP_ConcurrentPublishIsVersionOrdered(t *testing.T) {
	const n = 64

	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 0,
		map[string]any{"counter": 0})
	serveRoute(t, dev, "POST /bump", func(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
		if err := d.Write("counter", r.Body["v"]); err != nil {
			return spec.HTTPResponse{Status: http.StatusInternalServerError, Body: err.Error()}
		}
		return spec.HTTPResponse{Body: map[string]any{"ok": true}}
	})

	var (
		mu       sync.Mutex
		received []int64
	)
	dev.DiffPublisher = func(sc StateChange) {
		mu.Lock()
		received = append(received, sc.Version)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"v": %d}`, i)
			req := httptest.NewRequest("POST", "/bump", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			dev.ServeHTTP(httptest.NewRecorder(), req)
		}(i)
	}
	wg.Wait()

	if len(received) != n {
		t.Fatalf("got %d published events, want %d", len(received), n)
	}
	for i, v := range received {
		if v != int64(i+1) {
			t.Errorf("received[%d] = %d, want %d (versions must be strictly monotonic)", i, v, i+1)
		}
	}
}

// TestStartHTTP_TLS verifies that tls=true on an interface results in an
// actual HTTPS listener, closing the kestrel-vroom gap where the flag
// was parsed but ignored.
func TestStartHTTP_TLS(t *testing.T) {
	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 1,
		map[string]any{"msg": "ok"})
	serveText(t, dev, "GET /ping", "application/json", `{"msg":"ok"}`)

	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("generating cert: %v", err)
	}

	// Pick an ephemeral port by asking StartHTTP to bind 0, then read
	// back the actual port from the listener.
	if err := dev.StartHTTP(0, cert); err != nil {
		t.Fatalf("StartHTTP: %v", err)
	}
	defer dev.StopHTTP()

	port := dev.HTTPAddrs()[0].(*net.TCPAddr).Port

	// Wait briefly for the goroutine to begin Serve; sub-second is
	// generous for an in-process listener.
	time.Sleep(50 * time.Millisecond)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("https://127.0.0.1:" + strconv.Itoa(port) + "/ping")
	if err != nil {
		t.Fatalf("GET over TLS: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"msg":"ok"`) {
		t.Errorf("body = %s", body)
	}
}

// vroomWithCall is the shipped kestrel-vroom as the engine builds it, with the
// call state the dial tests start from: idle, no id, no one on.
func vroomWithCall(t *testing.T) *Device {
	t.Helper()
	e := shippedEngine(t)
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "d", Hostname: "h", LoopbackIP: "127.0.0.1", ProfileName: profileOf(t, e, "kestrel-vroom"), Version: 1}}); err != nil {
		t.Fatal(err)
	}
	dev := e.Devices()[0]
	state := dev.State()
	state["status"].(map[string]any)["call"] = map[string]any{"id": "", "active": false, "participants": int64(0)}
	dev.SetState(state, dev.Version+1)
	return dev
}

// The kestrel-vroom dial route (#1445): a refused id pattern must not
// leave status.call.active or participants written.
func TestServeHTTP_VroomDialRefusedLeavesStateUntouched(t *testing.T) {
	dev := vroomWithCall(t)
	req := httptest.NewRequest("POST", "/api/call/dial", strings.NewReader(`{"id":"bad id!","participants":5}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; %s", w.Code, w.Body.String())
	}
	call := dev.State()["status"].(map[string]any)["call"].(map[string]any)
	if call["active"] != false || call["participants"] != int64(0) || call["id"] != "" {
		t.Errorf("refused dial wrote state: %v", call)
	}
}

// A participants count outside 1..64 is refused the same way (#1445).
func TestServeHTTP_VroomDialRefusesOutOfBoundParticipants(t *testing.T) {
	for _, body := range []string{
		`{"id":"c1","participants":-3}`,
		`{"id":"c1","participants":65}`,
		`{"id":"c1","participants":1000000}`,
	} {
		dev := vroomWithCall(t)
		req := httptest.NewRequest("POST", "/api/call/dial", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		dev.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; %s", body, w.Code, w.Body.String())
		}
		call := dev.State()["status"].(map[string]any)["call"].(map[string]any)
		if call["active"] != false || call["participants"] != int64(0) || call["id"] != "" {
			t.Errorf("%s: refused dial wrote state: %v", body, call)
		}
	}
	// In bound still dials.
	dev := vroomWithCall(t)
	req := httptest.NewRequest("POST", "/api/call/dial", strings.NewReader(`{"id":"c1","participants":5}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("in-bound dial status = %d, want 200; %s", w.Code, w.Body.String())
	}
}
