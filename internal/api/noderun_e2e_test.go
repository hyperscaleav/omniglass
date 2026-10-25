package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The on-demand run routes' contract (#1163), the node behind them a stand-in
// that records what it was asked (the real wire is internal/node's
// run_ondemand_test): who may validate, preview and run now; what reaches the
// node; what comes back masked; and the stricter class that bounds runs in
// flight. Skipped under -short.

const nodeRunSecret = "node-run-pw-value"

// recordingRunner is the dependency behind the routes: it records each ask
// and answers with a fixed result, holding each answer until release closes
// when one is set.
type recordingRunner struct {
	mu       sync.Mutex
	previews [][]storage.WorklistTask
	runs     []string
	release  chan struct{}
}

func (r *recordingRunner) RunPreview(_ context.Context, node string, tasks []storage.WorklistTask, _ time.Duration) (collection.RunReply, error) {
	r.mu.Lock()
	r.previews = append(r.previews, tasks)
	rel := r.release
	r.mu.Unlock()
	if rel != nil {
		<-rel
	}
	return collection.RunReply{Results: []collection.RunResult{{TaskID: tasks[0].ID, Function: "identity",
		Samples: []collection.RunSample{{Name: "model-number", Text: "Edge 86"}},
		Excerpt: `{"device":{"model":"Edge 86"},"echo":"` + nodeRunSecret + `"}`}}}, nil
}

func (r *recordingRunner) RunTaskNow(_ context.Context, node, taskID string, _ time.Duration) (collection.RunReply, error) {
	r.mu.Lock()
	r.runs = append(r.runs, node+"/"+taskID)
	r.mu.Unlock()
	return collection.RunReply{Results: []collection.RunResult{{TaskID: taskID, Function: "identity", Samples: []collection.RunSample{{Name: "model-number", Text: "Edge 86"}}}}}, nil
}

type nodeRunStack struct {
	c      *apiClient
	runner *recordingRunner
	owner  string
	dsn    string
	ctx    context.Context
}

func newNodeRunStack(t *testing.T, opts ...api.Option) *nodeRunStack {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-api"}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: "display-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": nodeRunSecret}}, all, true); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	srv := httptest.NewServer(api.NewHandler(gw, append([]api.Option{api.WithNodeRunner(runner)}, opts...)...))
	t.Cleanup(srv.Close)
	return &nodeRunStack{c: &apiClient{t: t, ctx: ctx, base: srv.URL}, runner: runner, owner: bootstrapOwnerTok(t, ctx, gw), dsn: dsn, ctx: ctx}
}

var previewBody = map[string]any{"node": "edge-api", "driver": "boreal-edge-web", "inputs": map[string]string{"host": "10.20.4.31", "login": "display-login"}}

