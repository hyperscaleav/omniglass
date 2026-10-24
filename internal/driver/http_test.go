package driver_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The http transport as a full collection transport (#902): a driver's
// polls and commands send real requests (method, path, query, headers,
// body) and read JSON, XML, HTML or text answers through the one extraction
// primitive, authenticated by the endpoint's declared scheme.

func httpCatalog() fakeCatalog {
	c := catalog()
	c.lanes["volume"] = "metric"
	c.lanes["power-state"] = "property"
	c.lanes["firmware-version"] = "property"
	c.commands["set-volume"] = true
	c.secrets["bearer-token"] = true
	c.secrets["api-key"] = true
	return c
}

// httpSpec is a credentialed http driver: a JSON poll, an XML poll, an HTML
// poll, and a command with a JSON body, basic auth declared in the session
// block.
func httpSpec() *driver.Spec {
	return &driver.Spec{
		Version:   1,
		Transport: "http",
		Inputs: []driver.Input{
			{Name: "host", Kind: "string", Required: true},
			{Name: "port", Kind: "number", Default: "80"},
			{Name: "login", Kind: "secret", SecretType: "basic-auth", Required: true},
		},
		Session: &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic},
		Polls: []driver.Poll{
			{
				Name: "status", Schedule: driver.Schedule{Every: "30s"},
				Request: driver.Request{Path: "/api/status", Query: map[string]string{"detail": "full"}, Headers: map[string]string{"Accept": "application/json"}},
				Emits: []driver.Emit{
					{Name: "power-state", Extract: driver.Extract{JSONPath: "device.power"}},
					{Name: "volume", Extract: driver.Extract{JSONPath: "audio.volume"}},
				},
			},
			{
				Name: "firmware", Schedule: driver.Schedule{Every: "1h"},
				Request: driver.Request{Path: "/info.xml", Format: driver.FormatXML, Namespaces: map[string]string{"k": "urn:kestrel:info"}},
				Emits:   []driver.Emit{{Name: "firmware-version", Extract: driver.Extract{XPath: "/k:info/k:firmware"}}},
			},
			{
				Name: "page", Schedule: driver.Schedule{Every: "5m"},
				Request: driver.Request{Path: "/", Format: driver.FormatHTML},
				Emits:   []driver.Emit{{Name: "video-input", Extract: driver.Extract{XPath: `//td[@id="input"]`}}},
			},
		},
		Commands: []driver.CommandBinding{{
			CommandType: "set-volume",
			Request:     driver.Request{Method: "POST", Path: "/api/audio/volume", Body: json.RawMessage(`{"volume":"${value}"}`)},
			Settle:      &driver.Settle{From: driver.SettleFromResponse, Extract: &driver.Extract{JSONPath: "volume"}},
		}},
	}
}

func TestAnHTTPDriverValidates(t *testing.T) {
	if err := httpSpec().Validate(httpCatalog()); err != nil {
		t.Fatalf("validate = %v, want clean", err)
	}
	s := httpSpec()
	s.Transport = "https"
	s.Inputs = append(s.Inputs, driver.Input{Name: driver.TLSModeInput, Kind: "string", Default: driver.TLSModePin})
	if err := s.Validate(httpCatalog()); err != nil {
		t.Fatalf("https with a tls-mode input = %v, want clean", err)
	}
	if err := s.CheckInputValue(driver.TLSModeInput, "skip"); err != nil {
		t.Fatalf("an https endpoint takes skip from an operator: %v", err)
	}
	if err := s.CheckInputValue(driver.TLSModeInput, "maybe"); err == nil {
		t.Fatal("an https endpoint took a tls mode that is not one")
	}
}

func TestHTTPRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*driver.Spec)
		want   string
	}{
		{"a poll with no path", func(s *driver.Spec) { s.Polls[0].Request.Path = "" }, "path"},
		{"a relative path", func(s *driver.Spec) { s.Polls[0].Request.Path = "api/status" }, "/"},
		{"a query in the path", func(s *driver.Spec) { s.Polls[0].Request.Path = "/api/status?x=1" }, "query"},
		{"a line on http", func(s *driver.Spec) { s.Polls[0].Request.Line = "GET" }, "line"},
		{"an oid emit", func(s *driver.Spec) { s.Polls[0].Emits[0].Extract = driver.Extract{OID: "1.3"} }, "oid"},
		{"an xpath emit on a json response", func(s *driver.Spec) { s.Polls[0].Emits[0].Extract = driver.Extract{XPath: "//a"} }, "format"},
		{"a jsonpath emit on an xml response", func(s *driver.Spec) { s.Polls[1].Emits[0].Extract = driver.Extract{JSONPath: "a"} }, "format"},
		{"an uncompilable xpath", func(s *driver.Spec) { s.Polls[1].Emits[0].Extract.XPath = "//k:info[" }, "xpath"},
		{"an xpath naming an undeclared prefix", func(s *driver.Spec) { s.Polls[1].Emits[0].Extract.XPath = "/z:info" }, "xpath"},
		{"namespaces on an html response", func(s *driver.Spec) { s.Polls[2].Request.Namespaces = map[string]string{"h": "x"} }, "namespaces"},
		{"an unknown format", func(s *driver.Spec) { s.Polls[0].Request.Format = "yaml" }, "yaml"},
		{"an unknown method", func(s *driver.Spec) { s.Polls[0].Request.Method = "FETCH" }, "FETCH"},
		{"a body on a GET", func(s *driver.Spec) { s.Polls[0].Request.Body = json.RawMessage(`{"a":1}`) }, "body"},
		{"an authorization header", func(s *driver.Spec) { s.Polls[0].Request.Headers["Authorization"] = "Basic eA==" }, "Authorization"},
		{"a cookie header", func(s *driver.Spec) { s.Polls[0].Request.Headers["cookie"] = "a=b" }, "cookie"},
		{"a header name that is not a token", func(s *driver.Spec) { s.Polls[0].Request.Headers["X Bad"] = "1" }, "X Bad"},
		{"a template in a poll", func(s *driver.Spec) { s.Polls[0].Request.Query["v"] = "${value}" }, "template"},
		{"a secret in a request", func(s *driver.Spec) {
			s.Commands[0].Request.Headers = map[string]string{"X-Key": "${secret.login.password}"}
		}, "secret"},
		{"an unknown body format", func(s *driver.Spec) { s.Commands[0].Request.BodyFormat = "protobuf" }, "protobuf"},
		{"a form body that is not an object of strings", func(s *driver.Spec) {
			s.Commands[0].Request.BodyFormat, s.Commands[0].Request.Body = driver.BodyFormatForm, json.RawMessage(`{"a":{"b":1}}`)
		}, "form"},
		{"a text body that is not a string", func(s *driver.Spec) {
			s.Commands[0].Request.BodyFormat, s.Commands[0].Request.Body = driver.BodyFormatText, json.RawMessage(`{"a":1}`)
		}, "string"},
		{"a command with no path", func(s *driver.Spec) { s.Commands[0].Request.Path = "" }, "path"},
		{"a command settling by an xpath on a json reply", func(s *driver.Spec) {
			s.Commands[0].Settle.Extract = &driver.Extract{XPath: "//v"}
		}, "format"},
		{"retries on http", func(s *driver.Spec) { n := 1; s.Polls[0].Request.Retries = &n }, "retries"},
		{"a reply shape on http", func(s *driver.Spec) { s.Polls[0].Request.Reply = &driver.Reply{Prefix: "x"} }, "reply"},
		// The credential reaches a node only with a task: a credentialed
		// driver with commands alone could never authenticate one.
		{"credentialed commands with no poll", func(s *driver.Spec) { s.Polls = nil }, "poll"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := httpSpec()
			tc.mutate(s)
			err := s.Validate(httpCatalog())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
}

// The request fields http reads mean nothing over another transport, so a
// spec carrying them there refuses rather than silently ignoring them.
func TestHTTPRequestFieldsAreHTTPs(t *testing.T) {
	for _, mutate := range []func(*driver.Request){
		func(r *driver.Request) { r.Method = "GET" },
		func(r *driver.Request) { r.Path = "/x" },
		func(r *driver.Request) { r.Query = map[string]string{"a": "b"} },
		func(r *driver.Request) { r.Headers = map[string]string{"A": "b"} },
		func(r *driver.Request) { r.Format = driver.FormatJSON },
	} {
		s := validSpec()
		mutate(&s.Polls[0].Request)
		if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "http") {
			t.Errorf("a tcp poll carrying an http field = %v, want a refusal naming http", err)
		}
	}
}

func TestHTTPSessionRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*driver.Spec)
		want   string
	}{
		{"a scheme with no credential", func(s *driver.Spec) { s.Session.Credential = "" }, "credential"},
		{"a credential with no scheme", func(s *driver.Spec) { s.Session.Scheme = "" }, "scheme"},
		{"an unknown scheme", func(s *driver.Spec) { s.Session.Scheme = "kerberos" }, "kerberos"},
		{"a scheme whose secret type does not fit", func(s *driver.Spec) { s.Session.Scheme = driver.SchemeBearer }, "bearer-token"},
		{"an api key with no placement", func(s *driver.Spec) {
			s.Inputs[2].SecretType, s.Session.Scheme = "api-key", driver.SchemeAPIKey
		}, "api_key"},
		{"an api key placed nowhere known", func(s *driver.Spec) {
			s.Inputs[2].SecretType, s.Session.Scheme = "api-key", driver.SchemeAPIKey
			s.Session.APIKey = &driver.APIKeyPlacement{In: "body", Name: "k"}
		}, "body"},
		{"an api key header that is reserved", func(s *driver.Spec) {
			s.Inputs[2].SecretType, s.Session.Scheme = "api-key", driver.SchemeAPIKey
			s.Session.APIKey = &driver.APIKeyPlacement{In: "header", Name: "Host"}
		}, "Host"},
		{"an api key placement on basic", func(s *driver.Spec) {
			s.Session.APIKey = &driver.APIKeyPlacement{In: "header", Name: "X-Key"}
		}, "api_key"},
		{"a request header shadowing the api key", func(s *driver.Spec) {
			s.Inputs[2].SecretType, s.Session.Scheme = "api-key", driver.SchemeAPIKey
			s.Session.APIKey = &driver.APIKeyPlacement{In: "header", Name: "X-Key"}
			s.Polls[0].Request.Headers["x-key"] = "literal"
		}, "x-key"},
		{"a login over http", func(s *driver.Spec) {
			s.Session.Login = &driver.Login{Steps: []driver.LoginStep{{Expect: driver.Match{Prefix: "a"}, Send: "b"}}, Ready: driver.Match{Prefix: "c"}}
		}, "login"},
		{"a re-probe over http", func(s *driver.Spec) { s.Session.Reprobe = "10m" }, "reprobe"},
		{"a terminal status out of range", func(s *driver.Spec) {
			s.Session.Terminal = []driver.TerminalRule{{Class: driver.ClassAuth, Status: 99}}
		}, "status"},
		{"a terminal rule declaring nothing", func(s *driver.Spec) {
			s.Session.Terminal = []driver.TerminalRule{{Class: driver.ClassAuth}}
		}, "terminal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := httpSpec()
			tc.mutate(s)
			err := s.Validate(httpCatalog())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
	// A status rule is http's: a line has no status.
	s := validSpec()
	s.Session = &driver.SessionSpec{Terminal: []driver.TerminalRule{{Class: driver.ClassAuth, Status: 403}}}
	if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("a status rule over tcp = %v, want a refusal naming status", err)
	}
	// A declared 403 is the driver's way of saying its gear refuses a
	// credential with it (decision 5 of #1113).
	ok := httpSpec()
	ok.Session.Terminal = []driver.TerminalRule{{Class: driver.ClassAuth, Status: 403}, {Class: driver.ClassAuth, Match: driver.Match{Regex: "locked"}}}
	if err := ok.Validate(httpCatalog()); err != nil {
		t.Fatalf("declared terminal statuses = %v, want clean", err)
	}
}

