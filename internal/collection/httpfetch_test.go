package collection_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The status table the architect ruled (decision 5 of #1113): which answers
// gate, which fault the request alone and wait its interval, which back off.
func TestJudgeHTTPAnswerFollowsTheStatusTable(t *testing.T) {
	cred := &driver.HTTPCredential{Scheme: driver.SchemeBasic, Username: "admin", Password: "s3cret"}
	x := collection.HTTPExchange{Credential: cred}
	cases := []struct {
		name         string
		ans          collection.HTTPAnswer
		terminal     string // the terminal class, when it gates
		requestFault bool
		retryable    bool
	}{
		{"a 200 is success", collection.HTTPAnswer{Status: 200, Credentialed: true}, "", false, false},
		{"a 204 is success", collection.HTTPAnswer{Status: 204, Credentialed: true}, "", false, false},
		{"a credentialed 401 refuses the credential", collection.HTTPAnswer{Status: 401, Credentialed: true}, driver.ClassAuth, false, false},
		{"an uncredentialed 401 faults the request", collection.HTTPAnswer{Status: 401}, "", true, false},
		{"a 403 faults the request and gates nothing", collection.HTTPAnswer{Status: 403, Credentialed: true}, "", true, false},
		{"a 400", collection.HTTPAnswer{Status: 400}, "", true, false},
		{"a 404", collection.HTTPAnswer{Status: 404}, "", true, false},
		{"a 405", collection.HTTPAnswer{Status: 405}, "", true, false},
		{"a 406", collection.HTTPAnswer{Status: 406}, "", true, false},
		{"a 410", collection.HTTPAnswer{Status: 410}, "", true, false},
		{"a 415", collection.HTTPAnswer{Status: 415}, "", true, false},
		{"a 422", collection.HTTPAnswer{Status: 422}, "", true, false},
		{"a redirect is not followed", collection.HTTPAnswer{Status: 302, Location: "/login"}, "", true, false},
		{"a 408 backs off", collection.HTTPAnswer{Status: 408}, "", false, true},
		{"a 429 backs off", collection.HTTPAnswer{Status: 429, RetryAfter: "7"}, "", false, true},
		{"a 500 backs off", collection.HTTPAnswer{Status: 500}, "", false, true},
		{"a 503 backs off", collection.HTTPAnswer{Status: 503}, "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := collection.JudgeHTTPAnswer(tc.ans, x)
			terr, isTerm := collection.AsTerminal(err)
			_, isFault := collection.AsRequestFault(err)
			var retry *collection.RetryableAnswer
			isRetry := errors.As(err, &retry)
			switch {
			case tc.terminal != "":
				if !isTerm || terr.Class != tc.terminal {
					t.Fatalf("judged %v, want a terminal %s", err, tc.terminal)
				}
			case tc.requestFault:
				if !isFault || isTerm {
					t.Fatalf("judged %v, want a request fault that gates nothing", err)
				}
			case tc.retryable:
				if !isRetry || isTerm || isFault {
					t.Fatalf("judged %v, want a retryable answer", err)
				}
			default:
				if err != nil {
					t.Fatalf("judged %v, want success", err)
				}
			}
		})
	}
	if wait, ok := collection.RetryAfterOf(collection.JudgeHTTPAnswer(collection.HTTPAnswer{Status: 429, RetryAfter: "7"}, x)); !ok || wait != 7*time.Second {
		t.Fatalf("Retry-After = %v %v, want 7s", wait, ok)
	}
}

// A driver may declare more terminal answers (a gear's 403 for a wrong
// credential, an "account locked" body), and its rule decides first.
func TestJudgeHTTPAnswerHonoursDeclaredTerminals(t *testing.T) {
	x := collection.HTTPExchange{Session: &driver.SessionSpec{Terminal: []driver.TerminalRule{
		{Class: driver.ClassAuth, Status: 403},
		{Class: driver.ClassAuth, Match: driver.Match{Regex: "locked"}},
	}}}
	for _, ans := range []collection.HTTPAnswer{{Status: 403}, {Status: 200, Body: []byte(`{"error":"account locked"}`)}} {
		terr, ok := collection.AsTerminal(collection.JudgeHTTPAnswer(ans, x))
		if !ok || terr.Class != driver.ClassAuth {
			t.Fatalf("answer %d %q judged %v, want the declared auth refusal", ans.Status, ans.Body, terr)
		}
	}
}

