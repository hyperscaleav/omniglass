package sim

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// The websocket face (#1034): a simulated component's control socket, a
// JSON message per text frame, for the collector's held websocket to meet
// on a real wire. Its protocol is the shape a component's live API takes:
//
//   - {"type":"subscribe","paths":[...]} is acknowledged with
//     {"type":"subscribed","paths":[...]}, and from then on every change to
//     a watched path is pushed as {"type":"change","path":...,"value":...};
//   - {"type":"get","path":...,"id":...} is answered with
//     {"type":"result","id":...,"value":...}, naming no path, so a client
//     that claims pushes by their path never takes an answer for one;
//   - {"type":"set","path":...,"value":...,"id":...} writes a settable
//     path, pushes the change to every subscriber (this socket included,
//     so the push lands before the result), then answers with the result;
//   - anything else is {"type":"error","id":...,"error":...}, the id echoed
//     when the frame carried one.
//
// The upgrade sits behind the face's own auth (basic, a header key, a
// bearer token or a query key), a 401 refusing it before any socket opens,
// and the console can drop every socket, as a restart would.

// wsReadLimit bounds a frame the face reads: a control message is small.
const wsReadLimit = 64 << 10

// wsQueue is how many frames a socket may have waiting to be written; one
// that falls further behind is dropped, and its client reconnects.
const wsQueue = 64

// wsFace is a device's websocket face: its protocol, its auth, the sockets
// connected now, and what it was sent.
type wsFace struct {
	d    *Device
	cfg  profile.WebSocket
	auth HTTPAuth
	up   websocket.Upgrader

	mu    sync.Mutex
	socks map[*wsSock]struct{}

	srv     *http.Server
	lis     net.Listener
	serving sync.WaitGroup

	attempts   atomic.Int64
	upgrades   atomic.Int64
	refusals   atomic.Int64
	subscribes atomic.Int64
}

// wsSock is one connected socket: the frames it has yet to write, the paths
// it subscribed to, and its end.
type wsSock struct {
	c      *websocket.Conn
	out    chan []byte
	stop   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	paths  []string
	remote string
}

func (s *wsSock) end() { s.once.Do(func() { close(s.stop) }) }

// enqueue hands a frame to the socket's writer without blocking: a socket
// that has fallen behind is ended.
func (s *wsSock) enqueue(frame []byte) {
	select {
	case s.out <- frame:
	case <-s.stop:
	default:
		s.end()
	}
}

func (s *wsSock) watching(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.paths {
		if path == p || strings.HasPrefix(path, p+".") {
			return true
		}
	}
	return false
}

// SetWebSocket gives the device a websocket face, its auth the profile's.
// Call before the face starts.
func (d *Device) SetWebSocket(cfg profile.WebSocket, auth HTTPAuth) {
	if cfg.Path == "" {
		d.ws = nil
		return
	}
	d.ws = &wsFace{d: d, cfg: cfg, auth: auth, socks: map[*wsSock]struct{}{}}
}

// watches reports a state path the face lets a subscriber watch and a get
// read: one of the declared paths or beneath one; every path when none is
// declared.
func (f *wsFace) watches(path string) bool {
	if len(f.cfg.Paths) == 0 {
		return true
	}
	for _, p := range f.cfg.Paths {
		if path == p || strings.HasPrefix(path, p+".") {
			return true
		}
	}
	return false
}

// fanout pushes a batch's changes, in path order, to every socket watching
// them. Called under the device's write lock; never blocks.
func (f *wsFace) fanout(diffs map[string]any) {
	paths := make([]string, 0, len(diffs))
	for p := range diffs {
		if f.watches(p) {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return
	}
	sort.Strings(paths)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range paths {
		frame, err := json.Marshal(map[string]any{"type": "change", "path": p, "value": diffs[p]})
		if err != nil {
			continue
		}
		for s := range f.socks {
			if s.watching(p) {
				s.enqueue(frame)
			}
		}
	}
}

// drop closes every socket with a going-away close, as a restart would,
// and reports how many.
func (f *wsFace) drop() int {
	f.mu.Lock()
	socks := make([]*wsSock, 0, len(f.socks))
	for s := range f.socks {
		socks = append(socks, s)
	}
	f.mu.Unlock()
	for _, s := range socks {
		_ = s.c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, "restarting"), time.Now().Add(time.Second))
		s.end()
		_ = s.c.Close()
	}
	return len(socks)
}

