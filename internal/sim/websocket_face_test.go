package sim

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// The websocket face (#1034): a simulated component's control socket, a
// JSON message per text frame. A subscribe is acknowledged and then pushes
// every change to its paths; a get or a set is answered by a result carrying
// the request's id, after the push a set causes; the upgrade sits behind
// the face's own auth; the console drops every socket as a restart would;
// and the face serves over the device's https identity when it serves tls.

// wsTestConfig is the websocket block the face tests serve: two watchable
// paths, one settable, and a console drop route.
func wsTestConfig() profile.WebSocket {
	return profile.WebSocket{Path: "/api/ws", Paths: []string{"power", "audio"}, Settable: []string{"audio.volume"},
		DropPath: "/api/admin/sockets/drop"}
}

func newWSDevice(t *testing.T, auth HTTPAuth, withTLS bool) (*Device, string) {
	t.Helper()
	dev := NewDevice("d", "ws-host", net.ParseIP("127.0.0.1"), "p", 1,
		map[string]any{"power": "on", "audio": map[string]any{"volume": 30}, "secret": "x"})
	dev.SetWebSocket(wsTestConfig(), auth)
	addr, err := dev.StartWebSocket(0, withTLS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopWebSocket)
	return dev, addr.String()
}

func dialWS(t *testing.T, url string, h http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	d := websocket.Dialer{HandshakeTimeout: 2 * time.Second, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // the sim's self-signed identity
	c, resp, err := d.Dial(url, h)
	if c != nil {
		t.Cleanup(func() { _ = c.Close() })
	}
	return c, resp, err
}

func basicHeader(user, pass string) http.Header {
	return http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))}}
}

func send(t *testing.T, c *websocket.Conn, frame string) {
	t.Helper()
	if err := c.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatal(err)
	}
}

func next(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("frame %s: %v", data, err)
	}
	return m
}

func TestWebSocketFaceSubscribesPushesAndAnswersByID(t *testing.T) {
	dev, addr := newWSDevice(t, HTTPAuth{}, false)
	c, _, err := dialWS(t, "ws://"+addr+"/api/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	send(t, c, `{"type":"subscribe","paths":["audio","secret"]}`)
	if m := next(t, c); m["type"] != "subscribed" || len(m["paths"].([]any)) != 1 {
		t.Fatalf("ack = %v, want the watched path alone", m)
	}
	send(t, c, `{"type":"get","path":"audio.volume","id":"g1"}`)
	if m := next(t, c); m["type"] != "result" || m["id"] != "g1" || m["value"] != float64(30) {
		t.Fatalf("get = %v", m)
	}
	// A set pushes its change before it answers, and the answer carries the
	// set's id.
	send(t, c, `{"type":"set","path":"audio.volume","value":44,"id":"s1"}`)
	if m := next(t, c); m["type"] != "change" || m["path"] != "audio.volume" || m["value"] != float64(44) {
		t.Fatalf("push = %v", m)
	}
	if m := next(t, c); m["type"] != "result" || m["id"] != "s1" || m["value"] != float64(44) {
		t.Fatalf("set = %v", m)
	}
	if v := dev.ReadPath("audio.volume"); v != 44 {
		t.Fatalf("state = %v (%T), want 44 held as an integer", v, v)
	}
	// A refusal carries the id; a path outside the face is no path at all.
	for frame, want := range map[string]string{
		`{"type":"set","path":"power","value":"off","id":"s2"}`: "not settable",
		`{"type":"get","path":"secret","id":"g2"}`:              "no such path",
		`{"type":"reboot","id":"r1"}`:                           "unknown message type",
	} {
		send(t, c, frame)
		m := next(t, c)
		if m["type"] != "error" || m["id"] == nil || !strings.Contains(m["error"].(string), want) {
			t.Fatalf("%s = %v, want an error naming %q with its id", frame, m, want)
		}
	}
	// A change made elsewhere (a route, a command, the console) pushes too.
	if err := dev.applyExternalDiffs(map[string]any{"audio.volume": 12}, 9); err != nil {
		t.Fatal(err)
	}
	if m := next(t, c); m["type"] != "change" || m["value"] != float64(12) {
		t.Fatalf("an external change = %v", m)
	}
	if dev.WebSocketSubscribes() != 1 || dev.WebSocketUpgrades() != 1 {
		t.Fatalf("subscribes %d upgrades %d, want 1 and 1", dev.WebSocketSubscribes(), dev.WebSocketUpgrades())
	}
}

func TestWebSocketFaceRefusesAWrongCredentialBeforeTheUpgrade(t *testing.T) {
	dev, addr := newWSDevice(t, HTTPAuth{Mode: HTTPAuthBasic, User: "admin", Password: "kestrel"}, false)
	_, resp, err := dialWS(t, "ws://"+addr+"/api/ws", basicHeader("admin", "wrong"))
	if !errors.Is(err, websocket.ErrBadHandshake) || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong credential = %v, %v, want a 401", resp, err)
	}
	if _, _, err := dialWS(t, "ws://"+addr+"/api/ws", basicHeader("admin", "kestrel")); err != nil {
		t.Fatalf("the right credential = %v", err)
	}
	// The client's dial returns once it reads the 101; the face counts the
	// upgrade after its own Upgrade returns, so the count is waited for, not
	// read once (#1451).
	for deadline := time.Now().Add(3 * time.Second); dev.WebSocketUpgrades() == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if dev.WebSocketAttempts() != 2 || dev.WebSocketRefusals() != 1 || dev.WebSocketUpgrades() != 1 {
		t.Fatalf("attempts %d refusals %d upgrades %d, want 2, 1, 1", dev.WebSocketAttempts(), dev.WebSocketRefusals(), dev.WebSocketUpgrades())
	}
}

func TestWebSocketFaceDropsEverySocketFromTheConsole(t *testing.T) {
	dev, addr := newWSDevice(t, HTTPAuth{}, false)
	c, _, err := dialWS(t, "ws://"+addr+"/api/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	send(t, c, `{"type":"subscribe","paths":["power"]}`)
	next(t, c)
	if res := dev.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/admin/sockets/drop"}); res.Status != 200 || !strings.Contains(res.Body, `"dropped":1`) {
		t.Fatalf("drop = %+v", res)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = c.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != websocket.CloseGoingAway {
		t.Fatalf("after the drop = %v, want a going-away close", err)
	}
}

func TestWebSocketFaceServesOverTheDevicesTLSIdentity(t *testing.T) {
	_, addr := newWSDevice(t, HTTPAuth{}, true)
	c, _, err := dialWS(t, "wss://"+addr+"/api/ws", nil)
	if err != nil {
		t.Fatalf("wss = %v", err)
	}
	send(t, c, `{"type":"get","path":"power","id":"t"}`)
	if m := next(t, c); m["value"] != "on" || m["id"] != "t" {
		t.Fatalf("get over wss = %v", m)
	}
	cs := c.UnderlyingConn().(*tls.Conn).ConnectionState()
	if len(cs.PeerCertificates) == 0 {
		t.Fatal("no certificate offered")
	}
}