// A listener rides a held line session or udp's own socket; anywhere else
// nothing runs it, so it refuses at write rather than deriving a task no node
// ever runs (#1115).
func TestAListenerRefusesATransportThatCannotRunIt(t *testing.T) {
	listener := driver.Listener{Name: "events", Match: driver.Match{Prefix: "x"},
		Emits: []driver.Emit{{Name: "video-input", Extract: driver.Extract{Regex: "(.*)"}}}}
	for _, tr := range []string{"http", "https", "snmp", "icmp"} {
		s := &driver.Spec{Version: 1, Transport: tr, Listeners: []driver.Listener{listener}}
		if err := s.Validate(nil); err == nil || !strings.Contains(err.Error(), "listen") {
			t.Errorf("a listener over %s = %v, want a refusal naming the listen path", tr, err)
		}
	}
	for _, tr := range []string{"tcp", "tls", "telnet"} {
		s := &driver.Spec{Version: 1, Transport: tr, Listeners: []driver.Listener{listener}}
		if err := s.Validate(nil); err != nil {
			t.Errorf("a listener over %s = %v, want clean", tr, err)
		}
	}
}

func TestHTTPLadder(t *testing.T) {
	l := driver.ResolveLadder("https", nil, nil)
	if l.Transport != driver.RungHandshake || l.Session {
		t.Fatalf("https ladder = %+v, want the handshake and no session rung", l)
	}
	l = driver.ResolveLadder("http", nil, &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic})
	if l.Transport != driver.RungConnect || !l.Session {
		t.Fatalf("credentialed http ladder = %+v, want a connect and a session rung", l)
	}
	s := httpSpec()
	s.Transport = "https"
	s.Ladder = &driver.Ladder{Transport: &driver.TransportRung{Rung: driver.RungConnect}}
	if err := s.Validate(httpCatalog()); err != nil {
		t.Fatalf("an https ladder declaring a connect = %v, want clean", err)
	}
}

func TestRenderHTTPRequest(t *testing.T) {
	req := driver.Request{
		Method:  "POST",
		Path:    "/api/inputs/${arg.slot}/name",
		Query:   map[string]string{"who": "${arg.who}"},
		Headers: map[string]string{"X-Note": "set ${value}"},
		Body:    json.RawMessage(`{"name":"${value}","level":"${arg.level}","note":"said ${value}","n":[1,"${arg.level}"]}`),
	}
	got, err := driver.RenderRequest(req, `a "quoted"`+"\nline", map[string]any{"slot": "1/../2?x", "who": "a&b=c", "level": json.Number("7")})
	if err == nil {
		t.Fatal("a header carrying a newline rendered; want it refused")
	}
	req.Headers = map[string]string{"X-Note": "set ${arg.level}"}
	got, err = driver.RenderRequest(req, `a "quoted"`+"\nline", map[string]any{"slot": "1/../2?x", "who": "a&b=c", "level": json.Number("7")})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got.Path != "/api/inputs/1%2F..%2F2%3Fx/name" {
		t.Errorf("path = %q, want the argument escaped as one segment", got.Path)
	}
	if got.Query["who"] != "a&b=c" || got.Headers["X-Note"] != "set 7" {
		t.Errorf("query %v headers %v", got.Query, got.Headers)
	}
	var body map[string]any
	dec := json.NewDecoder(strings.NewReader(string(got.Body)))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("the rendered body %q is not JSON: %v", got.Body, err)
	}
	if body["name"] != `a "quoted"`+"\nline" || body["note"] != `said a "quoted"`+"\nline" {
		t.Errorf("body strings = %v", body)
	}
	// A leaf that is exactly one reference takes the value's own type.
	if body["level"] != json.Number("7") {
		t.Errorf("level = %#v, want the number 7", body["level"])
	}
	if arr, _ := body["n"].([]any); len(arr) != 2 || arr[1] != json.Number("7") {
		t.Errorf("n = %#v", body["n"])
	}
}

