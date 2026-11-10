package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// bookingService builds the shipped booking-service profile's device on a
// fixed clock.
func bookingService(t *testing.T, at time.Time) *Device {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	e := NewEngine(store, profiles.Templates())
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "hq-booking", Hostname: "hq-booking", LoopbackIP: "127.0.0.1", ProfileName: "booking-service", Version: 1}}); err != nil {
		t.Fatalf("load device: %v", err)
	}
	devs := e.Devices()
	if len(devs) != 1 {
		t.Fatalf("engine built %d devices from the booking-service profile, want one", len(devs))
	}
	devs[0].SetClock(func() time.Time { return at })
	return devs[0]
}

// The booking service keeps one meeting day, the same every day (#1370,
// #1743): a place reads booked while a meeting runs and free between, and
// its count is the meetings left in the day, the one in progress included.
func TestBookingsAtKeepsAMeetingDay(t *testing.T) {
	day := []any{"09:00-10:00", "10:30-11:30", "13:00-14:00", "15:00-16:30"}
	midnight := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at        string
		state     string
		remaining int
	}{
		{"08:59", "free", 4},
		{"09:00", "booked", 4},
		{"09:59", "booked", 4},
		{"10:00", "free", 3},
		{"10:45", "booked", 3},
		{"12:00", "free", 2},
		{"13:30", "booked", 2},
		{"15:10", "booked", 1},
		{"16:30", "free", 0},
		{"23:59", "free", 0},
	} {
		at, err := time.Parse("15:04", c.at)
		if err != nil {
			t.Fatal(err)
		}
		now := midnight.Add(time.Duration(at.Hour())*time.Hour + time.Duration(at.Minute())*time.Minute)
		b, err := bookingsAt(now, day)
		if err != nil {
			t.Fatalf("at %s: %v", c.at, err)
		}
		if b.State != c.state || b.Remaining != c.remaining {
			t.Errorf("at %s: %s with %d left, want %s with %d", c.at, b.State, b.Remaining, c.state, c.remaining)
		}
	}
}

// A booking day that is not a list of HH:MM-HH:MM windows fails the render
// rather than pushing a made-up day.
func TestBookingsAtRefusesAMalformedDay(t *testing.T) {
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	for _, day := range []any{nil, "09:00-10:00", []any{"9-10"}, []any{"10:00-09:00"}, []any{"09:00-25:00"}, []any{3}} {
		if _, err := bookingsAt(now, day); err == nil {
			t.Errorf("bookingsAt(%v) took it, want refused", day)
		}
	}
}

// The push face renders the booking-service batch from a fixed clock
// (#1743): the owner it is rendered for, the service's source, the meetings
// left as booking-count and the place's state as booking-state, the batch
// the retired simnode pusher sent.
func TestPushFaceRendersTheBookingServiceBatch(t *testing.T) {
	d := bookingService(t, time.Date(2026, 10, 29, 13, 30, 0, 0, time.UTC))
	body, err := d.RenderPush("sys-1")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var got, want any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("the rendered push is not JSON: %v\n%s", err, body)
	}
	_ = json.Unmarshal([]byte(`{"owner":{"kind":"system","ref":"sys-1"},"source":"sim-booking-service",
		"metrics":[{"name":"booking-count","value":2}],
		"properties":[{"name":"booking-state","value":"booked"}]}`), &want)
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if !bytes.Equal(g, w) {
		t.Errorf("rendered\n%s\nwant\n%s", g, w)
	}
	if every := d.PushEvery(); every != 30*time.Second {
		t.Errorf("the booking service pushes every %s, want 30s", every)
	}
}

// A device whose profile has no push face renders nothing and never pushes.
func TestADeviceWithNoPushFaceRendersNone(t *testing.T) {
	d := uccPlatform(t)
	if _, err := d.RenderPush("sys-1"); err == nil {
		t.Error("the ucc platform rendered a push, but its profile declares no push face")
	}
	if err := d.PushOnce(context.Background()); err == nil {
		t.Error("the ucc platform pushed, but its profile declares no push face")
	}
}

// pushServer is a fake server's two routes the push face calls: its own
// grant and the push route, recording each push and the auth it carried.
type pushServer struct {
	mu       sync.Mutex
	grants   []map[string]string
	pushes   []map[string]any
	auths    []string
	rejected bool
	down     bool
	gone     map[string]bool
}