// ServeHTTP answers the face's one path: the upgrade, behind the face's
// auth.
func (f *wsFace) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d := f.d
	if r.URL.Path != f.cfg.Path {
		http.NotFound(w, r)
		return
	}
	f.attempts.Add(1)
	rec := &statusRecorder{ResponseWriter: w, bodyCap: httpBodyCaptureCap}
	if !d.checkStaticAuth(rec, r, f.auth) {
		f.refusals.Add(1)
		f.log(r, rec.status)
		return
	}
	conn, err := f.up.Upgrade(w, r, nil)
	if err != nil {
		// The upgrader answered the refusal itself (a request that is not
		// an upgrade, or an origin it will not admit).
		f.log(r, http.StatusBadRequest)
		return
	}
	f.upgrades.Add(1)
	f.log(r, http.StatusSwitchingProtocols)
	conn.SetReadLimit(wsReadLimit)
	s := &wsSock{c: conn, out: make(chan []byte, wsQueue), stop: make(chan struct{}), remote: r.RemoteAddr}
	f.mu.Lock()
	f.socks[s] = struct{}{}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.socks, s)
		f.mu.Unlock()
		s.end()
		_ = conn.Close()
	}()
	go f.write(s)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		f.answer(s, data)
	}
}

// write is a socket's writer: every frame it is handed, in order, until it
// ends.
func (f *wsFace) write(s *wsSock) {
	for {
		select {
		case frame := <-s.out:
			_ = s.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := s.c.WriteMessage(websocket.TextMessage, frame); err != nil {
				s.end()
				_ = s.c.Close()
				return
			}
		case <-s.stop:
			_ = s.c.Close()
			return
		}
	}
}

// wsMessage is a frame the face reads.
type wsMessage struct {
	Type  string          `json:"type"`
	ID    *string         `json:"id"`
	Path  string          `json:"path"`
	Paths []string        `json:"paths"`
	Value json.RawMessage `json:"value"`
}

// answer reads one frame and answers it.
func (f *wsFace) answer(s *wsSock, data []byte) {
	var m wsMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		f.reply(s, nil, map[string]any{"type": "error", "error": "the frame is not a JSON object"})
		f.publish(s, "FRAME", "", http.StatusBadRequest)
		return
	}
	switch m.Type {
	case "subscribe":
		paths := slices.DeleteFunc(slices.Clone(m.Paths), func(p string) bool { return p == "" || !f.watches(p) })
		if len(paths) == 0 {
			f.reply(s, m.ID, map[string]any{"type": "error", "error": "subscribe to one or more of the paths this socket streams"})
			f.publish(s, "SUBSCRIBE", strings.Join(m.Paths, ","), http.StatusNotFound)
			return
		}
		s.mu.Lock()
		s.paths = paths
		s.mu.Unlock()
		f.subscribes.Add(1)
		f.reply(s, m.ID, map[string]any{"type": "subscribed", "paths": paths})
		f.publish(s, "SUBSCRIBE", strings.Join(paths, ","), http.StatusOK)
	case "get":
		v, ok := f.read(m.Path)
		if !ok {
			f.reply(s, m.ID, map[string]any{"type": "error", "error": "no such path: " + m.Path})
			f.publish(s, "GET", m.Path, http.StatusNotFound)
			return
		}
		f.reply(s, m.ID, map[string]any{"type": "result", "value": v})
		f.publish(s, "GET", m.Path, http.StatusOK)
	case "set":
		v, err := f.set(m.Path, m.Value)
		if err != nil {
			f.reply(s, m.ID, map[string]any{"type": "error", "error": err.Error()})
			f.publish(s, "SET", m.Path, http.StatusBadRequest)
			return
		}
		f.reply(s, m.ID, map[string]any{"type": "result", "value": v})
		f.publish(s, "SET", m.Path, http.StatusOK)
	default:
		f.reply(s, m.ID, map[string]any{"type": "error", "error": fmt.Sprintf("unknown message type %q", m.Type)})
		f.publish(s, "FRAME", m.Type, http.StatusBadRequest)
	}
}

// reply queues an answer, echoing the id the frame carried.
func (f *wsFace) reply(s *wsSock, id *string, body map[string]any) {
	if id != nil {
		body["id"] = *id
	}
	frame, err := json.Marshal(body)
	if err != nil {
		return
	}
	s.enqueue(frame)
}

// read is a get: a watched path the device holds a value at.
func (f *wsFace) read(path string) (any, bool) {
	if path == "" || !f.watches(path) {
		return nil, false
	}
	v := f.d.ReadPath(path)
	if v == nil {
		return nil, false
	}
	if _, branch := v.(map[string]any); branch {
		return nil, false
	}
	return v, true
}

// set writes a settable path and pushes the change, as any mutation does:
// the device's version moves, the diff publishes, and every subscriber
// (the asking socket among them) is handed the change before the answer.
func (f *wsFace) set(path string, raw json.RawMessage) (any, error) {
	if !slices.Contains(f.cfg.Settable, path) {
		return nil, fmt.Errorf("%s is not settable", path)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("a set carries a value")
	}
	v, err := wsValue(raw)
	if err != nil {
		return nil, err
	}
	d := f.d
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := SetPath(d.state, path, v); err != nil {
		return nil, err
	}
	diffs := map[string]any{path: v}
	d.Version++
	if d.DiffPublisher != nil {
		d.DiffPublisher(StateChange{DeviceID: d.DeviceID, Version: d.Version, Diffs: diffs, Cause: "websocket:set " + path})
	}
	d.fanoutToSessions(diffs)
	return v, nil
}

