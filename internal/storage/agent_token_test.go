package storage_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// An agent token (#1179) is stored with the agent it is for and its bounds,
// comes back on authentication as the minting person plus that agent, lists
// with its bounds on the person's own sessions, and every row it writes names
// the agent, so the trail reads "the agent did this on the person's behalf".
// Skipped under -short.
func TestAgentTokenRoundTripAndAudit(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, ownerHash, ownerPrefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: ownerHash, Prefix: ownerPrefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, err := gw.AuthenticateBearer(ctx, ownerHash)
	if err != nil {
		t.Fatalf("authenticate root: %v", err)
	}
	if root.Agent != nil {
		t.Fatalf("a personal token authenticated as agent %+v", root.Agent)
	}

	_, h, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	exp := time.Now().Add(24 * time.Hour)
	ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{
		ActorID: root.ID, Username: "root", SecretHash: h, Prefix: prefix, Purpose: "token",
		Description: "triage bot", ExpiresAt: &exp,
		Agent: "claude-desktop", AgentRoles: []string{"viewer"}, AgentChannels: []string{"mcp"},
	})
	if err != nil || !ok {
		t.Fatalf("issue agent token = (%v, %v)", ok, err)
	}

	pr, err := gw.AuthenticateBearer(ctx, h)
	if err != nil {
		t.Fatalf("authenticate agent token: %v", err)
	}
	if pr.ID != root.ID {
		t.Errorf("agent token principal = %s, want the minting person %s", pr.ID, root.ID)
	}
	if pr.Agent == nil || pr.Agent.Name != "claude-desktop" || !slices.Equal(pr.Agent.Roles, []string{"viewer"}) ||
		!slices.Equal(pr.Agent.Channels, []string{"mcp"}) || pr.Agent.CredentialID == "" {
		t.Fatalf("agent = %+v, want claude-desktop, [viewer], [mcp] and its credential id", pr.Agent)
	}

	creds, err := gw.ListBearerCredentials(ctx, root.ID, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed *storage.BearerCredential
	for i := range creds {
		if creds[i].Agent != "" {
			listed = &creds[i]
		}
	}
	if listed == nil || listed.Agent != "claude-desktop" || !slices.Equal(listed.AgentRoles, []string{"viewer"}) {
		t.Fatalf("listed agent token = %+v", listed)
	}

	// The mint row carries the agent and its bounds.
	created, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "credential", Verb: "create", Limit: 10})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var img map[string]any
	for _, r := range created {
		if r.ResourceID == listed.ID {
			if err := json.Unmarshal(r.New, &img); err != nil {
				t.Fatalf("decode: %v", err)
			}
		}
	}
	if img["agent"] != "claude-desktop" {
		t.Errorf("mint image %v, want agent claude-desktop", img)
	}

	// A row written under the agent context names the agent, and the filter
	// keeps only the agent's rows.
	actx := storage.WithAgent(ctx, "claude-desktop")
	if err := gw.WriteAuthEventFor(actx, root.ID, "login", "", ""); err != nil {
		t.Fatalf("write agent row: %v", err)
	}
	if err := gw.WriteAuthEventFor(ctx, root.ID, "login", "", ""); err != nil {
		t.Fatalf("write person row: %v", err)
	}
	byAgent, err := gw.ListAuditLog(ctx, storage.AuditFilter{Agent: "claude-desktop", Limit: 10})
	if err != nil {
		t.Fatalf("list by agent: %v", err)
	}
	if len(byAgent) != 1 || byAgent[0].Agent != "claude-desktop" || byAgent[0].ActorID != root.ID {
		t.Fatalf("rows by agent = %+v, want the one login, by root, as claude-desktop", byAgent)
	}

	// Revoking the token audits its agent too.
	if ok, err := gw.RevokeBearerByID(ctx, root.ID, root.ID, listed.ID); err != nil || !ok {
		t.Fatalf("revoke = (%v, %v)", ok, err)
	}
	revoked, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "credential", Verb: "revoke", Limit: 10})
	if err != nil || len(revoked) != 1 {
		t.Fatalf("revoke rows = %v, %v", revoked, err)
	}
	var old map[string]any
	_ = json.Unmarshal(revoked[0].Old, &old)
	if old["agent"] != "claude-desktop" {
		t.Errorf("revoke image %v, want agent claude-desktop", old)
	}
	if _, err := gw.AuthenticateBearer(ctx, h); err == nil {
		t.Error("a revoked agent token still authenticates")
	}
}
