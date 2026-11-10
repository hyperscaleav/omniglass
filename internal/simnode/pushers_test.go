package simnode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// A node's pushers (#1743) reach its emulator as one env value, name, profile
// and token each, and read back as they went; a malformed list is refused, so
// a typo never leaves a pusher silently out.
func TestPushersRoundTrip(t *testing.T) {
	in := []Pusher{
		{Name: "hq-booking-1", Profile: "booking-service", Token: "bk_0a0b0c0d_c2VjcmV0LXRva2Vu-_"},
		{Name: "sim-booking-service", Profile: "booking-service", Token: "bk_01020304_b3RoZXI"},
	}
	got, err := ParsePushers(FormatPushers(in))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, in) {
		t.Fatalf("round trip = %+v, want %+v", got, in)
	}
	if got, err := ParsePushers(""); err != nil || len(got) != 0 {
		t.Fatalf("empty = %v, %v; want none", got, err)
	}
	for _, bad := range []string{"a", "a=p", "a=p:", "=p:t", "a=:t", "a=p:t,a=p:u", "a=p:t,"} {
		if _, err := ParsePushers(bad); err == nil {
			t.Errorf("ParsePushers(%q) took it, want refused", bad)
		}
	}
}

// grantServer is a server's two push-face routes, granting each token the
// one system named for it and recording which token pushed for which system.
type grantServer struct {
	mu     sync.Mutex
	grants map[string]string
	pushed map[string]string
}

func (s *grantServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": []map[string]string{{"scope_kind": "system", "scope_id": s.grants[tok]}}})
	})
	mux.HandleFunc("POST /api/v1/telemetry:push", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Owner struct{ Ref string } `json:"owner"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		s.mu.Lock()
		s.pushed[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] = b.Owner.Ref
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": 2})
	})
	return mux
}

// The emulator runs each of its node's pushers as a device of its profile
// holding its own token (#1743), so each pushes for the systems its own grant
// names as soon as it starts, and stops when the emulator does.
func TestStartPushersRunsEachPusherWithItsToken(t *testing.T) {
	srv := &grantServer{grants: map[string]string{"tok-a": "sys-a", "tok-b": "sys-b"}, pushed: map[string]string{}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e, err := startPushers(store, ts.URL, []Pusher{
		{Name: "a-booking-1", Profile: "booking-service", Token: "tok-a"},
		{Name: "b-booking-1", Profile: "booking-service", Token: "tok-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.StopAll()
	deadline := time.Now().Add(5 * time.Second)
	for {
		srv.mu.Lock()
		a, b := srv.pushed["tok-a"], srv.pushed["tok-b"]
		srv.mu.Unlock()
		if a == "sys-a" && b == "sys-b" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pushed %v, want tok-a for sys-a and tok-b for sys-b", srv.pushed)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A pusher whose profile the catalog lacks, or whose profile has no push
// face, could never push, so the emulator refuses it rather than run it
// silently.
func TestStartPushersRefusesAProfileThatCannotPush(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"no-such-profile", "generic-device"} {
		if _, err := startPushers(store, "http://127.0.0.1:1", []Pusher{{Name: "x", Profile: p, Token: "t"}}); err == nil || !strings.Contains(err.Error(), p) {
			t.Errorf("profile %s: %v, want refused naming it", p, err)
		}
	}
}
