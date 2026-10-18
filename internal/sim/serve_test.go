package sim

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestServeHTTP_MuteChannelPropagates exercises the whole loop the
// Polaris DSP bug surfaced: a POST with a JSON body hits a route that
// carries a mutate clause, the mutation updates per-channel state, and
// a subsequent GET reflects the new mute value. If any link in the
// chain (body parse, path templating, JSON coercion, array index write,
// state-driven render) regresses, this test fails.
func TestServeHTTP_MuteChannelPropagates(t *testing.T) {
	initial := map[string]any{
		"status": map[string]any{
			"channels": []any{
				map[string]any{"index": 0, "name": "A", "mute": false},
				map[string]any{"index": 1, "name": "B", "mute": false},
				map[string]any{"index": 2, "name": "C", "mute": false},
			},
		},
	}

	dev := NewDevice("dev-1", "polaris-test", net.ParseIP("127.0.0.1"), "polaris-dsp-16", 1, initial)

	if err := dev.AddRoute(
		"GET /api/channels",
		"channels.json.gotempl",
		`{{ .State | get "status" | get "channels" | marshal }}`,
		nil,
	); err != nil {
		t.Fatal(err)
	}

	if err := dev.AddRoute(
		"POST /api/channels/mute",
		"channel-ack.json.gotempl",
		`{"ok":true}`,
		map[string]any{
			"status.channels.{{ .Request.Body.index }}.mute": "{{ .Request.Body.mute }}",
		},
	); err != nil {
		t.Fatal(err)
	}

	// Mute channel 0 (the exact scenario the user reported).
	postReq := httptest.NewRequest("POST", "/api/channels/mute",
		bytes.NewReader([]byte(`{"index": 0, "mute": true}`)))
	postReq.Header.Set("Content-Type", "application/json")
	postW := httptest.NewRecorder()
	dev.ServeHTTP(postW, postReq)

	if postW.Code != 200 {
		t.Fatalf("POST status = %d, body = %s", postW.Code, postW.Body.String())
	}

	// GET should now show channel 0 muted.
	getReq := httptest.NewRequest("GET", "/api/channels", nil)
	getW := httptest.NewRecorder()
	dev.ServeHTTP(getW, getReq)

	if getW.Code != 200 {
		t.Fatalf("GET status = %d", getW.Code)
	}

	var channels []map[string]any
	if err := json.Unmarshal(getW.Body.Bytes(), &channels); err != nil {
		t.Fatalf("parsing GET body: %v (body=%s)", err, getW.Body.String())
	}
	if len(channels) != 3 {
		t.Fatalf("got %d channels, want 3", len(channels))
	}
	if channels[0]["mute"] != true {
		t.Errorf("channel[0].mute = %v, want true", channels[0]["mute"])
	}
	if channels[1]["mute"] != false {
		t.Errorf("channel[1].mute = %v, want false", channels[1]["mute"])
	}

	// State() must also reflect the change, that is what the console's
	// "state blob" ultimately renders. Snapshotting into a deep-copy so
	// an eventual diff-pushback cannot claim the state is clean.
	state := dev.State()
	got := state["status"].(map[string]any)["channels"].([]any)[0].(map[string]any)["mute"]
	if got != true {
		t.Errorf("State()...channels[0].mute = %v, want true", got)
	}
}

// TestParseRequestContext_ParsesJSONBody verifies the body plumbing
// independently of mutations: a templated render can read .Request.Body
// fields directly even when no mutate clause is present.
func TestParseRequestContext_ParsesJSONBody(t *testing.T) {
	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 1, map[string]any{})
	if err := dev.AddRoute(
		"POST /echo",
		"echo.json.gotempl",
		`{"got":"{{ .Request.Body.msg }}"}`,
		nil,
	); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/echo", strings.NewReader(`{"msg": "hello"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), `"got":"hello"`) {
		t.Errorf("body = %s, want got:hello", w.Body.String())
	}
}

