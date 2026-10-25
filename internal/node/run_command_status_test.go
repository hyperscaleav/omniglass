//go:build linux

package node_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A command's status from issue to settlement, end to end (#1155): the API
// issue with wait, a real node, and the simulated Kestrel bar over two
// transports. set-volume over the held websocket answers settled with the
// bar's answer frame as its evidence, set-volume over https answers settled
// with the bar's confirmation, and each reads back the same by its id, with
// command-settled on the component's timeline. Neither reply carries the
// login the endpoint authenticates with.

// ownerToken bootstraps the owner and returns its bearer token, for driving
// the API as an operator would.
func (s *gateStack) ownerToken() string {
	s.t.Helper()
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.gw.BootstrapOwner(s.ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		s.t.Fatal(err)
	}
	return tok
}

// api sends one request as tok and decodes the answer into out.
func (s *gateStack) api(tok, method, path string, body any, out any) int {
	s.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			s.t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(s.ctx, method, s.apiURL+"/api/v1"+path, rdr)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode < 300 {
		if err := json.Unmarshal(raw, out); err != nil {
			s.t.Fatalf("%s %s: decode %s: %v", method, path, raw, err)
		}
	} else if resp.StatusCode >= 300 {
		s.t.Logf("%s %s = %d %s", method, path, resp.StatusCode, raw)
	}
	return resp.StatusCode
}

type waitedCommand struct {
	ID          int64   `json:"id"`
	Status      string  `json:"status"`
	Phase       string  `json:"phase"`
	SettleMode  string  `json:"settle_mode"`
	SettledBy   string  `json:"settled_by"`
	Reason      string  `json:"reason"`
	Reply       *string `json:"reply"`
	Endpoint    string  `json:"endpoint"`
	Node        string  `json:"node"`
	Driver      string  `json:"driver"`
	DeliveredAt *string `json:"delivered_at"`
	ExecutedAt  *string `json:"executed_at"`
	SettledAt   *string `json:"settled_at"`
	Issuer      string  `json:"issuer"`
	Settlement  string  `json:"settlement"`
}

func TestNodeCommandWaitSettlesWithItsReply(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var barProfile string
	s := newHTTPStack(t, "edge-cmd", nil, func(store *profile.Store) []string {
		barProfile = driverProfile(t, store, "kestrel-api")
		return []string{barProfile}
	})
	bar := s.devices[barProfile]
	s.component("bar-socket")
	s.component("bar-web")
	s.secret("bar-cmd-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	ws := s.attachHTTP("bar-socket", "kestrel-ws", "edge-cmd", map[string]string{
		"host": s.ips[barProfile], "port": fmt.Sprint(s.facePort(barProfile, "websocket")), "login": "bar-cmd-login"})
	web := s.attachHTTP("bar-web", "kestrel-api", "edge-cmd", map[string]string{
		"host": s.ips[barProfile], "port": fmt.Sprint(s.httpPort(barProfile)), "login": "bar-cmd-login"})
	s.runNode("edge-cmd")
	s.waitFor("bar-socket", ws.Name, "session-state", is("up"), "up: the socket held")
	s.waitFor("bar-web", web.Name, "endpoint-authenticated", is("yes"), "yes: the bar accepted the login")
	tok := s.ownerToken()

	for _, tc := range []struct {
		comp, endpoint, driver string
		volume                 int
		reply                  string
	}{
		{"bar-socket", "wss", "kestrel-ws", 42, "42"},
		{"bar-web", "https", "kestrel-api", 47, `"ok"`},
	} {
		var got waitedCommand
		if status := s.api(tok, http.MethodPost, "/components/"+tc.comp+"/commands:issue?wait=true&wait_timeout=20",
			map[string]any{"command_type": "set-volume", "value": tc.volume}, &got); status != http.StatusOK {
			t.Fatalf("%s: issue with wait = %d", tc.comp, status)
		}
		if got.Status != "settled" || got.Phase != "settled" || got.SettledBy != "reply" || got.SettleMode != "reply" || got.Settlement == "" {
			t.Fatalf("%s: waited = %+v, want settled by its reply", tc.comp, got)
		}
		if got.Reply == nil || !strings.Contains(*got.Reply, tc.reply) {
			t.Fatalf("%s: reply = %v, want the bar's answer carrying %s", tc.comp, got.Reply, tc.reply)
		}
		if strings.Contains(*got.Reply, "kestrel") || strings.Contains(got.Reason, "kestrel") {
			t.Fatalf("%s: the evidence carries the login: reply %q reason %q", tc.comp, *got.Reply, got.Reason)
		}
		t.Logf("%s: %s; reply %s", tc.comp, got.Reason, *got.Reply)
		if got.Endpoint != tc.endpoint || got.Node != "edge-cmd" || got.Driver != tc.driver || got.Issuer != "root" {
			t.Fatalf("%s: carried by %q on %q via %q, issued by %q", tc.comp, got.Endpoint, got.Node, got.Driver, got.Issuer)
		}
		if got.DeliveredAt == nil || got.ExecutedAt == nil || got.SettledAt == nil {
			t.Fatalf("%s: transitions = delivered %v executed %v settled %v, want each stamped", tc.comp, got.DeliveredAt, got.ExecutedAt, got.SettledAt)
		}

		var read waitedCommand
		if status := s.api(tok, http.MethodGet, fmt.Sprintf("/components/%s/commands/%d", tc.comp, got.ID), nil, &read); status != http.StatusOK {
			t.Fatalf("%s: read = %d", tc.comp, status)
		}
		if read.Status != "settled" || read.Reply == nil || *read.Reply != *got.Reply || read.Reason != got.Reason {
			t.Fatalf("%s: read back = %+v, want what the wait answered", tc.comp, read)
		}

		var events struct {
			Events []struct {
				Key     string `json:"key"`
				Message string `json:"message"`
			} `json:"events"`
		}
		if status := s.api(tok, http.MethodGet, "/components/"+tc.comp+"/events", nil, &events); status != http.StatusOK {
			t.Fatalf("%s: events = %d", tc.comp, status)
		}
		var issued, settled bool
		for _, e := range events.Events {
			issued = issued || e.Key == "command-issued"
			settled = settled || (e.Key == "command-settled" && strings.Contains(e.Message, "set-volume"))
		}
		if !issued || !settled {
			t.Fatalf("%s: timeline = %+v, want command-issued and command-settled", tc.comp, events.Events)
		}
	}
	if got := fmt.Sprint(bar.ReadPath("status.audio.volume")); got != "47" {
		t.Fatalf("the bar shows volume %s, want the last command's 47", got)
	}
}
