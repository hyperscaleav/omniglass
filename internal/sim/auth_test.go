package sim

import (
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// newAuthDevice builds a minimal device with one route for auth tests.
// The route renders a fixed body so a successful response is easy to
// distinguish from a 401.
func newAuthDevice(t *testing.T, auth HTTPAuth) *Device {
	t.Helper()
	dev := NewDevice("d", "auth-host", net.ParseIP("127.0.0.1"), "p", 1,
		map[string]any{"ok": true})
	dev.HTTPAuth = auth
	if err := dev.AddRoute("GET /api/status", "status.json.gotempl",
		`{"status":"ok"}`, nil); err != nil {
		t.Fatal(err)
	}
	if err := dev.AddRoute("POST /api/power", "ack.json.gotempl",
		`{"ok":true}`,
		map[string]any{"power": "{{ .Request.Body.power }}"}); err != nil {
		t.Fatal(err)
	}
	return dev
}

func TestHTTPAuth_NoneAcceptsEverything(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{})

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (no auth = open)", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestHTTPAuth_BasicMissingChallengesWithRealm(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:     HTTPAuthBasic,
		User:     "admin",
		Password: "letmein",
		Realm:    "Boreal Edge",
	})

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	got := w.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(got, "Basic ") {
		t.Errorf("WWW-Authenticate = %q, want Basic challenge", got)
	}
	if !strings.Contains(got, `realm="Boreal Edge"`) {
		t.Errorf("WWW-Authenticate = %q, want realm=Boreal Edge", got)
	}
}

func TestHTTPAuth_BasicWrongPasswordRejected(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:     HTTPAuthBasic,
		User:     "admin",
		Password: "letmein",
	})

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.SetBasicAuth("admin", "nope")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestHTTPAuth_BasicCorrectCredentialsAccepted(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:     HTTPAuthBasic,
		User:     "admin",
		Password: "letmein",
	})

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.SetBasicAuth("admin", "letmein")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHTTPAuth_BasicRealmDefaultsToHostname(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:     HTTPAuthBasic,
		User:     "admin",
		Password: "letmein",
	})

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	got := w.Header().Get("WWW-Authenticate")
	if !strings.Contains(got, `realm="auth-host"`) {
		t.Errorf("WWW-Authenticate = %q, want realm=auth-host (hostname fallback)", got)
	}
}

func TestHTTPAuth_BasicGateBlocksMutation(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:     HTTPAuthBasic,
		User:     "admin",
		Password: "letmein",
	})

	// POST without auth should be rejected, and the mutation must not
	// run. Otherwise the gate is decorative.
	req := httptest.NewRequest("POST", "/api/power",
		strings.NewReader(`{"power":"off"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("POST status = %d, want 401", w.Code)
	}
	if got, ok := dev.State()["power"]; ok {
		t.Errorf("power was mutated to %v despite 401; auth gate did not block mutate", got)
	}
}

func TestHTTPAuth_HeaderMissingRejected(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:       HTTPAuthHeader,
		APIKey:     "secret-key-123",
		HeaderName: "X-API-Key",
	})

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"X-API-Key"`) {
		t.Errorf("body = %s, want it to name the expected header", w.Body.String())
	}
}

func TestHTTPAuth_HeaderWrongKeyRejected(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:       HTTPAuthHeader,
		APIKey:     "secret-key-123",
		HeaderName: "X-API-Key",
	})

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-API-Key", "wrong")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestHTTPAuth_HeaderCorrectKeyAccepted(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:       HTTPAuthHeader,
		APIKey:     "secret-key-123",
		HeaderName: "X-API-Key",
	})

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-API-Key", "secret-key-123")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHTTPAuth_HeaderDefaultsToXAPIKey(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:   HTTPAuthHeader,
		APIKey: "k",
		// HeaderName intentionally empty.
	})

	// Wrong header name still 401s.
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "k")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("status = %d, want 401 with non-default header", w.Code)
	}

	// Default X-API-Key passes.
	req2 := httptest.NewRequest("GET", "/api/status", nil)
	req2.Header.Set("X-API-Key", "k")
	w2 := httptest.NewRecorder()
	dev.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("status = %d, want 200 with default header", w2.Code)
	}
}

func TestHTTPAuth_HeaderCustomNameHonored(t *testing.T) {
	dev := newAuthDevice(t, HTTPAuth{
		Mode:       HTTPAuthHeader,
		APIKey:     "k",
		HeaderName: "X-Polaris-Token",
	})

	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-Polaris-Token", "k")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHTTPAuth_PublicPathBypassesGate(t *testing.T) {
	// A header-auth device exposing a landing page that has to be
	// reachable without the API key, so the user can paste it into a
	// form. Requests to "/" go through; everything else still 401s.
	dev := newAuthDevice(t, HTTPAuth{
		Mode:        HTTPAuthHeader,
		APIKey:      "k",
		HeaderName:  "X-API-Key",
		PublicPaths: []string{"/"},
	})
	if err := dev.AddRoute("GET /", "landing.gotempl",
		`<html>landing</html>`, nil); err != nil {
		t.Fatal(err)
	}

	// Public path passes without a key.
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("public path status = %d, want 200", w.Code)
	}

	// Non-public path still gated.
	req2 := httptest.NewRequest("GET", "/api/status", nil)
	w2 := httptest.NewRecorder()
	dev.ServeHTTP(w2, req2)
	if w2.Code != 401 {
		t.Errorf("gated path status = %d, want 401", w2.Code)
	}
}

