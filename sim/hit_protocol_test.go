package sim

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// Every hit a face reports names a protocol the hub counts
// (collection.SimProtocols). The websocket face reported "websocket" while the
// hub kept a list without it, so the hub dropped each one and a websocket
// component showed no hits (#1366). A real face is dialed here, and what it
// reports on every path (the upgrade's request log, a frame's request event,
// and the console's drop operation) is held to the hub's vocabulary.
func TestAWebSocketFacesHitsNameAProtocolTheHubCounts(t *testing.T) {
	// The hooks are set before the listener starts, and the hits are guarded:
	// the face reports from the accept loop, the handler and the socket's own
	// goroutines, none of them the test's.
	dev := NewDevice("d", "ws-host", net.ParseIP("127.0.0.1"), "p", 1,
		map[string]any{"power": "on", "audio": map[string]any{"volume": 30}, "secret": "x"})
	var mu sync.Mutex
	var hits []Hit
	dev.Logger = func(l RequestLog) {
		mu.Lock()
		defer mu.Unlock()
		hits = append(hits, hitFromLog(l))
	}
	dev.RequestPublisher = func(ev RequestEvent) {
		mu.Lock()
		defer mu.Unlock()
		hits = append(hits, hitFromEvent(ev))
	}
	dev.SetWebSocket(wsTestConfig(), HTTPAuth{})
	addr, err := dev.StartWebSocket(0, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopWebSocket)

	c, _, err := dialWS(t, "ws://"+addr.String()+"/api/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	send(t, c, `{"type":"get","path":"audio.volume","id":"g1"}`)
	next(t, c)
	// The console's drop of every socket reports its own operation hit.
	if res := dev.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/admin/sockets/drop"}); res.Status != 200 {
		t.Fatalf("drop = %+v", res)
	}

	// The socket reports a frame's event just after it queues the reply, so
	// the test waits for the face's hits rather than reading them on the reply.
	snapshot := func() (all []Hit, byWebSocket int) {
		mu.Lock()
		defer mu.Unlock()
		for _, h := range hits {
			if h.Protocol == "websocket" {
				byWebSocket++
			}
		}
		return append([]Hit(nil), hits...), byWebSocket
	}
	deadline := time.Now().Add(5 * time.Second)
	var all []Hit
	var byWebSocket int
	for {
		all, byWebSocket = snapshot()
		if byWebSocket >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, h := range all {
		if !collection.IsSimProtocol(h.Protocol) {
			t.Errorf("a hit names protocol %q (%s %s), which the hub rejects", h.Protocol, h.Method, h.Path)
		}
	}
	// The socket's own connection is a tcp hit; the requests on it are the
	// face's: the upgrade's log, the frame's event and the console's drop.
	if byWebSocket < 3 {
		t.Errorf("%d hits by websocket of %d, want the upgrade, the frame and the drop operation", byWebSocket, len(all))
	}
}
