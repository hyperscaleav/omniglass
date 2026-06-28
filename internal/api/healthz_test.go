package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestHealthzReportsDBOK is the integration proof: migrate applies clean
// against a real testcontainer Postgres, the Storage Gateway Ping succeeds, and
// GET /api/v1/healthz returns 200 with status=ok, db=ok. Skipped under -short.
func TestHealthzReportsDBOK(t *testing.T) {
	gw := storagetest.NewDB(t)

	if err := gw.Ping(context.Background()); err != nil {
		t.Fatalf("gateway ping against migrated DB: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/healthz")
	if err != nil {
		t.Fatalf("GET healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		Status string `json:"status"`
		DB     string `json:"db"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if got.DB != "ok" {
		t.Errorf("db = %q, want ok", got.DB)
	}
}

// degradedGateway is a Gateway whose Ping always fails, proving healthz maps a
// down database leg to status=degraded, db=down (still HTTP 200: the process is
// up, the probe just reports the leg). It is a test double of a COLLABORATOR
// (the gateway), not of the system under test (the handler), so it does not
// violate the no-mocking-the-DB doctrine: the real-DB path is covered by
// TestHealthzReportsDBOK above.
type degradedGateway struct{ storage.UnimplementedGateway }

func (degradedGateway) Ping(context.Context) error { return errDown }

type sentinel string

func (s sentinel) Error() string { return string(s) }

const errDown = sentinel("db unreachable")

func TestHealthzReportsDBDown(t *testing.T) {
	srv := httptest.NewServer(api.NewHandler(degradedGateway{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/healthz")
	if err != nil {
		t.Fatalf("GET healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		Status string `json:"status"`
		DB     string `json:"db"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Status != "degraded" {
		t.Errorf("status = %q, want degraded", got.Status)
	}
	if got.DB != "down" {
		t.Errorf("db = %q, want down", got.DB)
	}
}
