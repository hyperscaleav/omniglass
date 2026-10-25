package driver_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The websocket transport (#1034, slice 4 of #1113): a held session whose
// messages are text frames. A request is a frame, a line verbatim or a JSON
// body rendered injection-safe; its reply is the frame carrying back an id
// the node mints into a declared field, whatever pushes interleave; and the
// upgrade rides the session's scheme or login.

func wsCatalog() fakeCatalog {
	c := httpCatalog()
	c.lanes["mute"] = "property"
	return c
}

// wsSpec is a credentialed wss driver: a subscription armed on every
// connection, pushes claimed by a field, a poll and a command answered by
// their minted id.
func wsSpec() *driver.Spec {
	return &driver.Spec{
		Version:   1,
		Transport: "wss",
		Inputs: []driver.Input{
			{Name: "host", Kind: "string", Required: true},
			{Name: "port", Kind: "number", Default: "9443"},
			{Name: driver.TLSModeInput, Kind: "string", Default: driver.TLSModePin},
			{Name: "login", Kind: "secret", SecretType: "basic-auth", Required: true},
		},
		Session: &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic, Upgrade: &driver.Request{Path: "/ws"}},
		Listeners: []driver.Listener{{
			Name:  "changes",
			Arm:   []string{`{"type":"subscribe","paths":["status.audio"]}`},
			Match: driver.Match{Field: &driver.FieldMatch{Path: "type", Equals: "change"}},
			Emits: []driver.Emit{{Name: "volume", Extract: driver.Extract{JSONPath: "value"}}},
		}},
		Polls: []driver.Poll{{
			Name: "volume", Schedule: driver.Schedule{Every: "30s"},
			Request: driver.Request{Body: json.RawMessage(`{"type":"get","path":"status.audio.volume"}`), Reply: &driver.Reply{Correlate: "id"}},
			Emits:   []driver.Emit{{Name: "volume", Extract: driver.Extract{JSONPath: "value"}}},
		}},
		Commands: []driver.CommandBinding{{
			CommandType: "set-volume",
			Request: driver.Request{Body: json.RawMessage(`{"type":"set","path":"status.audio.volume","value":"${value}"}`),
				Reply: &driver.Reply{Correlate: "id", Error: &driver.Match{Regex: `"type":"error"`}}},
			Settle: &driver.Settle{From: driver.SettleFromResponse, Extract: &driver.Extract{JSONPath: "value"}},
		}},
	}
}

func TestAWebSocketDriverValidates(t *testing.T) {
	if err := wsSpec().Validate(wsCatalog()); err != nil {
		t.Fatalf("validate = %v, want clean", err)
	}
	plain := wsSpec()
	plain.Transport = "websocket"
	plain.Inputs = append(plain.Inputs[:2], plain.Inputs[3])
	if err := plain.Validate(wsCatalog()); err != nil {
		t.Fatalf("plain websocket = %v, want clean", err)
	}
	// A text frame and its reply shape, as a line protocol's.
	text := wsSpec()
	text.Polls[0].Request = driver.Request{Line: "GET VOLUME", Reply: &driver.Reply{Prefix: "VOLUME "}}
	text.Polls[0].Emits[0].Extract = driver.Extract{Regex: `^VOLUME (\d+)$`}
	if err := text.Validate(wsCatalog()); err != nil {
		t.Fatalf("a text frame = %v, want clean", err)
	}
	// No credential, no upgrade path declared: the root, open.
	open := wsSpec()
	open.Session = nil
	open.Inputs = open.Inputs[:3]
	if err := open.Validate(wsCatalog()); err != nil {
		t.Fatalf("an open websocket = %v, want clean", err)
	}
	// Behind an http login, its cookie riding the upgrade.
	login := wsSpec()
	login.Session = &driver.SessionSpec{Upgrade: &driver.Request{Path: "/ws"}, Login: &driver.Login{Path: "/login",
		Steps: []driver.LoginStep{{Request: &driver.Request{Method: "POST", Path: "/login", BodyFormat: driver.BodyFormatForm,
			Body: json.RawMessage(`{"username":"${secret.login.username}","password":"${secret.login.password}"}`)}}}}}
	if err := login.Validate(wsCatalog()); err != nil {
		t.Fatalf("a websocket behind a login = %v, want clean", err)
	}
	if !driver.ResolveLadder("wss", nil, wsSpec().Session).Session {
		t.Fatal("a credentialed websocket declares no session rung")
	}
}

func TestWebSocketRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*driver.Spec)
		want   string
	}{
		{"a correlated line", func(s *driver.Spec) {
			s.Polls[0].Request = driver.Request{Line: "GET", Reply: &driver.Reply{Correlate: "id"}}
		}, "correlate"},
		{"a correlated body that is not an object", func(s *driver.Spec) {
			s.Polls[0].Request.Body = json.RawMessage(`["get"]`)
		}, "object"},
		{"a line and a body", func(s *driver.Spec) { s.Polls[0].Request.Line = "GET" }, "line or a body"},
		{"a frame with a path", func(s *driver.Spec) { s.Polls[0].Request.Path = "/x" }, "path"},
		{"a form frame", func(s *driver.Spec) { s.Polls[0].Request.BodyFormat = driver.BodyFormatForm }, "json"},
		{"a malformed correlate path", func(s *driver.Spec) { s.Polls[0].Request.Reply.Correlate = "a..b" }, "correlate"},
		{"a correlate beside a prefix", func(s *driver.Spec) { s.Polls[0].Request.Reply.Prefix = "{" }, "one"},
		{"a digest upgrade", func(s *driver.Spec) { s.Session.Scheme = driver.SchemeDigest }, "digest"},
		{"a stream on a websocket", func(s *driver.Spec) { s.Session.Stream = &driver.Request{Path: "/e"} }, "stream"},
		{"an upgrade posted", func(s *driver.Spec) { s.Session.Upgrade.Method = "POST" }, "GET"},
		{"an upgrade with a body", func(s *driver.Spec) { s.Session.Upgrade.Body = json.RawMessage(`{}`) }, "body"},
		{"an upgrade setting Upgrade", func(s *driver.Spec) { s.Session.Upgrade.Headers = map[string]string{"Upgrade": "x"} }, "Upgrade"},
		{"an upgrade setting the key", func(s *driver.Spec) {
			s.Session.Upgrade.Headers = map[string]string{"Sec-WebSocket-Key": "x"}
		}, "Sec-WebSocket-Key"},
		{"a prompt login", func(s *driver.Spec) {
			s.Session = &driver.SessionSpec{Login: &driver.Login{Steps: []driver.LoginStep{{Expect: driver.Match{Prefix: "login:"}, Send: "${secret.login.username}"}}, Ready: driver.Match{Prefix: "OK"}}}
		}, "request"},
		{"a pty", func(s *driver.Spec) { s.Session.PTY = true }, "pty"},
		{"a re-probe", func(s *driver.Spec) { s.Session.Reprobe = "10m" }, "reprobe"},
		{"an api key in a header the handshake owns", func(s *driver.Spec) {
			s.Inputs[3] = driver.Input{Name: "login", Kind: "secret", SecretType: "api-key", Required: true}
			s.Session = &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeAPIKey, Upgrade: &driver.Request{Path: "/ws"},
				APIKey: &driver.APIKeyPlacement{In: driver.APIKeyInHeader, Name: "Sec-WebSocket-Version"}}
		}, "Sec-WebSocket-Version"},
		{"a login header the handshake owns", func(s *driver.Spec) {
			s.Session = &driver.SessionSpec{Upgrade: &driver.Request{Path: "/ws"}, Login: &driver.Login{Path: "/login",
				Steps:   []driver.LoginStep{{Request: &driver.Request{Method: "POST", Path: "/login", Body: json.RawMessage(`{"u":"${secret.login.username}"}`)}}},
				Capture: []driver.Capture{{Name: "token", Extract: driver.Extract{JSONPath: "token"}}},
				Headers: map[string]string{"Sec-WebSocket-Extensions": "${session.token}"}}}
		}, "Sec-WebSocket-Extensions"},
		{"a correlate through a field that is not an object", func(s *driver.Spec) {
			s.Polls[0].Request.Body = json.RawMessage(`{"type":"get","meta":"keep-me"}`)
			s.Polls[0].Request.Reply.Correlate = "meta.id"
		}, "meta"},
		{"a body that sets the correlated field", func(s *driver.Spec) {
			s.Polls[0].Request.Body = json.RawMessage(`{"type":"get","id":"fixed"}`)
		}, "id"},
		{"an arm frame across lines", func(s *driver.Spec) {
			s.Listeners[0].Arm = []string{"{\n  \"type\": \"subscribe\"\n}"}
		}, "line break"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := wsSpec()
			tc.mutate(s)
			err := s.Validate(wsCatalog())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
	tcp := validSpec()
	tcp.Session = &driver.SessionSpec{Upgrade: &driver.Request{Path: "/ws"}}
	if err := tcp.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("an upgrade over tcp = %v, want a refusal", err)
	}
	corr := validSpec()
	corr.Polls[0].Request.Reply = &driver.Reply{Correlate: "id"}
	if err := corr.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "correlate") {
		t.Fatalf("a correlate over tcp = %v, want a refusal", err)
	}
	// An http request is answered by its own response: no reply shape, a
	// body beside it or not.
	for _, rep := range []*driver.Reply{{Correlate: "id"}, {Prefix: "{"}, {Field: &driver.FieldMatch{Path: "ok", Equals: "true"}}} {
		h := httpSpec()
		h.Commands[0].Request.Reply = rep
		if err := h.Validate(httpCatalog()); err == nil || !strings.Contains(err.Error(), "reply") {
			t.Fatalf("an http command with reply %+v = %v, want a refusal", rep, err)
		}
	}
}

