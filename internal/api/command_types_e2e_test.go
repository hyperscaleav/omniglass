package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestCommandTypeAPI drives the /command-types catalog over HTTP: an owner registers,
// reads, updates, and deletes a custom command type (with a settle window and a target
// property); an official (seeded) type is read-only; a malformed name, a duplicate, and
// an unknown target property are rejected; an ungranted principal is forbidden to
// create. Mirrors the property_type and event_type catalog e2e.
func TestCommandTypeAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Register a custom, settleable command type targeting a seeded property.
	created := c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{
		"name": "set-volume", "display_name": "Set volume",
		"settle_window_seconds": 5, "target_property_type": "video-input",
	}, http.StatusCreated)
	var ct struct {
		Name                string `json:"name"`
		SettleWindowSeconds int    `json:"settle_window_seconds"`
		TargetPropertyType  string `json:"target_property_type"`
		Official            bool   `json:"official"`
	}
	json.Unmarshal(created, &ct)
	if ct.Name != "set-volume" || ct.SettleWindowSeconds != 5 || ct.TargetPropertyType != "video-input" || ct.Official {
		t.Fatalf("created = %+v", ct)
	}

	// Get it back; list includes the custom + the seeded official ones.
	c.do(ownerTok, http.MethodGet, "/command-types/set-volume", nil, http.StatusOK)
	var listed struct {
		CommandTypes []struct {
			Name string `json:"name"`
		} `json:"command_types"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/command-types", nil, http.StatusOK), &listed)
	names := map[string]bool{}
	for _, e := range listed.CommandTypes {
		names[e.Name] = true
	}
	if !names["set-volume"] || !names["set-input"] || !names["reboot"] {
		t.Fatalf("list missing command types: %v", names)
	}

	// Update the settle window.
	c.do(ownerTok, http.MethodPatch, "/command-types/set-volume", map[string]any{"settle_window_seconds": 10}, http.StatusOK)

	// A malformed name is a 422; an unknown target property is a 422.
	c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{"name": "Bad-Name"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{"name": "set_thing", "target_property_type": "no.such.property"}, http.StatusUnprocessableEntity)

	// A duplicate name is a 409.
	c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{"name": "set-volume"}, http.StatusConflict)

	// The other arm of the exclusive arc (#596): a metric-targeted type created by
	// name round-trips through the read, on the create response and on GET.
	var armed struct {
		TargetPropertyType string `json:"target_property_type"`
		TargetMetricType   string `json:"target_metric_type"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{
		"name": "set-gain", "settle_window_seconds": 5, "target_metric_type": "icmp-rtt-avg",
	}, http.StatusCreated), &armed)
	if armed.TargetMetricType != "icmp-rtt-avg" || armed.TargetPropertyType != "" {
		t.Fatalf("metric-targeted create = %+v, want the metric arm carried", armed)
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/command-types/set-gain", nil, http.StatusOK), &armed)
	if armed.TargetMetricType != "icmp-rtt-avg" {
		t.Fatalf("read dropped the metric arm: %+v", armed)
	}

	// A patch naming the other arm switches the target wholesale (one target,
	// two forms), and an empty arm clears it outright. The struct is zeroed
	// between decodes because a cleared arm is omitted from the body entirely.
	armed.TargetPropertyType, armed.TargetMetricType = "", ""
	json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/command-types/set-gain",
		map[string]any{"target_property_type": "video-input"}, http.StatusOK), &armed)
	if armed.TargetPropertyType != "video-input" || armed.TargetMetricType != "" {
		t.Fatalf("arm switch = %+v, want the property arm set and the metric arm cleared", armed)
	}
	armed.TargetPropertyType, armed.TargetMetricType = "", ""
	json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/command-types/set-gain",
		map[string]any{"target_property_type": ""}, http.StatusOK), &armed)
	if armed.TargetPropertyType != "" || armed.TargetMetricType != "" {
		t.Fatalf("cleared target = %+v, want fire-and-forget", armed)
	}

	// Both targets in one request is the arc refusal, named: a 422 whose message
	// says never both (the DB CHECK is the backstop, not the message).
	arc := c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{
		"name": "both-arms", "target_property_type": "video-input", "target_metric_type": "icmp-rtt-avg",
	}, http.StatusUnprocessableEntity)
	if !strings.Contains(string(arc), "never both") {
		t.Fatalf("both-targets 422 body %s does not name the arc", arc)
	}

	// An unregistered metric target is a 422 naming the name.
	miss := c.do(ownerTok, http.MethodPost, "/command-types",
		map[string]any{"name": "set-gain-m", "target_metric_type": "no-such-metric"}, http.StatusUnprocessableEntity)
	if !strings.Contains(string(miss), "no-such-metric") {
		t.Fatalf("unregistered metric target 422 body %s does not name the target", miss)
	}

	// An official (seeded) command type is read-only.
	c.do(ownerTok, http.MethodPatch, "/command-types/set-input", map[string]any{"display_name": "x"}, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/command-types/reboot", nil, http.StatusConflict)

	// An unknown command type is a 404.
	c.do(ownerTok, http.MethodGet, "/command-types/nope", nil, http.StatusNotFound)

	// Delete the custom command type.
	c.do(ownerTok, http.MethodDelete, "/command-types/set-volume", nil, http.StatusNoContent)

	// An ungranted principal is forbidden to create.
	noneTok := principalWithGrants(t, ctx, dsn, "nocmds", nil)
	c.do(noneTok, http.MethodPost, "/command-types", map[string]any{"name": "nope_cmd"}, http.StatusForbidden)
}
