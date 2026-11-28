package sim

import (
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
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

	serveRoute(t, dev, "GET /status", func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
		return spec.HTTPResponse{Body: map[string]any{
			"model":  spec.String(d, "configuration.device.model"),
			"uptime": spec.Int64(d, "status.device.uptime"),
		}}
	})

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
	// The simulator answers 200 even when no route matches "/",
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