// A component that echoes the credential back cannot carry it into a
// reason: every form of it is masked.
func TestJudgeHTTPAnswerScrubsTheCredentialFromItsReason(t *testing.T) {
	cases := []struct {
		cred    *driver.HTTPCredential
		secrets []string
	}{
		{&driver.HTTPCredential{Scheme: driver.SchemeBasic, Username: "admin", Password: "s3cret-pw"},
			[]string{"s3cret-pw", base64.StdEncoding.EncodeToString([]byte("admin:s3cret-pw"))}},
		{&driver.HTTPCredential{Scheme: driver.SchemeBearer, Token: "tok-123456"}, []string{"tok-123456"}},
		{&driver.HTTPCredential{Scheme: driver.SchemeAPIKey, Key: "key with space&more", In: driver.APIKeyInQuery, Name: "apikey"},
			[]string{"key with space&more", "key+with+space%26more"}},
	}
	for _, tc := range cases {
		echo := "you sent " + strings.Join(tc.secrets, " | ")
		if tc.cred.Scheme == driver.SchemeBasic {
			echo += " | Authorization: Basic " + tc.secrets[1]
		}
		for _, status := range []int{400, 500, 302} {
			err := collection.JudgeHTTPAnswer(collection.HTTPAnswer{Status: status, Body: []byte(echo), Location: "/x?" + echo, Credentialed: true}, collection.HTTPExchange{Credential: tc.cred})
			for _, secret := range tc.secrets {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("status %d reason %q carries the credential %q", status, err, secret)
				}
			}
		}
	}
}

// The session rung a credentialed answer proves (decision 7 of #1113): a 2xx
// says yes, a refusal says no, and a redirect, a 403, a 404 or a 5xx never
// judged the credential, so says nothing.
func TestAuthenticatedReadsTheCredentialedAnswer(t *testing.T) {
	cases := []struct {
		ans     collection.HTTPAnswer
		verdict string
		ok      bool
	}{
		{collection.HTTPAnswer{Status: 200, Credentialed: true}, "yes", true},
		{collection.HTTPAnswer{Status: 404, Credentialed: true}, "", false},
		{collection.HTTPAnswer{Status: 301, Credentialed: true}, "", false},
		{collection.HTTPAnswer{Status: 401, Credentialed: true}, "no", true},
		{collection.HTTPAnswer{Status: 403, Credentialed: true}, "", false},
		{collection.HTTPAnswer{Status: 503, Credentialed: true}, "", false},
		{collection.HTTPAnswer{Status: 200}, "", false},
	}
	for _, tc := range cases {
		v, ok := collection.Authenticated(tc.ans, collection.JudgeHTTPAnswer(tc.ans, collection.HTTPExchange{}))
		if v != tc.verdict || ok != tc.ok {
			t.Errorf("answer %d (credentialed %v) = %q %v, want %q %v", tc.ans.Status, tc.ans.Credentialed, v, ok, tc.verdict, tc.ok)
		}
	}
}

func TestBuildHTTPRequestAppliesTheSchemeAndTheShape(t *testing.T) {
	ctx := context.Background()
	req := driver.Request{Method: "POST", Path: "/api/a%2Fb", Query: map[string]string{"x": "1 2"}, Headers: map[string]string{"X-Note": "n"},
		Body: json.RawMessage(`{"v":1}`)}
	cases := []struct {
		cred  *driver.HTTPCredential
		check func(t *testing.T, header map[string][]string, rawQuery string)
	}{
		{&driver.HTTPCredential{Scheme: driver.SchemeBasic, Username: "u", Password: "p"}, func(t *testing.T, h map[string][]string, _ string) {
			if got := h["Authorization"]; len(got) != 1 || got[0] != "Basic "+base64.StdEncoding.EncodeToString([]byte("u:p")) {
				t.Errorf("authorization = %v", got)
			}
		}},
		{&driver.HTTPCredential{Scheme: driver.SchemeBearer, Token: "t"}, func(t *testing.T, h map[string][]string, _ string) {
			if got := h["Authorization"]; len(got) != 1 || got[0] != "Bearer t" {
				t.Errorf("authorization = %v", got)
			}
		}},
		{&driver.HTTPCredential{Scheme: driver.SchemeAPIKey, Key: "k", In: driver.APIKeyInHeader, Name: "X-Census-Key"}, func(t *testing.T, h map[string][]string, _ string) {
			if got := h["X-Census-Key"]; len(got) != 1 || got[0] != "k" {
				t.Errorf("key header = %v", got)
			}
		}},
		{&driver.HTTPCredential{Scheme: driver.SchemeAPIKey, Key: "k&k", In: driver.APIKeyInQuery, Name: "apikey"}, func(t *testing.T, h map[string][]string, q string) {
			if q != "apikey=k%26k&x=1+2" {
				t.Errorf("query = %q", q)
			}
			if _, ok := h["Authorization"]; ok {
				t.Error("a query key also rode a header")
			}
		}},
	}
	for _, tc := range cases {
		r, err := collection.BuildHTTPRequest(ctx, collection.HTTPExchange{Target: "10.0.0.5:8080", Request: req, Credential: tc.cred})
		if err != nil {
			t.Fatal(err)
		}
		if r.Method != "POST" || r.URL.EscapedPath() != "/api/a%2Fb" || r.URL.Host != "10.0.0.5:8080" || r.URL.Scheme != "http" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-Note") != "n" {
			t.Errorf("headers %v", r.Header)
		}
		tc.check(t, r.Header, r.URL.RawQuery)
		if strings.Contains(collection.RedactedURL(r.URL, tc.cred), "k%26k") {
			t.Errorf("the redacted url %q carries the query key", collection.RedactedURL(r.URL, tc.cred))
		}
	}
	r, err := collection.BuildHTTPRequest(ctx, collection.HTTPExchange{Target: "h:443", TLS: &collection.TLSTrust{}, Request: driver.Request{Path: "/"}})
	if err != nil || r.URL.Scheme != "https" {
		t.Fatalf("an https exchange builds %v, %v", r.URL, err)
	}
}