func TestRenderTypedIntendedValue(t *testing.T) {
	req := driver.Request{Method: "POST", Path: "/v", Body: json.RawMessage(`{"volume":"${value}","mute":"${value}"}`)}
	got, err := driver.RenderRequestTyped(req, "42", json.RawMessage(`42`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != `{"mute":42,"volume":42}` {
		t.Errorf("body = %s, want the number", got.Body)
	}
	got, err = driver.RenderRequestTyped(req, "true", json.RawMessage(`"true"`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != `{"mute":"true","volume":"true"}` {
		t.Errorf("body = %s, want the property's text", got.Body)
	}
}

func TestRenderFormAndXMLBodies(t *testing.T) {
	form := driver.Request{Method: "POST", Path: "/f", BodyFormat: driver.BodyFormatForm, Body: json.RawMessage(`{"input":"${value}","fixed":"x y"}`)}
	got, err := driver.RenderRequest(form, "a&b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != "fixed=x+y&input=a%26b" {
		t.Errorf("form body = %q", got.Body)
	}
	xml := driver.Request{Method: "POST", Path: "/x", BodyFormat: driver.BodyFormatXML, Body: json.RawMessage(`"<set><input>${value}</input></set>"`)}
	got, err = driver.RenderRequest(xml, `</input><evil/>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != "<set><input>&lt;/input&gt;&lt;evil/&gt;</input></set>" {
		t.Errorf("xml body = %q, want the value escaped as text", got.Body)
	}
}

func TestInterpretXPathAndHTML(t *testing.T) {
	xmlBody := `<?xml version="1.0"?><info xmlns="urn:kestrel:info"><firmware> 2.4.1 </firmware><fan rpm="1200"/><fan rpm="1300"/></info>`
	fn := &driver.BakedFunction{Driver: "d", Function: "f", Request: &driver.Request{Format: driver.FormatXML, Namespaces: map[string]string{"k": "urn:kestrel:info"}}, Emits: []driver.BakedEmit{
		{Name: "firmware-version", Lane: "property", Extract: driver.Extract{XPath: "/k:info/k:firmware"}},
		{Name: "fan-speed", Lane: "metric", Extract: driver.Extract{XPath: "/k:info/k:fan[2]/@rpm"}},
		{Name: "fan-count", Lane: "metric", Extract: driver.Extract{XPath: "count(//k:fan)"}},
		{Name: "missing", Lane: "property", Extract: driver.Extract{XPath: "//k:nothing"}},
		{Name: "many", Lane: "metric", Extract: driver.Extract{XPath: "//k:fan/@rpm"}},
	}}
	out, faults := fn.Interpret(driver.HTTPPayload([]byte(xmlBody), fn.Request), time.Now())
	got := map[string]driver.Emitted{}
	for _, e := range out {
		got[e.Name] = e
	}
	if got["firmware-version"].Text != "2.4.1" {
		t.Errorf("firmware = %q, want the trimmed text", got["firmware-version"].Text)
	}
	if got["fan-speed"].Value != 1300 || got["fan-count"].Value != 2 {
		t.Errorf("fan-speed %v fan-count %v", got["fan-speed"].Value, got["fan-count"].Value)
	}
	if len(faults) != 2 || !strings.Contains(faults[0].Error()+faults[1].Error(), "selects 2") {
		t.Fatalf("faults = %v, want the missing and the many named", faults)
	}

	html := `<html><body><table><tr><td>Input</td><td id="input">HDMI 2</td></tr></table><p class="v">v<b>9</b></p></body></html>`
	page := &driver.BakedFunction{Driver: "d", Function: "p", Request: &driver.Request{Format: driver.FormatHTML}, Emits: []driver.BakedEmit{
		{Name: "video-input", Lane: "property", Extract: driver.Extract{XPath: `//td[@id="input"]`}},
		{Name: "rev", Lane: "property", Extract: driver.Extract{XPath: `//p[@class="v"]`}},
		{Name: "word", Lane: "property", Extract: driver.Extract{Regex: `id="input">([^<]+)`}},
	}}
	out, faults = page.Interpret(driver.HTTPPayload([]byte(html), page.Request), time.Now())
	if len(faults) != 0 || len(out) != 3 {
		t.Fatalf("html out %v faults %v", out, faults)
	}
	if out[0].Text != "HDMI 2" || out[1].Text != "v9" || out[2].Text != "HDMI 2" {
		t.Errorf("html values = %q %q %q", out[0].Text, out[1].Text, out[2].Text)
	}
	// A body that is not the declared format faults each xpath emit by name.
	_, faults = fn.Interpret(driver.HTTPPayload([]byte("not xml <"), fn.Request), time.Now())
	if len(faults) != len(fn.Emits) {
		t.Fatalf("faults %v, want one per emit", faults)
	}
}

func TestJudgeAnHTTPReply(t *testing.T) {
	b := httpSpec().Commands[0]
	v := b.JudgeReply(`{"ok":true,"volume":40}`)
	if !v.Judged || !v.Succeeded || v.Value != "40" {
		t.Fatalf("verdict = %+v, want the volume read off the JSON reply", v)
	}
}

func TestTerminalForResponse(t *testing.T) {
	ss := &driver.SessionSpec{Terminal: []driver.TerminalRule{
		{Class: driver.ClassAuth, Status: 403},
		{Class: driver.ClassAuth, Match: driver.Match{Regex: "account locked"}},
		{Class: driver.ClassProtocol, Status: 400, Match: driver.Match{Prefix: "bad api version"}},
	}}
	cases := []struct {
		status int
		body   string
		class  string
		ok     bool
	}{
		{403, "", driver.ClassAuth, true},
		{200, "your account locked", driver.ClassAuth, true},
		{400, "bad api version 2", driver.ClassProtocol, true},
		{400, "bad request", "", false},
		{404, "", "", false},
	}
	for _, tc := range cases {
		class, ok := ss.TerminalForResponse(tc.status, tc.body)
		if ok != tc.ok || class != tc.class {
			t.Errorf("TerminalForResponse(%d, %q) = %q %v, want %q %v", tc.status, tc.body, class, ok, tc.class, tc.ok)
		}
	}
}

func TestResolveHTTPCredential(t *testing.T) {
	ss := &driver.SessionSpec{Credential: "key", Scheme: driver.SchemeAPIKey, APIKey: &driver.APIKeyPlacement{In: "query", Name: "apikey"}}
	c, err := driver.ResolveHTTPCredential(ss, map[string]map[string]string{"key": {"key": "s3cret"}})
	if err != nil || c.Scheme != driver.SchemeAPIKey || c.Key != "s3cret" || c.In != "query" || c.Name != "apikey" {
		t.Fatalf("credential = %+v, %v", c, err)
	}
	_, err = driver.ResolveHTTPCredential(ss, map[string]map[string]string{"key": {}})
	if err == nil || !strings.Contains(err.Error(), "key.key") || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("a missing field = %v, want the reference named and no value", err)
	}
	none, err := driver.ResolveHTTPCredential(nil, nil)
	if err != nil || none != nil {
		t.Fatalf("no session = %+v, %v, want no credential", none, err)
	}
	basic, err := driver.ResolveHTTPCredential(&driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic}, map[string]map[string]string{"login": {"username": "admin", "password": "p"}})
	if err != nil || basic.Username != "admin" || basic.Password != "p" {
		t.Fatalf("basic = %+v, %v", basic, err)
	}
	bearer, err := driver.ResolveHTTPCredential(&driver.SessionSpec{Credential: "tok", Scheme: driver.SchemeBearer}, map[string]map[string]string{"tok": {"token": "t"}})
	if err != nil || bearer.Token != "t" {
		t.Fatalf("bearer = %+v, %v", bearer, err)
	}
}