func (s *pushServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.auths = append(s.auths, r.Header.Get("Authorization"))
		if s.down {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": s.grants})
	})
	mux.HandleFunc("POST /api/v1/telemetry:push", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.auths = append(s.auths, r.Header.Get("Authorization"))
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.pushes = append(s.pushes, b)
		if ref, _ := b["owner"].(map[string]any)["ref"].(string); s.gone[ref] {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if s.rejected {
			_ = json.NewEncoder(w).Encode(map[string]any{"rejected": []map[string]string{{"name": "booking-count", "reason": "no such metric"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": 2})
	})
	return mux
}

func (s *pushServer) refs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, p := range s.pushes {
		out = append(out, p["owner"].(map[string]any)["ref"].(string))
	}
	return out
}

// The push face reads the systems it may push to off its own grant and
// pushes each one's batch with its bearer token (#1743): every system grant,
// and nothing else its grants name.
func TestPushFacePushesEverySystemItsGrantNames(t *testing.T) {
	srv := &pushServer{grants: []map[string]string{
		{"scope_kind": "system", "scope_id": "sys-1"},
		{"scope_kind": "location", "scope_id": "loc-1"},
		{"scope_kind": "system", "scope_id": "sys-2"},
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	d := bookingService(t, time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC))
	ctx := context.Background()
	if err := d.PushOnce(ctx); err == nil {
		t.Fatal("pushed with no credential")
	}
	d.SetPushCredential(ts.URL+"/", "bk_tok_secret")
	if err := d.PushOnce(ctx); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := strings.Join(srv.refs(), ","); got != "sys-1,sys-2" {
		t.Errorf("pushed to %s, want sys-1,sys-2", got)
	}
	for _, a := range srv.auths {
		if a != "Bearer bk_tok_secret" {
			t.Errorf("a call carried %q, want the bearer token", a)
		}
	}
}

// A push the server rejects an item of is an error, and the next push reads
// the grant again; a server not up yet is retried on the next tick.
func TestPushFaceRereadsItsGrantAfterAFailure(t *testing.T) {
	srv := &pushServer{grants: []map[string]string{{"scope_kind": "system", "scope_id": "sys-1"}}, down: true}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	d := bookingService(t, time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC))
	d.SetPushCredential(ts.URL, "tok")
	ctx := context.Background()
	if err := d.PushOnce(ctx); err == nil {
		t.Fatal("pushed while the server was down")
	}
	srv.mu.Lock()
	srv.down, srv.rejected = false, true
	srv.mu.Unlock()
	if err := d.PushOnce(ctx); err == nil || !strings.Contains(err.Error(), "no such metric") {
		t.Fatalf("a rejected item read %v, want the server's reason", err)
	}
	srv.mu.Lock()
	srv.rejected = false
	srv.grants = []map[string]string{{"scope_kind": "system", "scope_id": "sys-9"}}
	srv.mu.Unlock()
	if err := d.PushOnce(ctx); err != nil {
		t.Fatalf("push: %v", err)
	}
	if refs := srv.refs(); refs[len(refs)-1] != "sys-9" {
		t.Errorf("pushed to %v after a failure, want the grant read again (sys-9)", refs)
	}
}

// A grant outlives the system it names (deleting a system leaves the grant),
// so a push for that system is refused on every tick; it costs only its own
// push, and every other system the grant names is still pushed for (#1743).
func TestPushFaceKeepsPushingPastAGoneSystem(t *testing.T) {
	srv := &pushServer{grants: []map[string]string{
		{"scope_kind": "system", "scope_id": "sys-gone"},
		{"scope_kind": "system", "scope_id": "sys-1"},
	}, gone: map[string]bool{"sys-gone": true}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	d := bookingService(t, time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC))
	d.SetPushCredential(ts.URL, "tok")
	err := d.PushOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sys-gone") {
		t.Fatalf("push = %v, want the gone system's refusal named", err)
	}
	if got := strings.Join(srv.refs(), ","); got != "sys-gone,sys-1" {
		t.Errorf("pushed to %s, want sys-gone,sys-1: the live system still pushed", got)
	}
}

// The push loop pushes as soon as it holds a credential and never writes the
// token to the log, whatever the server answers.
func TestPushLoopPushesOnItsCredentialAndLogsNoToken(t *testing.T) {
	srv := &pushServer{grants: []map[string]string{{"scope_kind": "system", "scope_id": "sys-1"}}, rejected: true}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	var logged bytes.Buffer
	var logMu sync.Mutex
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(lockedWriter{&logMu, &logged}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	d := bookingService(t, time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC))
	d.StartPush()
	defer d.StopPush()
	d.SetPushCredential(ts.URL, "bk_00000000_never-in-a-log")
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.refs()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the loop did not push once it held a credential")
		}
		time.Sleep(10 * time.Millisecond)
	}
	d.StopPush()
	logMu.Lock()
	defer logMu.Unlock()
	if !strings.Contains(logged.String(), "push") {
		t.Errorf("a rejected push logged nothing:\n%s", logged.String())
	}
	if strings.Contains(logged.String(), "never-in-a-log") {
		t.Errorf("the log carries the token:\n%s", logged.String())
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// A credential that arrives before the loop starts (the simulator loads its
// pushers, then hands each its token) is pushed on once, not twice: the
// loop's first tick already holds it, so the wake it left is spent.
func TestPushLoopPushesOnceForACredentialHeldAtStart(t *testing.T) {
	srv := &pushServer{grants: []map[string]string{{"scope_kind": "system", "scope_id": "sys-1"}}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	d := bookingService(t, time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC))
	d.SetPushCredential(ts.URL, "tok")
	d.StartPush()
	defer d.StopPush()
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.refs()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the loop did not push the credential it held at start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(srv.refs()); n != 1 {
		t.Errorf("pushed %d times at start, want once (the cadence is 30s)", n)
	}
}