func TestAFrameAndItsCorrelatedReply(t *testing.T) {
	req := wsSpec().Polls[0].Request
	frame, err := req.Frame()
	if err != nil || frame != `{"type":"get","path":"status.audio.volume"}` {
		t.Fatalf("frame = %q, %v", frame, err)
	}
	sent, reply, err := req.Reply.Correlated(frame, "k7")
	if err != nil || sent != `{"id":"k7","path":"status.audio.volume","type":"get"}` {
		t.Fatalf("correlated frame = %q, %v", sent, err)
	}
	for frame, want := range map[string]bool{
		`{"id":"k7","type":"result","value":42}`:         true,
		`{"id":"k8","type":"result","value":42}`:         false,
		`{"type":"change","path":"status.audio.volume"}`: false,
		`not json`: false,
	} {
		if got := reply.Accepts(frame); got != want {
			t.Errorf("accepts %s = %v, want %v", frame, got, want)
		}
	}
	// A nested field, and the error shape kept: a refusal carrying the id
	// is the exchange's refusal, never its answer, and one carrying another
	// exchange's id is neither.
	nested := &driver.Reply{Correlate: "meta.id", Error: &driver.Match{Regex: `"error"`}}
	sent, reply, err = nested.Correlated(`{"op":"set"}`, "z1")
	if err != nil || sent != `{"meta":{"id":"z1"},"op":"set"}` {
		t.Fatalf("nested = %q, %+v, %v", sent, reply, err)
	}
	refusal, other := `{"meta":{"id":"z1"},"error":"no"}`, `{"meta":{"id":"z2"},"error":"no"}`
	if !reply.Fails(refusal) || reply.Accepts(refusal) || reply.Fails(other) || reply.Accepts(other) {
		t.Fatalf("a correlated refusal: fails %v accepts %v; another's: fails %v accepts %v",
			reply.Fails(refusal), reply.Accepts(refusal), reply.Fails(other), reply.Accepts(other))
	}
	// Bind mints a fresh id per exchange.
	a, ra, err := wsSpec().Polls[0].Request.Bind()
	b, _, _ := wsSpec().Polls[0].Request.Bind()
	if err != nil || a == b || !ra.Accepts(a) {
		t.Fatalf("bind = %q, %q, %v", a, b, err)
	}
	// A path through a value that is not an object is refused, never
	// overwritten (a rendered ${value} can land one there).
	if _, _, err := nested.Correlated(`{"meta":[1,2]}`, "z2"); err == nil {
		t.Fatal("a correlation through a list overwrote it")
	}
	// A reply with no correlation binds nothing.
	plain := &driver.Reply{Prefix: "OK"}
	if sent, got, err := plain.Correlated("PING", "x"); err != nil || sent != "PING" || got != plain {
		t.Fatalf("an uncorrelated reply = %q, %+v, %v", sent, got, err)
	}
	if _, _, err := req.Reply.Correlated("not json", "x"); err == nil {
		t.Fatal("a correlated frame that is not json bound")
	}
	// A line, and a JSON frame, read by jsonpath as by regex.
	fn := &driver.BakedFunction{Driver: "d", Function: "v", Emits: []driver.BakedEmit{{Name: "volume", Lane: "metric", Extract: driver.Extract{JSONPath: "value"}}}}
	got, faults := fn.Interpret(driver.LineMessage(`{"type":"change","value":9}`).Payload(), time.Now())
	if len(faults) != 0 || len(got) != 1 || got[0].Value != 9 {
		t.Fatalf("a json frame = %+v, %v", got, faults)
	}
}

// A terminal rule that names a status judges the upgrade's answer alone: a
// frame has no status, so its text never rejects the session by a rule
// scoped to one.
func TestAStatusScopedRuleJudgesOnlyTheUpgrade(t *testing.T) {
	s := wsSpec()
	s.Session.Terminal = []driver.TerminalRule{
		{Status: 403, Match: driver.Match{Regex: "locked"}, Class: driver.ClassAuth},
		{Match: driver.Match{Prefix: `{"type":"fatal"`}, Class: driver.ClassProtocol},
	}
	if err := s.Validate(wsCatalog()); err != nil {
		t.Fatalf("validate = %v", err)
	}
	if class, ok := s.Session.TerminalFor(`{"type":"result","id":"k1","value":"locked"}`); ok {
		t.Fatalf("a healthy frame matched a status-scoped rule: %s", class)
	}
	if class, ok := s.Session.TerminalFor(`{"type":"fatal","error":"license expired"}`); !ok || class != driver.ClassProtocol {
		t.Fatalf("a statusless rule = %q, %v, want protocol", class, ok)
	}
	if class, ok := s.Session.TerminalForResponse(403, "account locked"); !ok || class != driver.ClassAuth {
		t.Fatalf("the upgrade's 403 = %q, %v, want auth", class, ok)
	}
}