// A preview reaches the node named with the spec's polls and the secret
// unsealed, and its answer comes back masked of that secret.
func TestPreviewReachesTheNodeAndComesBackMasked(t *testing.T) {
	s := newNodeRunStack(t)
	raw := s.c.do(s.owner, http.MethodPost, "/drivers:preview", previewBody, http.StatusOK)
	var out struct {
		Node, Target string
		Stored       bool
		Results      []struct {
			Poll    string `json:"poll"`
			Excerpt string `json:"excerpt"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Node != "edge-api" || out.Target != "10.20.4.31:80" || out.Stored || len(out.Results) != 1 || out.Results[0].Poll != "identity" {
		t.Fatalf("preview = %s", raw)
	}
	if strings.Contains(string(raw), nodeRunSecret) || !strings.Contains(out.Results[0].Excerpt, "[redacted]") {
		t.Errorf("the answer carries the secret: %s", raw)
	}
	if len(s.runner.previews) != 1 || s.runner.previews[0][0].Secrets["login"]["password"] != nodeRunSecret {
		t.Errorf("the node was asked %+v", s.runner.previews)
	}
}

// A spec that does not validate is a 422 listing its findings, and the node is
// never asked; exactly one of driver or spec is required.
func TestPreviewRefusals(t *testing.T) {
	s := newNodeRunStack(t)
	code, raw := s.c.send(s.owner, http.MethodPost, "/drivers:preview", map[string]any{"node": "edge-api",
		"spec": map[string]any{"version": 1, "transport": "http", "polls": []map[string]any{{"name": "p", "schedule": map[string]any{"every": "soon"},
			"request": map[string]any{"path": "/x"}, "emits": []map[string]any{{"name": "uptime", "extract": map[string]any{"jsonpath": "a"}}}}}}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(string(raw), "schedule") {
		t.Errorf("an invalid spec = %d %s, want 422 with its findings", code, raw)
	}
	if code, _ := s.c.send(s.owner, http.MethodPost, "/drivers:preview", map[string]any{"node": "edge-api"}); code != http.StatusUnprocessableEntity {
		t.Errorf("neither driver nor spec = %d, want 422", code)
	}
	if code, _ := s.c.send(s.owner, http.MethodPost, "/drivers:preview", map[string]any{"node": "no-such-node", "driver": "boreal-edge-web", "inputs": map[string]string{"host": "10.0.0.1", "login": "display-login"}}); code != http.StatusNotFound {
		t.Errorf("an unknown node = %d, want 404", code)
	}
	if len(s.runner.previews) != 0 {
		t.Errorf("a refused preview asked the node: %+v", s.runner.previews)
	}
}

// A viewer may validate (a pure check under driver:read) but neither preview
// nor run a poll now; an operator may do both.
func TestOnDemandRunPermissions(t *testing.T) {
	s := newNodeRunStack(t)
	viewer := principalWithGrants(t, s.ctx, s.dsn, "run-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	operator := principalWithGrants(t, s.ctx, s.dsn, "run-operator", []grant{{role: "operator", scopeKind: "all"}})
	spec := map[string]any{"spec": map[string]any{"version": 1, "transport": "tcp"}}
	if code, _ := s.c.send(viewer, http.MethodPost, "/drivers:validate", spec); code != http.StatusOK {
		t.Errorf("viewer validate = %d, want 200", code)
	}
	if code, _ := s.c.send(viewer, http.MethodPost, "/drivers:preview", previewBody); code != http.StatusForbidden {
		t.Errorf("viewer preview = %d, want 403", code)
	}
	if code, _ := s.c.send(operator, http.MethodPost, "/drivers:preview", previewBody); code != http.StatusOK {
		t.Errorf("operator preview = %d, want 200", code)
	}

	s.c.do(s.owner, http.MethodPost, "/components", map[string]any{"name": "display-api", "product": "generic-device"}, http.StatusCreated)
	var ep struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodPost, "/endpoints", map[string]any{"component": "display-api", "driver": "boreal-edge-web", "node": "edge-api",
		"inputs": map[string]string{"host": "10.20.4.31", "login": "display-login"}}, http.StatusCreated), &ep); err != nil {
		t.Fatal(err)
	}
	// Nodes take an all-scope node:read, so an operator scoped to one
	// component may not name one for a preview: a 403, not a 500.
	compID := entityID(t, s.c, s.owner, "/components", "display-api")
	scoped := principalWithGrants(t, s.ctx, s.dsn, "run-scoped", []grant{{role: "operator", scopeKind: "component", scopeID: compID}})
	if code, _ := s.c.send(scoped, http.MethodPost, "/drivers:preview", previewBody); code != http.StatusForbidden {
		t.Errorf("a component-scoped preview = %d, want 403", code)
	}
	path := "/endpoints/display-api%2F" + ep.Name + "/polls/identity:run"
	if code, _ := s.c.send(viewer, http.MethodPost, path, nil); code != http.StatusForbidden {
		t.Errorf("viewer run now = %d, want 403", code)
	}
	if code, raw := s.c.send(operator, http.MethodPost, path, nil); code != http.StatusOK || !strings.Contains(string(raw), `"stored":true`) {
		t.Errorf("operator run now = %d %s", code, raw)
	}
	if len(s.runner.runs) != 1 || !strings.HasPrefix(s.runner.runs[0], "edge-api/") {
		t.Errorf("the node was asked %v", s.runner.runs)
	}
}

// The stricter class: past the per-caller bound a run is refused before
// anything is planned or asked.
func TestOnDemandRunsAreBounded(t *testing.T) {
	s := newNodeRunStack(t, api.WithNodeRunLimits(8, 1))
	s.runner.release = make(chan struct{})
	done := make(chan int, 1)
	go func() {
		code, _ := s.c.send(s.owner, http.MethodPost, "/drivers:preview", previewBody)
		done <- code
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.runner.mu.Lock()
		n := len(s.runner.previews)
		s.runner.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code, _ := s.c.send(s.owner, http.MethodPost, "/drivers:preview", previewBody); code != http.StatusTooManyRequests {
		t.Errorf("a second run in flight = %d, want 429", code)
	}
	close(s.runner.release)
	if code := <-done; code != http.StatusOK {
		t.Errorf("the first run = %d, want 200", code)
	}
}

// Without a bus nothing can reach a node: 503, not a hang.
func TestOnDemandRunsWithoutABus(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	if code, _ := c.send(bootstrapOwnerTok(t, ctx, gw), http.MethodPost, "/drivers:preview", previewBody); code != http.StatusServiceUnavailable {
		t.Errorf("preview without a bus = %d, want 503", code)
	}
}
