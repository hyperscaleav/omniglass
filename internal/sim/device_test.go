package sim

import (
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

func TestDeviceServeHTTP(t *testing.T) {
	state := map[string]any{
		"status": map[string]any{
			"device": map[string]any{
				"power":  "on",
				"uptime": 12345,
			},
			"audio": map[string]any{
				"volume": 50,
				"mute":   false,
			},
		},
		"configuration": map[string]any{
			"device": map[string]any{
				"model":        "Room Bar",
				"manufacturer": "Kestrel",
			},
			"firmware": map[string]any{
				"version": "ce11.14.1",
			},
		},
	}

	dev := NewDevice("test-id", "BOI-17C-415B-COD1", net.ParseIP("127.1.0.1"), "kestrel-vroom", 1, state)

	tmpl := `{"model":"{{ .State | get "configuration" | get "device" | get "model" }}","uptime":{{ .State | get "status" | get "device" | get "uptime" }}}`
	if err := dev.AddRoute("GET /status", "test.json.gotempl", tmpl, nil); err != nil {
		t.Fatal(err)
	}

	t.Run("matching route", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/status", nil)
		w := httptest.NewRecorder()
		dev.ServeHTTP(w, req)

		resp := w.Result()
		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != 200 {
			t.Fatalf("got status %d, want 200", resp.StatusCode)
		}
		if !strings.Contains(string(body), `"model":"Room Bar"`) {
			t.Errorf("body = %s, missing model", body)
		}
		if !strings.Contains(string(body), `"uptime":12345`) {
			t.Errorf("body = %s, missing uptime", body)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nonexistent", nil)
		w := httptest.NewRecorder()
		dev.ServeHTTP(w, req)

		if w.Code != 404 {
			t.Fatalf("got status %d, want 404", w.Code)
		}
	})

	// A collector's HTTPS service check completes the TCP+TLS
	// handshake then sends `HEAD /` to confirm the server answers.
	// The simulator answers 200 even when no profile route matches "/",
	// so the probe doesn't spam the event log with HEAD / 404.
	t.Run("liveness HEAD /", func(t *testing.T) {
		req := httptest.NewRequest("HEAD", "/", nil)
		w := httptest.NewRecorder()
		dev.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("got status %d, want 200", w.Code)
		}
	})

	t.Run("liveness GET /", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		dev.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("got status %d, want 200", w.Code)
		}
	})
}

func TestDeviceSetState(t *testing.T) {
	dev := NewDevice("test-id", "host", net.ParseIP("127.0.0.1"), "test", 1, map[string]any{"a": "b"})

	newState := map[string]any{"x": "y"}
	dev.SetState(newState, 5)

	got := dev.State()
	if got["x"] != "y" {
		t.Errorf("state = %v, want x=y", got)
	}
	if dev.Version != 5 {
		t.Errorf("version = %d, want 5", dev.Version)
	}
}

func TestParseMatch(t *testing.T) {
	tests := []struct {
		input      string
		wantMethod string
		wantPath   string
		wantErr    bool
	}{
		{"GET /status", "GET", "/status", false},
		{"POST /api/command/Audio.Volume.Set", "POST", "/api/command/Audio.Volume.Set", false},
		{"invalid", "", "", true},
	}

	for _, tt := range tests {
		method, path, err := parseMatch(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseMatch(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if method != tt.wantMethod || path != tt.wantPath {
			t.Errorf("parseMatch(%q) = %q %q, want %q %q", tt.input, method, path, tt.wantMethod, tt.wantPath)
		}
	}
}

func TestMarshalFunc(t *testing.T) {
	dev := NewDevice("test-id", "host", net.ParseIP("127.0.0.1"), "test", 1,
		map[string]any{
			"status": map[string]any{
				"peripherals": []any{
					map[string]any{"type": "camera", "status": "connected"},
				},
			},
		})

	tmpl := `{{ .State | get "status" | get "peripherals" | marshal }}`
	if err := dev.AddRoute("GET /test", "test.json.gotempl", tmpl, nil); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `"camera"`) {
		t.Errorf("body = %s, missing camera", body)
	}
}

func TestEngineLoadTemplates(t *testing.T) {
	// Every template the catalog references resolves from the embedded
	// tree. LoadDevices only warns and drops the route on a miss, so this
	// is the check that keeps the catalog whole.
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("loading catalog: %v", err)
	}
	e := NewEngine(store, profiles.Templates())
	seen := 0
	for name, prof := range store.Devices {
		for _, iface := range prof.Interfaces {
			for _, r := range iface.Routes {
				if r.Template == "" {
					continue
				}
				seen++
				if _, err := e.loadTemplate(r.Template); err != nil {
					t.Errorf("%s route %q: %v", name, r.Match, err)
				}
			}
			for _, c := range iface.Commands {
				if c.Template == "" {
					continue
				}
				seen++
				if _, err := e.loadTemplate(c.Template); err != nil {
					t.Errorf("%s command %q: %v", name, c.Match, err)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("catalog references no templates; the walk found nothing to check")
	}
	content, err := e.loadTemplate("api/kestrel-vroom/status.json.gotempl")
	if err != nil {
		t.Fatalf("loading template: %v", err)
	}
	if !strings.Contains(content, "{{") {
		t.Error("template content carries no template actions")
	}
}
