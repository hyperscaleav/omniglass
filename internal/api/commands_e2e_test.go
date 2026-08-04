package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestCommandIssueAPI drives the command pillar's composition over HTTP: issuing a
// command records a caused event, opens the intended value the observed value settles
// against, and returns the computed settlement verdict. A zero settle window makes the
// verdict immediate: observed matching intended is settled, a mismatch is failed. An
// unknown command_type is a 422, an out-of-scope component a 404, and an ungranted
// caller a 403.
func TestCommandIssueAPI(t *testing.T) {
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
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	// A settleable command with a zero window, so settlement is immediate.
	if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
		Name: "set-input-now", DisplayName: "Set input (now)", TargetPropertyType: "video.input", SettleWindowSeconds: 0,
	}); err != nil {
		t.Fatalf("create command type: %v", err)
	}
	// The device already reports hdmi2 (an observed value the command settles against).
	if err := gw.UpsertProperties(ctx, []storage.PropertyUpsert{{
		OwnerKind: "component", OwnerID: "disp-1", Key: "video.input", Instance: "", Provenance: "observed",
		Value: json.RawMessage(`"hdmi2"`), TS: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("seed observed: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Issue set-input-now to hdmi2: the observed value already matches, so past the
	// zero window it settles. The response carries the caused event id.
	var settled struct {
		CausedEventID int64  `json:"caused_event_id"`
		Settlement    string `json:"settlement"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components/disp-1/commands:issue", map[string]any{
		"command_type": "set-input-now", "value": "hdmi2",
	}, http.StatusOK), &settled)
	if settled.Settlement != "settled" || settled.CausedEventID == 0 {
		t.Fatalf("issue to matching value: want settled with a caused event, got %+v", settled)
	}

	// The caused event was recorded: a command.issued occurrence with origin caused.
	var events struct {
		Events []struct {
			Key    string `json:"key"`
			Origin string `json:"origin"`
		} `json:"events"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/disp-1/events", nil, http.StatusOK), &events)
	var foundCaused bool
	for _, e := range events.Events {
		if e.Key == "command.issued" && e.Origin == "caused" {
			foundCaused = true
		}
	}
	if !foundCaused {
		t.Fatalf("no caused command.issued event recorded: %+v", events.Events)
	}

	// Issue to hdmi3: the observed value (hdmi2) no longer matches the intended one,
	// so past the zero window it is a failed command.
	var failed struct {
		Settlement string `json:"settlement"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components/disp-1/commands:issue", map[string]any{
		"command_type": "set-input-now", "value": "hdmi3",
	}, http.StatusOK), &failed)
	if failed.Settlement != "failed" {
		t.Fatalf("issue to mismatched value: want failed, got %q", failed.Settlement)
	}

	// An unknown command_type is a 422.
	c.do(ownerTok, http.MethodPost, "/components/disp-1/commands:issue", map[string]any{"command_type": "no_such_command"}, http.StatusUnprocessableEntity)

	// An operator with command:issue but scoped to a different component gets a
	// non-disclosing 404 on disp-1 (permission gate passes, scope injection hides it).
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1"}, all); err != nil {
		t.Fatalf("create other: %v", err)
	}
	other, err := gw.GetComponent(ctx, "other-1", all)
	if err != nil {
		t.Fatalf("get other: %v", err)
	}
	opTok := setupScopedViewer(t, ctx, dsn, "op-other", "operator", "component", other.ID)
	c.do(opTok, http.MethodPost, "/components/disp-1/commands:issue", map[string]any{"command_type": "reboot"}, http.StatusNotFound)

	// An ungranted principal is forbidden to issue.
	noneTok := principalWithGrants(t, ctx, dsn, "nocmd", nil)
	c.do(noneTok, http.MethodPost, "/components/disp-1/commands:issue", map[string]any{"command_type": "reboot"}, http.StatusForbidden)
}