// wsValue reads a set's value: a whole number as an integer (as a
// profile's defaults hold one), another number as a float, and a string, a
// boolean or null as itself.
func wsValue(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("the value is not JSON")
	}
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return int(n), nil
		}
		fl, err := t.Float64()
		if err != nil {
			return nil, fmt.Errorf("the value is not a number")
		}
		return fl, nil
	case map[string]any, []any:
		return nil, fmt.Errorf("a set writes one value, not an object or a list")
	}
	return v, nil
}

// log reports an upgrade attempt to the hit reporter.
func (f *wsFace) log(r *http.Request, status int) {
	d := f.d
	if d.Logger == nil {
		return
	}
	d.Logger(RequestLog{DeviceID: d.DeviceID, Hostname: d.Hostname, Protocol: "websocket", Port: f.port(),
		Method: r.Method, Path: r.URL.Path, Remote: r.RemoteAddr, Status: status, ReqHeaders: redactAuth(r.Header)})
}

// publish reports one frame to the hit reporter: its type, its path and
// how it was answered.
func (f *wsFace) publish(s *wsSock, method, path string, status int) {
	d := f.d
	if d.RequestPublisher == nil {
		return
	}
	d.RequestPublisher(RequestEvent{DeviceID: d.DeviceID, Hostname: d.Hostname, Protocol: "websocket",
		Method: method, Port: f.port(), Path: path, Status: status, Remote: s.remote})
}

// redactAuth copies a request's headers with its credential masked.
func redactAuth(h http.Header) http.Header {
	out := h.Clone()
	if out.Get("Authorization") != "" {
		out.Set("Authorization", "[redacted]")
	}
	return out
}

func (f *wsFace) port() int {
	if f.lis == nil {
		return 0
	}
	return boundPort(f.lis.Addr())
}

// startWebSocket binds the face's listener, behind TLS when cfg is set,
// and returns the address it bound.
func (d *Device) startWebSocket(port int, tlsCfg *tls.Config) (net.Addr, error) {
	f := d.ws
	if f == nil {
		return nil, fmt.Errorf("the device has no websocket face")
	}
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	srv := &http.Server{Handler: f, TLSConfig: tlsCfg,
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
		ConnState: func(c net.Conn, s http.ConnState) {
			if s == http.StateNew {
				d.logConnect(c)
			}
		}}
	f.srv, f.lis = srv, lis
	f.serving.Add(1)
	go func() {
		defer f.serving.Done()
		var err error
		if tlsCfg != nil {
			err = srv.ServeTLS(lis, "", "")
		} else {
			err = srv.Serve(lis)
		}
		if err != nil && !strings.Contains(err.Error(), "closed") {
			slog.Error("device websocket error", "device", d.Hostname, "error", err)
		}
	}()
	return lis.Addr(), nil
}

// StartWebSocket starts the device's websocket face on port, plain or (with
// tls) under the device's https identity: a test's way in, and a restart's.
func (d *Device) StartWebSocket(port int, withTLS bool) (net.Addr, error) {
	if !withTLS {
		return d.startWebSocket(port, nil)
	}
	cfg, err := d.httpsServerConfig(0)
	if err != nil {
		return nil, fmt.Errorf("https identity: %w", err)
	}
	return d.startWebSocket(port, cfg)
}

// StopWebSocket closes the face's listener and every socket, as a component
// going down would.
func (d *Device) StopWebSocket() {
	f := d.ws
	if f == nil || f.srv == nil {
		return
	}
	f.drop()
	_ = f.srv.Close()
	_ = f.lis.Close()
	f.serving.Wait()
	f.srv = nil
}

// WebSocketAttempts counts the upgrades the face was sent, refused or not.
func (d *Device) WebSocketAttempts() int64 {
	if d.ws == nil {
		return 0
	}
	return d.ws.attempts.Load()
}

// WebSocketUpgrades counts the sockets the face opened.
func (d *Device) WebSocketUpgrades() int64 {
	if d.ws == nil {
		return 0
	}
	return d.ws.upgrades.Load()
}

// WebSocketRefusals counts the upgrades the face's auth refused.
func (d *Device) WebSocketRefusals() int64 {
	if d.ws == nil {
		return 0
	}
	return d.ws.refusals.Load()
}

// WebSocketSubscribes counts the subscriptions the face acknowledged.
func (d *Device) WebSocketSubscribes() int64 {
	if d.ws == nil {
		return 0
	}
	return d.ws.subscribes.Load()
}

// DropWebSockets closes every connected socket, as a restart would, and
// reports how many.
func (d *Device) DropWebSockets() int {
	if d.ws == nil {
		return 0
	}
	return d.ws.drop()
}

// operateSocketsDrop answers the console's drop of every socket.
func (d *Device) operateSocketsDrop() OperationResult {
	n := d.DropWebSockets()
	d.publishOperation("websocket", http.MethodPost, d.ws.cfg.DropPath, http.StatusOK)
	return OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"dropped":%d}`, n) + "\n"}
}
