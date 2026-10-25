package driver_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// http logins (#1114, slice 2 of #1113): the login dialog #1061 shipped,
// its steps requests rather than prompts, and HTTP digest on basic-auth.

// formLoginSpec is an https driver behind a form login: one POST naming the
// login secret's fields, a redirect to its login page meaning the session
// lapsed.
func formLoginSpec() *driver.Spec {
	s := httpSpec()
	s.Transport = "https"
	s.Session = &driver.SessionSpec{Login: &driver.Login{
		Path: "/login",
		Steps: []driver.LoginStep{{Request: &driver.Request{Method: "POST", Path: "/login", BodyFormat: driver.BodyFormatForm,
			Body: json.RawMessage(`{"username":"${secret.login.username}","password":"${secret.login.password}"}`)}}},
	}}
	return s
}

// tokenLoginSpec logs in with JSON and captures a token and its lifetime,
// which ride every later request as a bearer authorization: the shape an
// OAuth2 client-credentials grant takes too (#1119).
func tokenLoginSpec() *driver.Spec {
	s := httpSpec()
	s.Session = &driver.SessionSpec{Login: &driver.Login{
		Steps: []driver.LoginStep{{Request: &driver.Request{Method: "POST", Path: "/api/login",
			Body: json.RawMessage(`{"username":"${secret.login.username}","password":"${secret.login.password}"}`)}}},
		Capture: []driver.Capture{{Name: "token", Extract: driver.Extract{JSONPath: "token"}}},
		Expiry:  &driver.Extract{JSONPath: "expires_in"},
		Headers: map[string]string{"Authorization": "Bearer ${session.token}"},
		Expired: &driver.Match{Regex: "token expired"},
	}}
	return s
}

func TestHTTPLoginsValidate(t *testing.T) {
	for name, s := range map[string]*driver.Spec{"form": formLoginSpec(), "token": tokenLoginSpec()} {
		if err := s.Validate(httpCatalog()); err != nil {
			t.Errorf("%s login = %v, want clean", name, err)
		}
	}
	digest := httpSpec()
	digest.Session = &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeDigest}
	if err := digest.Validate(httpCatalog()); err != nil {
		t.Fatalf("digest on basic-auth = %v, want clean", err)
	}
	l := driver.ResolveLadder("http", nil, tokenLoginSpec().Session)
	if !l.Session {
		t.Fatal("a login declares the session rung")
	}
}

func TestHTTPLoginRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*driver.Spec)
		want   string
	}{
		{"a login beside a scheme", func(s *driver.Spec) { s.Session.Scheme, s.Session.Credential = driver.SchemeBasic, "login" }, "scheme or a login"},
		{"a login with no steps", func(s *driver.Spec) { s.Session.Login.Steps = nil }, "step"},
		{"a step with no request", func(s *driver.Spec) { s.Session.Login.Steps[0].Request = nil }, "request"},
		{"a step that is a prompt", func(s *driver.Spec) {
			s.Session.Login.Steps[0].Expect, s.Session.Login.Steps[0].Send = driver.Match{Prefix: "login:"}, "x"
		}, "prompt"},
		{"a step naming an undeclared secret", func(s *driver.Spec) {
			s.Session.Login.Steps[0].Request.Body = json.RawMessage(`{"u":"${secret.other.username}"}`)
		}, "other"},
		{"a step naming a command value", func(s *driver.Spec) {
			s.Session.Login.Steps[0].Request.Body = json.RawMessage(`{"u":"${value}"}`)
		}, "value"},
		{"a step with no path", func(s *driver.Spec) { s.Session.Login.Steps[0].Request.Path = "" }, "path"},
		{"a capture with no name", func(s *driver.Spec) { s.Session.Login.Capture[0].Name = "" }, "name"},
		{"two captures with one name", func(s *driver.Spec) {
			s.Session.Login.Capture = append(s.Session.Login.Capture, s.Session.Login.Capture[0])
		}, "token"},
		{"a capture by xpath on a json answer", func(s *driver.Spec) {
			s.Session.Login.Capture[0].Extract = driver.Extract{XPath: "//t"}
		}, "format"},
		{"a header naming an undeclared capture", func(s *driver.Spec) {
			s.Session.Login.Headers["Authorization"] = "Bearer ${session.nope}"
		}, "nope"},
		{"a header naming a secret", func(s *driver.Spec) {
			s.Session.Login.Headers["Authorization"] = "Basic ${secret.login.password}"
		}, "secret"},
		{"a cookie header", func(s *driver.Spec) { s.Session.Login.Headers["Cookie"] = "${session.token}" }, "Cookie"},
		{"a relative login path", func(s *driver.Spec) { s.Session.Login.Path = "login" }, "/"},
		{"an uncompilable expired match", func(s *driver.Spec) { s.Session.Login.Expired = &driver.Match{Regex: "("} }, "expired"},
		{"an expiry by xpath on a json answer", func(s *driver.Spec) { s.Session.Login.Expiry = &driver.Extract{XPath: "//e"} }, "format"},
		{"a poll header shadowing a login header", func(s *driver.Spec) { s.Polls[0].Request.Headers["authorization"] = "x" }, "authorization"},
		{"a re-probe beside the login", func(s *driver.Spec) { s.Session.Reprobe = "10m" }, "reprobe"},
		// The credential reaches a node only with a task.
		{"a login with commands and no poll", func(s *driver.Spec) { s.Polls = nil }, "poll"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tokenLoginSpec()
			tc.mutate(s)
			err := s.Validate(httpCatalog())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
	// Digest reads a basic-auth secret; any other shape refuses.
	s := httpSpec()
	s.Inputs[2].SecretType = "bearer-token"
	s.Session = &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeDigest}
	if err := s.Validate(httpCatalog()); err == nil || !strings.Contains(err.Error(), "basic-auth") {
		t.Fatalf("digest on a bearer token = %v, want a refusal naming basic-auth", err)
	}
	// A line login still declares prompts: an http step's request means
	// nothing there.
	line := sessionSpec()
	line.Session.Login.Steps[0].Request = &driver.Request{Path: "/login"}
	if err := line.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "request") {
		t.Fatalf("a line login step with a request = %v, want a refusal", err)
	}
}

func TestRenderLoginStepNamesSecretsNeverValues(t *testing.T) {
	step := formLoginSpec().Session.Login.Steps[0].Request
	got, used, err := driver.RenderLoginRequest(*step, map[string]map[string]string{"login": {"username": "admin", "password": `p"a&ss`}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := got.BodyBytes()
	if err != nil || string(body) != "password=p%22a%26ss&username=admin" {
		t.Fatalf("login body = %q, %v", body, err)
	}
	if len(used) != 2 {
		t.Fatalf("the values to scrub = %d, want the username and the password", len(used))
	}
	_, _, err = driver.RenderLoginRequest(*step, map[string]map[string]string{"login": {"username": "admin"}})
	if err == nil || !strings.Contains(err.Error(), "login.password") {
		t.Fatalf("a missing field = %v, want the reference named", err)
	}
}

func TestLoginSessionHeadersAndExpiry(t *testing.T) {
	l := tokenLoginSpec().Session.Login
	h, err := l.SessionHeaders(map[string]string{"token": "tok-1"})
	if err != nil || h["Authorization"] != "Bearer tok-1" {
		t.Fatalf("headers = %v, %v", h, err)
	}
	if _, err := l.SessionHeaders(map[string]string{"token": "a\r\nb"}); err == nil {
		t.Fatal("a captured value with CR LF rode a header")
	}
	if d, err := driver.ParseExpiry("300"); err != nil || d != 300*time.Second {
		t.Fatalf("expiry = %v, %v", d, err)
	}
	if _, err := driver.ParseExpiry("soon"); err == nil {
		t.Fatal("an expiry that is not seconds parsed")
	}
}