// TestServeHTTP_ConcurrentPublishIsVersionOrdered stresses the race
// that the first draft of this PR had: Publish called after the lock
// released meant two concurrent requests could hand events to the
// publisher in the wrong order, producing version gaps at the
// consumer. The fix moved Publish inside the lock. This test asserts
// the ordering invariant by firing N concurrent mutating POSTs and
// demanding the published Versions be strictly 1..N in order.
func TestServeHTTP_ConcurrentPublishIsVersionOrdered(t *testing.T) {
	const n = 64

	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 0,
		map[string]any{"counter": 0})
	if err := dev.AddRoute("POST /bump", "ack.json.gotempl", `{"ok":true}`,
		map[string]any{"counter": "{{ .Request.Body.v }}"}); err != nil {
		t.Fatal(err)
	}

	var (
		mu       sync.Mutex
		received []int64
	)
	dev.DiffPublisher = func(sc StateChange) {
		mu.Lock()
		received = append(received, sc.Version)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"v": %d}`, i)
			req := httptest.NewRequest("POST", "/bump", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			dev.ServeHTTP(httptest.NewRecorder(), req)
		}(i)
	}
	wg.Wait()

	if len(received) != n {
		t.Fatalf("got %d published events, want %d", len(received), n)
	}
	for i, v := range received {
		if v != int64(i+1) {
			t.Errorf("received[%d] = %d, want %d (versions must be strictly monotonic)", i, v, i+1)
		}
	}
}

// TestStartHTTP_TLS verifies that tls=true in a profile results in an
// actual HTTPS listener, closing the kestrel-vroom gap where the flag
// was parsed but ignored.
func TestStartHTTP_TLS(t *testing.T) {
	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 1,
		map[string]any{"msg": "ok"})
	if err := dev.AddRoute("GET /ping", "ping.json.gotempl",
		`{"msg":"{{ .State | get "msg" }}"}`, nil); err != nil {
		t.Fatal(err)
	}

	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("generating cert: %v", err)
	}

	// Pick an ephemeral port by asking StartHTTP to bind 0, then read
	// back the actual port from the listener.
	if err := dev.StartHTTP(0, cert); err != nil {
		t.Fatalf("StartHTTP: %v", err)
	}
	defer dev.StopHTTP()

	port := dev.HTTPAddrs()[0].(*net.TCPAddr).Port

	// Wait briefly for the goroutine to begin Serve; sub-second is
	// generous for an in-process listener.
	time.Sleep(50 * time.Millisecond)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("https://127.0.0.1:" + strconv.Itoa(port) + "/ping")
	if err != nil {
		t.Fatalf("GET over TLS: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"msg":"ok"`) {
		t.Errorf("body = %s", body)
	}
}

