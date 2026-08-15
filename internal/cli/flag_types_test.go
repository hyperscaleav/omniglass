package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The generated flag's type is the schema's (#711).
//
// Every body flag used to be a string, and every non-string body field was
// coerced at run time by jsonOrString. The schema said integer, the CLI said
// string, and an operator who typed a word found out from the server's 422 after
// a round trip that had already authenticated. These drive the REAL generated
// tree (the same commands an operator runs) against a canned server, so what is
// asserted is what the shell refuses and what the wire carries, not what the
// generator's model says.

// runCLI executes the real root command with args, returning the combined output
// and the error Execute would have surfaced.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := Root("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// captureBody stands up a server that records the one request body it is sent.
func captureBody(t *testing.T) (url string, got func() map[string]any) {
	t.Helper()
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() map[string]any {
		m := map[string]any{}
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("the server received %q, which is not a JSON object: %v", body, err)
		}
		return m
	}
}

// TestAnIntegerBodyFlagIsRefusedAtTheShell is the acceptance for #711: a field
// the schema types as an integer refuses a non-number at parse time, before any
// request is issued, rather than at the server's 422.
//
// settle_window_seconds is the worked case (#718 gave it a floor of 0 the flag
// could not carry). The canned server is a tripwire: it fails the test if the CLI
// issued a request at all, which is the difference between refusing at the shell
// and refusing after a round trip.
func TestAnIntegerBodyFlagIsRefusedAtTheShell(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	out, err := runCLI(t, "command-type", "create", "--name", "set-input",
		"--settle-window-seconds", "soon", "--server", srv.URL)
	if err == nil {
		t.Fatalf("a word for an integer flag was accepted; output was %q", out)
	}
	if !strings.Contains(err.Error(), "settle-window-seconds") {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
	if reached {
		t.Error("the CLI issued the request anyway; the point of a typed flag is that it never leaves the shell")
	}
}

// TestATypedBodyFlagSendsItsOwnType pins the other half: the value on the wire is
// a JSON number, not the quoted string a string flag would have sent had
// jsonOrString not rescued it. The flag's declared type is asserted too, since
// that is what the generated CLI reference publishes and what --help shows.
func TestATypedBodyFlagSendsItsOwnType(t *testing.T) {
	url, got := captureBody(t)
	if _, err := runCLI(t, "command-type", "create", "--name", "set-input",
		"--settle-window-seconds", "15", "--server", url); err != nil {
		t.Fatalf("create: %v", err)
	}
	body := got()
	v, ok := body["settle_window_seconds"].(float64)
	if !ok {
		t.Fatalf("settle_window_seconds arrived as %T (%v), want a JSON number", body["settle_window_seconds"], body["settle_window_seconds"])
	}
	if v != 15 {
		t.Errorf("settle_window_seconds = %v, want 15", v)
	}

	cmd, _, err := Root("test").Find([]string{"command-type", "create"})
	if err != nil {
		t.Fatalf("find command-type create: %v", err)
	}
	if f := cmd.Flags().Lookup("settle-window-seconds"); f == nil || f.Value.Type() != "int" {
		t.Errorf("--settle-window-seconds is %v, want an int flag", f)
	}
}

// TestABooleanBodyFlagIsABoolFlag covers the other scalar the generator used to
// flatten. `--propagates=false` reaches the wire as false rather than "false",
// and the flag is a bool, so `--propagates` alone means true.
func TestABooleanBodyFlagIsABoolFlag(t *testing.T) {
	url, got := captureBody(t)
	if _, err := runCLI(t, "tag", "create", "--name", "rack-position",
		"--propagates=false", "--server", url); err != nil {
		t.Fatalf("create: %v", err)
	}
	if v, ok := got()["propagates"].(bool); !ok || v {
		t.Errorf("propagates arrived as %#v, want the JSON false", got()["propagates"])
	}
}

// TestAnObjectBodyFlagStaysOneJSONString is the decision half of #711, asserted
// rather than assumed. An object-valued field (name_rule, a $ref) keeps a single
// string flag parsed as JSON: a nested value has no shell-native flag type, and
// `null` has to stay sendable because it is what clears the field under an update
// mask (ADR-0106). Both spellings are pinned here, since a later "improvement" to
// per-leaf flags would silently take the second one away.
func TestAnObjectBodyFlagStaysOneJSONString(t *testing.T) {
	url, got := captureBody(t)
	if _, err := runCLI(t, "location-type", "create", "--name", "wing",
		"--label", "Wing", "--name-rule", `{"stem":"wing","bare_first":true}`,
		"--server", url); err != nil {
		t.Fatalf("create: %v", err)
	}
	rule, ok := got()["name_rule"].(map[string]any)
	if !ok {
		t.Fatalf("name_rule arrived as %T (%v), want a JSON object", got()["name_rule"], got()["name_rule"])
	}
	if rule["stem"] != "wing" {
		t.Errorf("name_rule = %v, want stem wing", rule)
	}

	clearURL, cleared := captureBody(t)
	if _, err := runCLI(t, "location-type", "update", "wing", "--update-mask", "name_rule",
		"--name-rule", "null", "--server", clearURL); err != nil {
		t.Fatalf("update: %v", err)
	}
	body := cleared()
	if v, present := body["name_rule"]; !present || v != nil {
		t.Errorf("name_rule = %#v (present=%v), want an explicit null: that is how a mask clears it", v, present)
	}
}
