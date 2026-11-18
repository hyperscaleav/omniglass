package api_test

import (
	"crypto/x509"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// liveKey is the install's model key; no route, event or log line may carry it.
const liveKey = "ghp-live-key-0123456789"

// fakeModel is an OpenAI-compatible chat completions server: a turn with no
// tool result in it calls list-components, a turn after one answers in text.
// It records the bearer and the model each request named.
type fakeModel struct {
	mu     sync.Mutex
	auths  []string
	models []string
	answer string
}

func (f *fakeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || r.URL.Path != "/chat/completions" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.auths = append(f.auths, r.Header.Get("Authorization"))
	f.models = append(f.models, req.Model)
	f.mu.Unlock()
	msg := map[string]any{"role": "assistant", "content": nil, "tool_calls": []map[string]any{{
		"id": "call-live", "type": "function", "function": map[string]any{"name": "list-components", "arguments": "{}"},
	}}}
	if req.Messages[len(req.Messages)-1].Role == "tool" {
		msg = map[string]any{"role": "assistant", "content": f.answer}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": msg}}})
}

func (f *fakeModel) seen() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...), append([]string(nil), f.models...)
}

func lastText(evs []map[string]any) string {
	var out string
	for _, ev := range evs {
		if ev["type"] == "TEXT_MESSAGE_CONTENT" {
			out, _ = ev["delta"].(string)
		}
	}
	return out
}

func TestOmnibotRunsOnTheConfiguredModel(t *testing.T) {
	t.Parallel()
	first := &fakeModel{answer: "Two displays."}
	second := &fakeModel{answer: "Still two displays."}
	srv1, srv2 := httptest.NewTLSServer(first), httptest.NewTLSServer(second)
	defer srv1.Close()
	defer srv2.Close()
	refusing := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A server that echoes the key it was sent, as some do.
		http.Error(w, `{"error":{"message":"bad credentials: `+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")+`"}}`, http.StatusUnauthorized)
	}))
	defer refusing.Close()
	roots := x509.NewCertPool()
	for _, s := range []*httptest.Server{srv1, srv2, refusing} {
		roots.AddCert(s.Certificate())
	}
	logs := &lockedBuffer{}
	p := newPeople(t, runModel(t), api.WithEgressOptions(egress.Options{RootCAs: roots}),
		api.WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	if _, err := p.gw.CreateSecret(p.ctx, "", storage.SecretSpec{
		Name: "models-key", SecretType: "api-key", OwnerKind: "platform", Fields: map[string]string{"key": liveKey},
	}, scope.Set{All: true}, true); err != nil {
		t.Fatal(err)
	}
	p.person("olga", "operator", "all", "")
	p.c.do(p.owner, http.MethodPatch, "/settings/notification", map[string]any{"egress_allow": []string{"127.0.0.1"}}, http.StatusOK)
	p.c.do(p.owner, http.MethodPatch, "/settings/assistant", map[string]any{
		"provider": "openai", "base_url": srv1.URL, "key_secret": "models-key", "model": "test-model",
	}, http.StatusOK)

	var all []map[string]any
	converse := func(name, text string) []map[string]any {
		t.Helper()
		p.c.do(p.toks["olga"], http.MethodPost, "/assistant/conversations", map[string]any{"name": name}, http.StatusCreated)
		status, evs := p.run(p.toks["olga"], name, prompt(text))
		if status != http.StatusOK {
			t.Fatalf(":run = %d %v", status, evs)
		}
		all = append(all, evs...)
		return evs
	}

	// The configured model answers, called with the key and the model named.
	evs := converse("live", "how many displays?")
	want := "RUN_STARTED,TOOL_CALL_START,TOOL_CALL_ARGS,TOOL_CALL_END,TOOL_CALL_RESULT," +
		"TEXT_MESSAGE_START,TEXT_MESSAGE_CONTENT,TEXT_MESSAGE_END,RUN_FINISHED"
	if got := eventTypes(evs); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	if got := lastText(evs); got != "Two displays." {
		t.Errorf("answer = %q, want the live model's", got)
	}
	auths, models := first.seen()
	if len(auths) != 2 || auths[0] != "Bearer "+liveKey || models[0] != "test-model" {
		t.Fatalf("the model saw auths %v models %v, want two turns with the key and test-model", auths, models)
	}

	// A new base_url is the next run's model, with no restart.
	p.c.do(p.owner, http.MethodPatch, "/settings/assistant", map[string]any{"base_url": srv2.URL}, http.StatusOK)
	if got := lastText(converse("moved", "and now?")); got != "Still two displays." {
		t.Errorf("after the move, answer = %q, want the second server's", got)
	}

	// A refused key ends the run on an error that names it, without the key.
	p.c.do(p.owner, http.MethodPatch, "/settings/assistant", map[string]any{"base_url": refusing.URL}, http.StatusOK)
	evs = converse("refused", "hello?")
	last := evs[len(evs)-1]
	if msg, _ := last["message"].(string); last["type"] != "RUN_ERROR" || !strings.Contains(msg, "refused the key") {
		t.Errorf("refused run ended on %v, want an error naming the refused key", last)
	}

	// No event, read or log line carries the key.
	for _, where := range []struct{ name, body string }{
		{"events", func() string { b, _ := json.Marshal(all); return string(b) }()},
		{"conversation", string(p.c.do(p.toks["olga"], http.MethodGet, "/assistant/conversations/refused", nil, http.StatusOK))},
		{"settings", string(p.c.do(p.owner, http.MethodGet, "/settings", nil, http.StatusOK))},
		{"logs", logs.String()},
	} {
		if strings.Contains(where.body, liveKey) {
			t.Errorf("the key leaked into the %s: %s", where.name, where.body)
		}
	}
	if !strings.Contains(logs.String(), "refused the key") {
		t.Errorf("logs = %q, want the refused run logged", logs.String())
	}

	// A bound key that no longer resolves refuses the run, naming why.
	if err := p.gw.DeleteSecret(p.ctx, "", "models-key", scope.Set{All: true}, scope.Set{All: true}, true, true); err != nil {
		t.Fatal(err)
	}
	p.c.do(p.toks["olga"], http.MethodPost, "/assistant/conversations", map[string]any{"name": "keyless"}, http.StatusCreated)
	if status, evs := p.run(p.toks["olga"], "keyless", prompt("hello?")); status != http.StatusServiceUnavailable ||
		!strings.Contains(evs[0]["message"].(string), "models-key") {
		t.Errorf("a vanished key: %d %v, want a 503 naming the secret", status, evs)
	}

	// With no provider, the recording plays.
	p.c.do(p.owner, http.MethodPatch, "/settings/assistant", map[string]any{"provider": "none", "key_secret": ""}, http.StatusOK)
	evs = converse("recorded", "hello?")
	if b, _ := json.Marshal(evs); !strings.Contains(string(b), `"Let me look."`) {
		t.Errorf("with no provider, events = %s, want the recording's", b)
	}
}