// The scrub runs on the whole body before it is collapsed and cut, so an
// echo straddling the cut, a credential with runs of spaces, and a JSON or
// html escaped echo are all masked; and a reason is always valid UTF-8 (a
// protobuf string), however the body was encoded or where the cut fell.
func TestJudgeHTTPAnswerScrubsBeforeItCutsAndKeepsUTF8(t *testing.T) {
	token := "tok_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/+="
	cred := &driver.HTTPCredential{Scheme: driver.SchemeBearer, Token: token}
	straddle := strings.Repeat("x", 190) + " " + token + " tail"
	pw := "correct  horse\u00a0battery & <more>/"
	pass := &driver.HTTPCredential{Scheme: driver.SchemeBasic, Username: "u", Password: pw}
	jsonForm, _ := json.Marshal(pw)
	jsonEcho := strings.ReplaceAll(string(jsonForm), "/", `\/`) // PHP's json_encode escapes the slash
	collapsed := strings.Join(strings.Fields(pw), " ")
	cases := []struct {
		cred    *driver.HTTPCredential
		body    string
		secrets []string
	}{
		{cred, straddle, []string{"tok_ABCDE", token}},
		{cred, `{"echo":"tok_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789\/+="}`, []string{"tok_ABCDEFGHIJ"}},
		{pass, "you sent " + pw + " and " + string(jsonForm) + " and " + jsonEcho + " and " + html.EscapeString(pw),
			[]string{collapsed, "correct  horse", "\\u0026 \\u003cmore", "&amp; &lt;more"}},
	}
	for _, tc := range cases {
		err := collection.JudgeHTTPAnswer(collection.HTTPAnswer{Status: 500, Body: []byte(tc.body), Credentialed: true}, collection.HTTPExchange{Credential: tc.cred})
		for _, sec := range tc.secrets {
			if strings.Contains(err.Error(), sec) {
				t.Errorf("reason %q carries %q", err, sec)
			}
		}
	}
	// A multibyte character straddling the cut, and a Latin-1 body.
	for _, body := range [][]byte{[]byte(strings.Repeat("a", 199) + "\u00e9\u00e9\u00e9"), {0xe9, 0x20, 0xff, 0xfe}} {
		err := collection.JudgeHTTPAnswer(collection.HTTPAnswer{Status: 400, Body: body, Location: string([]byte{0xff})}, collection.HTTPExchange{})
		if !utf8.ValidString(err.Error()) {
			t.Fatalf("reason %q is not valid UTF-8", err)
		}
	}
	err := collection.JudgeHTTPAnswer(collection.HTTPAnswer{Status: 302, Location: "/x\xff"}, collection.HTTPExchange{})
	if !utf8.ValidString(err.Error()) {
		t.Fatalf("a redirect's reason %q is not valid UTF-8", err)
	}
}

// A poll's form body leaves encoded in its format, with its media type.
func TestBuildHTTPRequestEncodesTheBodyFormat(t *testing.T) {
	for _, tc := range []struct {
		req       driver.Request
		body, typ string
	}{
		{driver.Request{Method: "POST", Path: "/f", BodyFormat: driver.BodyFormatForm, Body: json.RawMessage(`{"cmd":"status"}`)}, "cmd=status", "application/x-www-form-urlencoded"},
		{driver.Request{Method: "POST", Path: "/x", BodyFormat: driver.BodyFormatXML, Body: json.RawMessage(`"<GetStatus/>"`)}, "<GetStatus/>", "application/xml"},
	} {
		r, err := collection.BuildHTTPRequest(context.Background(), collection.HTTPExchange{Target: "h:80", Request: tc.req})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != tc.body || r.Header.Get("Content-Type") != tc.typ {
			t.Errorf("sent %q as %q, want %q as %q", b, r.Header.Get("Content-Type"), tc.body, tc.typ)
		}
	}
}