func TestHTTPAuth_UnknownModeFailsClosed(t *testing.T) {
	// A typo in a profile must not silently leave the device wide open.
	dev := newAuthDevice(t, HTTPAuth{Mode: HTTPAuthMode("bogus")})

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("unknown auth mode status = %d, want 401 (fail closed)", w.Code)
	}
}

func TestHTTPAuthFromProfile_OpenWhenEnforceFalse(t *testing.T) {
	// A profile that declares basic credentials but does not opt into
	// enforcement must produce an open gate. This is the backward-compat
	// guarantee for every existing profile.
	got := httpAuthFromProfile(profile.Auth{
		Type:            "basic",
		DefaultUser:     "admin",
		DefaultPassword: "secret",
		// Enforce: false (zero value).
	})
	if got.Mode != HTTPAuthNone {
		t.Errorf("Mode = %q, want HTTPAuthNone for non-enforced profile", got.Mode)
	}
}

func TestHTTPAuthFromProfile_BasicEnforced(t *testing.T) {
	got := httpAuthFromProfile(profile.Auth{
		Type:            "basic",
		DefaultUser:     "admin",
		DefaultPassword: "letmein",
		Realm:           "Edge",
		Enforce:         true,
	})
	if got.Mode != HTTPAuthBasic {
		t.Errorf("Mode = %q, want HTTPAuthBasic", got.Mode)
	}
	if got.User != "admin" || got.Password != "letmein" {
		t.Errorf("creds = %q/%q, want admin/letmein", got.User, got.Password)
	}
	if got.Realm != "Edge" {
		t.Errorf("Realm = %q, want Edge", got.Realm)
	}
}

func TestHTTPAuthFromProfile_HeaderEnforced(t *testing.T) {
	got := httpAuthFromProfile(profile.Auth{
		Type:    "header",
		APIKey:  "k",
		Header:  "X-Polaris-Token",
		Enforce: true,
	})
	if got.Mode != HTTPAuthHeader {
		t.Errorf("Mode = %q, want HTTPAuthHeader", got.Mode)
	}
	if got.APIKey != "k" {
		t.Errorf("APIKey = %q, want k", got.APIKey)
	}
	if got.HeaderName != "X-Polaris-Token" {
		t.Errorf("HeaderName = %q, want X-Polaris-Token", got.HeaderName)
	}
}

// TestHTTPAuthFromProfile_CatalogProfiles loads the embedded catalog and
// asserts each HTTP interface produces the auth mode its YAML claims. This
// is the regression catch for "I changed the YAML and forgot to flip
// enforce" or "I renamed a field".
func TestHTTPAuthFromProfile_CatalogProfiles(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	expected := map[string]HTTPAuthMode{
		"boreal-edge-86":  HTTPAuthBasic,
		"kestrel-vroom":   HTTPAuthBasic,
		"polaris-dsp-16":  HTTPAuthHeader,
		"newtron-panel-7": HTTPAuthNone, // declared basic, enforce=false → open
	}

	for name, want := range expected {
		dp := store.Devices[name]
		if dp == nil {
			t.Errorf("profile %q not loaded", name)
			continue
		}
		var httpIface *profile.Interface
		for i := range dp.Interfaces {
			if dp.Interfaces[i].Protocol == "http" {
				httpIface = &dp.Interfaces[i]
				break
			}
		}
		if httpIface == nil {
			t.Errorf("profile %q has no http interface", name)
			continue
		}
		got := httpAuthFromProfile(httpIface.Auth).Mode
		if got != want {
			t.Errorf("profile %q HTTPAuth.Mode = %q, want %q", name, got, want)
		}
	}
}

func TestHTTPAuthFromProfile_UnknownTypeIsOpen(t *testing.T) {
	// A typo'd auth type with enforce: true should be treated as open
	// rather than fail-closed at the profile-decoding layer; the gate
	// itself fail-closes on unknown modes, which catches the case where
	// the runtime mode is set directly. Profiles get a startup warning
	// elsewhere instead.
	got := httpAuthFromProfile(profile.Auth{
		Type:    "oauth-bearer",
		Enforce: true,
	})
	if got.Mode != HTTPAuthNone {
		t.Errorf("Mode = %q, want HTTPAuthNone for unknown type", got.Mode)
	}
}
