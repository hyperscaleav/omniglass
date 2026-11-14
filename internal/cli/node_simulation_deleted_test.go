package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// registryHub is the server's simulator registry with no simulator to reach:
// the simulation reads answer from the registry alone.
type registryHub struct{ *simhub.Registry }

func (h registryHub) SimStatus(node string) (simhub.Status, bool) { return h.Status(node) }

func (registryHub) FireSimOperation(context.Context, string, collection.SimOperation) (collection.SimOperationResult, error) {
	return collection.SimOperationResult{}, errors.New("no simulator")
}

func (registryHub) EndpointObservation(context.Context, string) (collection.EndpointObservation, error) {
	return collection.EndpointObservation{}, errors.New("no simulator")
}

// TestNodeSimulationOfADeletedNodeIsNotFound is #907's CLI acceptance, end to
// end: a node whose simulator pulled reads its simulation, and once `node
// delete` removes it, `node simulation` on it is the node-not-found error, not
// the stale status the registry held. A node created again under the same name
// starts with no simulator, not the deleted one's. Skipped under -short.
func TestNodeSimulationOfADeletedNodeIsNotFound(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("gw: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	future := time.Now().Add(time.Hour)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix, ExpiresAt: &future}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatalf("node: %v", err)
	}
	hub := registryHub{simhub.New()}
	hub.Pulled("edge", collection.SimCapabilities{}, nil)
	srv := httptest.NewServer(api.NewHandler(gw, api.WithSimHub(hub)))
	defer srv.Close()
	run := func(args ...string) (string, error) {
		t.Helper()
		return runCLI(t, append(args, "--server", srv.URL, "--token", tok)...)
	}
	decode := func(out string) map[string]any {
		t.Helper()
		var body map[string]any
		if err := json.Unmarshal([]byte(out), &body); err != nil {
			t.Fatalf("node simulation edge printed %q, not a JSON object", out)
		}
		return body
	}

	out, err := run("node", "simulation", "edge")
	if err != nil {
		t.Fatalf("node simulation edge before the delete: %v (%s)", err, out)
	}
	if status := decode(out); status["connected"] != true || status["last_seen"] == nil {
		t.Fatalf("node simulation edge before the delete = %v, want its simulator connected and seen", status)
	}

	if out, err := run("node", "delete", "edge"); err != nil {
		t.Fatalf("node delete edge: %v (%s)", err, out)
	}

	out, err = run("node", "simulation", "edge")
	if err == nil {
		t.Fatalf("node simulation edge after the delete printed %q, want the node-not-found error", out)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("node simulation edge after the delete: %v, want status 404", err)
	}
	if !strings.Contains(strings.ToLower(out), "not found") {
		t.Errorf("node simulation edge after the delete printed %q, want it to say the node is not found", out)
	}

	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatalf("node again: %v", err)
	}
	out, err = run("node", "simulation", "edge")
	if err != nil {
		t.Fatalf("node simulation edge once created again: %v (%s)", err, out)
	}
	if status := decode(out); status["connected"] != false || status["last_seen"] != nil {
		t.Errorf("node simulation edge once created again = %v, want no simulator, not the deleted node's", status)
	}
}