// TestServeHTTP_EnumValidationAccepts is the route-level happy path:
// a POST with a body that satisfies the route's enum mutates state and
// returns 200 with the success template.
func TestServeHTTP_EnumValidationAccepts(t *testing.T) {
	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 0,
		map[string]any{"status": map[string]any{"device": map[string]any{"power": "off"}}})
	route := &profile.Route{
		Match:    "POST /api/system/power",
		Template: "ack.json.gotempl",
		Mutations: []profile.Mutation{
			{
				Path:  "status.device.power",
				Value: "{{ .Request.Body.power }}",
				Enum:  []string{"on", "off", "standby"},
			},
		},
	}
	if err := dev.AddRouteFromProfile(route, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/system/power",
		strings.NewReader(`{"power": "standby"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := dev.State()["status"].(map[string]any)["device"].(map[string]any)["power"]
	if got != "standby" {
		t.Errorf("state power = %v, want standby", got)
	}
}

// TestServeHTTP_EnumValidationRejects is the route-level failure path:
// a POST whose body misses the enum returns 400 with a JSON body
// listing the failure, and state is left untouched.
func TestServeHTTP_EnumValidationRejects(t *testing.T) {
	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 0,
		map[string]any{"status": map[string]any{"device": map[string]any{"power": "off"}}})
	route := &profile.Route{
		Match:    "POST /api/system/power",
		Template: "ack.json.gotempl",
		Mutations: []profile.Mutation{
			{
				Path:  "status.device.power",
				Value: "{{ .Request.Body.power }}",
				Enum:  []string{"on", "off", "standby"},
			},
		},
	}
	if err := dev.AddRouteFromProfile(route, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}

	startVersion := dev.Version
	req := httptest.NewRequest("POST", "/api/system/power",
		strings.NewReader(`{"power": "yolo"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Error  string `json:"error"`
		Fields []struct {
			Path  string   `json:"path"`
			Value string   `json:"value"`
			Enum  []string `json:"enum"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body %q: %v", w.Body.String(), err)
	}
	if body.Error == "" {
		t.Errorf("response body missing 'error' field: %s", w.Body.String())
	}
	if len(body.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d: %s", len(body.Fields), w.Body.String())
	}
	if body.Fields[0].Path != "status.device.power" {
		t.Errorf("fields[0].path = %q, want status.device.power", body.Fields[0].Path)
	}
	if body.Fields[0].Value != "yolo" {
		t.Errorf("fields[0].value = %q, want yolo", body.Fields[0].Value)
	}
	if len(body.Fields[0].Enum) != 3 {
		t.Errorf("fields[0].enum = %v, want 3 entries", body.Fields[0].Enum)
	}

	got := dev.State()["status"].(map[string]any)["device"].(map[string]any)["power"]
	if got != "off" {
		t.Errorf("state power = %v, want off (state must not change on validation failure)", got)
	}
	if dev.Version != startVersion {
		t.Errorf("Version bumped from %d to %d on validation failure", startVersion, dev.Version)
	}
}

// TestServeHTTP_RequiredMutationFailureReturns400 covers the core
// fail-closed contract: a mutation whose path cannot resolve at runtime
// (here, an out-of-range array index) is required by default and must
// surface as 400 with a JSON body listing the failed path. Previously
// the handler logged the error and returned 200 with the success
// template, leaving the client with a green response over unchanged
// state.
func TestServeHTTP_RequiredMutationFailureReturns400(t *testing.T) {
	initial := map[string]any{
		"status": map[string]any{
			"channels": []any{
				map[string]any{"index": 0, "mute": false},
			},
		},
	}
	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 1, initial)
	route := &profile.Route{
		Match:    "POST /api/channels/mute",
		Template: "ack.json.gotempl",
		Mutations: []profile.Mutation{
			{
				Path:  "status.channels.{{ .Request.Body.index }}.mute",
				Value: "{{ .Request.Body.mute }}",
			},
		},
	}
	if err := dev.AddRouteFromProfile(route, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}

	startVersion := dev.Version
	req := httptest.NewRequest("POST", "/api/channels/mute",
		strings.NewReader(`{"index": 99, "mute": true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Error           string `json:"error"`
		FailedMutations []struct {
			Path  string `json:"path"`
			Error string `json:"error"`
		} `json:"failed_mutations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body %q: %v", w.Body.String(), err)
	}
	if body.Error == "" {
		t.Errorf("response body missing 'error' field: %s", w.Body.String())
	}
	if len(body.FailedMutations) != 1 {
		t.Fatalf("expected 1 failed mutation, got %d: %s", len(body.FailedMutations), w.Body.String())
	}
	if body.FailedMutations[0].Path != "status.channels.99.mute" {
		t.Errorf("failed_mutations[0].path = %q, want status.channels.99.mute", body.FailedMutations[0].Path)
	}
	if body.FailedMutations[0].Error == "" {
		t.Errorf("failed_mutations[0].error empty: %s", w.Body.String())
	}

	// Success template must not have been rendered: the response body
	// shape is the error envelope, not the route template.
	if strings.Contains(w.Body.String(), `"ok":true`) {
		t.Errorf("success template rendered on required failure: %s", w.Body.String())
	}
	if dev.Version != startVersion {
		t.Errorf("Version bumped from %d to %d on required failure", startVersion, dev.Version)
	}
}

// TestServeHTTP_OptionalMutationFailureReturns200 confirms the opt-out:
// a mutation marked optional in the profile that fails at runtime logs
// a warning but does not block the request. The response is the route's
// success template with status 200, mirroring the legacy best-effort
// behavior for profiles that intentionally touch a path that may not
// exist on every variant.
func TestServeHTTP_OptionalMutationFailureReturns200(t *testing.T) {
	initial := map[string]any{
		"status": map[string]any{"channels": []any{map[string]any{"mute": false}}},
	}
	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 1, initial)
	route := &profile.Route{
		Match:    "POST /api/channels/mute",
		Template: "ack.json.gotempl",
		Mutations: []profile.Mutation{
			{
				Path:     "status.channels.{{ .Request.Body.index }}.mute",
				Value:    "{{ .Request.Body.mute }}",
				Optional: true,
			},
		},
	}
	if err := dev.AddRouteFromProfile(route, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/channels/mute",
		strings.NewReader(`{"index": 99, "mute": true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for optional failure; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Errorf("body = %s, want success template", w.Body.String())
	}
}

// TestServeHTTP_MixedRequiredSuccessOptionalFailureReturns200 confirms
// a route with one required mutation that succeeds and one optional
// mutation that fails still renders the success template. The required
// write applies, the optional failure logs, and the client sees 200.
func TestServeHTTP_MixedRequiredSuccessOptionalFailureReturns200(t *testing.T) {
	initial := map[string]any{
		"status": map[string]any{
			"device":   map[string]any{"power": "off"},
			"channels": []any{map[string]any{"mute": false}},
		},
	}
	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 1, initial)
	route := &profile.Route{
		Match:    "POST /api/system/configure",
		Template: "ack.json.gotempl",
		Mutations: []profile.Mutation{
			{
				Path:  "status.device.power",
				Value: "on",
			},
			{
				Path:     "status.channels.{{ .Request.Body.index }}.mute",
				Value:    "{{ .Request.Body.mute }}",
				Optional: true,
			},
		},
	}
	if err := dev.AddRouteFromProfile(route, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/system/configure",
		strings.NewReader(`{"index": 99, "mute": true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Errorf("body = %s, want success template", w.Body.String())
	}
	got := dev.State()["status"].(map[string]any)["device"].(map[string]any)["power"]
	if got != "on" {
		t.Errorf("required mutation did not apply: power = %v, want on", got)
	}
}

// TestServeHTTP_MixedRequiredSuccessRequiredFailureReturns400 confirms
// the partial-failure case: when one required mutation succeeds and a
// second required mutation fails, the request is 400. State writes from
// the successful mutation still applied, matching applyMutations'
// per-entry semantics; the response surfaces the failure so the client
// knows the batch was not clean.
func TestServeHTTP_MixedRequiredSuccessRequiredFailureReturns400(t *testing.T) {
	initial := map[string]any{
		"status": map[string]any{
			"device":   map[string]any{"power": "off"},
			"channels": []any{map[string]any{"mute": false}},
		},
	}
	dev := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 1, initial)
	route := &profile.Route{
		Match:    "POST /api/system/configure",
		Template: "ack.json.gotempl",
		Mutations: []profile.Mutation{
			{
				Path:  "status.device.power",
				Value: "on",
			},
			{
				Path:  "status.channels.{{ .Request.Body.index }}.mute",
				Value: "{{ .Request.Body.mute }}",
			},
		},
	}
	if err := dev.AddRouteFromProfile(route, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/system/configure",
		strings.NewReader(`{"index": 99, "mute": true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Error           string `json:"error"`
		FailedMutations []struct {
			Path  string `json:"path"`
			Error string `json:"error"`
		} `json:"failed_mutations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body %q: %v", w.Body.String(), err)
	}
	if len(body.FailedMutations) != 1 {
		t.Fatalf("expected 1 failed mutation, got %d: %s", len(body.FailedMutations), w.Body.String())
	}
	if body.FailedMutations[0].Path != "status.channels.99.mute" {
		t.Errorf("failed_mutations[0].path = %q, want status.channels.99.mute", body.FailedMutations[0].Path)
	}
}
