package sim

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestEmitConfiguredOffSkips verifies the safe default: a behavior whose
// emit url renders empty (operator hasn't set the target) sends nothing
// and does not error. The behavior still ticks and applies its mutation.
func TestEmitConfiguredOffSkips(t *testing.T) {
	d := newTestDevice(t, "ucc-1", map[string]any{
		"status": map[string]any{"meeting": map[string]any{"started": false}},
	})
	d.SetDefaults(map[string]any{
		"configuration": map[string]any{"webhook": map[string]any{"target_url": ""}},
	})

	if err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:      "meeting_started",
		Schedule:  profile.Schedule{Interval: "60s"},
		Mutations: []profile.Mutation{{Path: "status.meeting.started", Value: "true"}},
		Emit:      &profile.Emit{URL: "{{ .Defaults.configuration.webhook.target_url }}", Body: `{"event":"meeting.started"}`},
	}); err != nil {
		t.Fatalf("AddBehaviorFromProfile: %v", err)
	}

	d.tickBehavior(&d.behaviors[0])

	// Mutation still applied even though emit was a no-op.
	started := d.State()["status"].(map[string]any)["meeting"].(map[string]any)["started"]
	if started != true {
		t.Fatalf("meeting.started = %v, want true", started)
	}
}

// TestEmitPostsRenderedBody drives a full tick against an httptest
// collector and asserts the emitted POST: method, content-type, and a
// body rendered from post-mutation state + hostname + defaults. The emit
// fires on its own goroutine, so the test waits on the handler.
func TestEmitPostsRenderedBody(t *testing.T) {
	var (
		mu        sync.Mutex
		gotBody   string
		gotMethod string
		gotCT     string
		gotCalled bool
	)
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotCalled = true
		mu.Unlock()
		close(done)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := newTestDevice(t, "ucc-room-7", map[string]any{
		"status": map[string]any{"meeting": map[string]any{"started": false}},
	})
	d.SetDefaults(map[string]any{
		"configuration": map[string]any{
			"webhook": map[string]any{"target_url": srv.URL},
			"room":    map[string]any{"name": "Boardroom"},
		},
	})

	if err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:      "meeting_started",
		Schedule:  profile.Schedule{Interval: "60s"},
		Mutations: []profile.Mutation{{Path: "status.meeting.started", Value: "true"}},
		Emit: &profile.Emit{
			URL:  "{{ .Defaults.configuration.webhook.target_url }}",
			Body: `{"host":"{{ .Hostname }}","event":"meeting.started","room":"{{ .Defaults.configuration.room.name }}"}`,
		},
	}); err != nil {
		t.Fatalf("AddBehaviorFromProfile: %v", err)
	}

	d.tickBehavior(&d.behaviors[0])

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("collector never received the emit POST")
	}

	mu.Lock()
	defer mu.Unlock()
	if !gotCalled {
		t.Fatal("collector not called")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	want := `{"host":"ucc-room-7.lab","event":"meeting.started","room":"Boardroom"}`
	if gotBody != want {
		t.Errorf("body = %q, want %q", gotBody, want)
	}
}

// TestEmitFailureDoesNotBlockTick verifies best-effort semantics: an
// unreachable collector must not panic, block, or stop the behavior from
// applying its mutation. The emit dispatch errors on its own goroutine
// and logs; the tick returns normally.
func TestEmitFailureDoesNotBlockTick(t *testing.T) {
	d := newTestDevice(t, "ucc-2", map[string]any{
		"status": map[string]any{"meeting": map[string]any{"started": false}},
	})
	// 127.0.0.1:1 is a reserved unreachable port, the POST fails fast.
	d.SetDefaults(map[string]any{
		"configuration": map[string]any{"webhook": map[string]any{"target_url": "http://127.0.0.1:1/catch"}},
	})

	if err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:      "meeting_started",
		Schedule:  profile.Schedule{Interval: "60s"},
		Mutations: []profile.Mutation{{Path: "status.meeting.started", Value: "true"}},
		Emit:      &profile.Emit{URL: "{{ .Defaults.configuration.webhook.target_url }}", Body: `{}`},
	}); err != nil {
		t.Fatalf("AddBehaviorFromProfile: %v", err)
	}

	d.tickBehavior(&d.behaviors[0]) // must return without blocking on the dead collector

	started := d.State()["status"].(map[string]any)["meeting"].(map[string]any)["started"]
	if started != true {
		t.Fatalf("meeting.started = %v, want true (mutation must apply despite emit failure)", started)
	}
}

// TestCompileEmitRejectsMissingURL pins the load-time guard: an emit
// block with no url can never fire and is almost certainly a typo.
func TestCompileEmitRejectsMissingURL(t *testing.T) {
	d := newTestDevice(t, "d-1", nil)
	err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:     "b",
		Schedule: profile.Schedule{Interval: "1s"},
		Emit:     &profile.Emit{Body: "{}"},
	})
	if err == nil {
		t.Fatal("expected error for emit missing url, got nil")
	}
	if !contains(err.Error(), "url") {
		t.Errorf("error %q does not mention url", err)
	}
}

// TestEmitOnlyBehaviorAllowed verifies a behavior may carry an emit with
// no mutations: the meeting-started shape sets a flag, but an emit-only
// heartbeat is a valid (more general) behavior too.
func TestEmitOnlyBehaviorAllowed(t *testing.T) {
	d := newTestDevice(t, "d-1", nil)
	err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:     "heartbeat",
		Schedule: profile.Schedule{Interval: "1s"},
		Emit:     &profile.Emit{URL: "http://example.invalid/", Body: "{}"},
	})
	if err != nil {
		t.Fatalf("emit-only behavior rejected: %v", err)
	}
}
