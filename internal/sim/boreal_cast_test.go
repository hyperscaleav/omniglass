//go:build linux

package sim

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Boreal media player's face (#893): its JSON API behind an api key,
// reporting identity, storage and playback, and setting playback, with a
// playlist that moves along while it plays.

const castKey = "boreal-cast-key"

func startCast(t *testing.T) *Device {
	t.Helper()
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "player-1", Hostname: "player-1", LoopbackIP: ip, ProfileName: "boreal-cast-m1", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return deviceByProfile(t, e, "boreal-cast-m1")
}

func castDo(t *testing.T, d *Device, method, path, body, key string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://"+faceAddr(t, d, "http-api")+path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("X-Boreal-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestBorealCastServesStatusAndPlayback: the status route reports identity,
// uptime and storage, the playback route the state and the item playing;
// a request without the key is refused.
func TestBorealCastServesStatusAndPlayback(t *testing.T) {
	d := startCast(t)
	code, s := castDo(t, d, http.MethodGet, "/api/status", "", castKey)
	if code != http.StatusOK {
		t.Fatalf("GET /api/status: %d", code)
	}
	dev, storage := s["device"].(map[string]any), s["storage"].(map[string]any)
	if dev["model"] != "Cast M1" || dev["serial"] == "" || dev["firmware"] == "" {
		t.Errorf("device = %v", dev)
	}
	if u, ok := storage["used_percent"].(float64); !ok || u <= 0 || u >= 100 {
		t.Errorf("storage = %v, want a used percentage", storage)
	}
	code, p := castDo(t, d, http.MethodGet, "/api/playback", "", castKey)
	if code != http.StatusOK || p["state"] != "playing" || p["content"] != "Welcome loop" {
		t.Errorf("GET /api/playback = %d %v, want playing the welcome loop", code, p)
	}
	if code, _ := castDo(t, d, http.MethodGet, "/api/playback", "", ""); code != http.StatusUnauthorized {
		t.Errorf("GET /api/playback with no key: %d, want 401", code)
	}
}

// TestBorealCastPlaylistMovesAlongWhilePlaying: each tick steps to the next
// item, wrapping after the fourth; paused, the item holds; a state outside
// the enum is refused.
func TestBorealCastPlaylistMovesAlongWhilePlaying(t *testing.T) {
	d := startCast(t)
	d.StopBehaviors()
	var advance *behaviorEntry
	d.behaviorMu.Lock()
	for i := range d.behaviors {
		if d.behaviors[i].Name == "playlist_advance" {
			advance = &d.behaviors[i]
		}
	}
	d.behaviorMu.Unlock()
	if advance == nil {
		t.Fatalf("no playlist_advance behavior; have %v", d.Behaviors())
	}
	content := func() string {
		_, p := castDo(t, d, http.MethodGet, "/api/playback", "", castKey)
		return p["content"].(string)
	}
	var seen []string
	for i := 0; i < 5; i++ {
		d.tickBehavior(advance)
		seen = append(seen, content())
	}
	want := []string{"Wayfinding", "Today's events", "Safety notice", "Welcome loop", "Wayfinding"}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("playlist over five ticks = %q, want %q", seen, want)
		}
	}

	if code, p := castDo(t, d, http.MethodPost, "/api/playback", `{"state": "paused"}`, castKey); code != http.StatusOK || p["state"] != "paused" {
		t.Fatalf("pause = %d %v", code, p)
	}
	d.tickBehavior(advance)
	if got := content(); got != "Wayfinding" {
		t.Errorf("paused, a tick moved the item to %q", got)
	}
	if code, _ := castDo(t, d, http.MethodPost, "/api/playback", `{"state": "rewinding"}`, castKey); code < 400 {
		t.Errorf("state %q: %d, want a refusal", "rewinding", code)
	}
	if _, p := castDo(t, d, http.MethodGet, "/api/playback", "", castKey); p["state"] != "paused" {
		t.Errorf("after a refused write the state is %v, want paused", p["state"])
	}
}
