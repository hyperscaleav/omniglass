package storage_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A token is how an external agent (an MCP client, a script) is given access, so
// minting and revoking one are audited (#1162): a create row when a token is
// issued and a revoke row for each token ended, one at a time or in bulk, each
// naming the credential by id and carrying its non-secret metadata, never the
// token or its hash. A web-login session is the auth lane's (login, logout) and
// writes no credential row. Skipped under -short.
func TestTokenMintAndRevokeAreAudited(t *testing.T) {
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

	issue := func(purpose, description string) (hash []byte) {
		t.Helper()
		_, h, prefix, err := auth.NewBearerToken()
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{
			ActorID: root.ID, Username: "root", SecretHash: h, Prefix: prefix, Purpose: purpose, Description: description,
		})
		if err != nil || !ok {
			t.Fatalf("issue %s = (%v, %v)", purpose, ok, err)
		}
		return h
	}
	issue("token", "agent token one")
	issue("token", "agent token two")
	issue("session", "")

	rows := func(verb string) []storage.AuditEntry {
		t.Helper()
		out, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "credential", Verb: verb, Limit: 50})
		if err != nil {
			t.Fatalf("list %s: %v", verb, err)
		}
		return out
	}
	created := rows("create")
	if len(created) != 2 {
		t.Fatalf("credential create rows = %d, want 2 (the tokens, not the session)", len(created))
	}
	for _, r := range created {
		if r.ActorID != root.ID || r.ResourceID == "" {
			t.Errorf("create row actor=%q resource_id=%q, want root and the credential id", r.ActorID, r.ResourceID)
		}
		img := string(r.New)
		var fields map[string]any
		if err := json.Unmarshal(r.New, &fields); err != nil {
			t.Fatalf("decode create image %s: %v", img, err)
		}
		if desc, _ := fields["description"].(string); !strings.HasPrefix(desc, "agent token") || fields["purpose"] != "token" || fields["prefix"] == "" {
			t.Errorf("create image %s, want the description, purpose and prefix", img)
		}
		for _, banned := range []string{"secret_hash", "SecretHash", "hash"} {
			if strings.Contains(img, banned) {
				t.Errorf("create image carries %q: %s", banned, img)
			}
		}
	}

	creds, err := gw.ListBearerCredentials(ctx, root.ID, nil)
	if err != nil {
		t.Fatalf("list credentials: %v", err)
	}
	var oneToken, session string
	for _, c := range creds {
		switch {
		case c.Purpose == "token" && c.Description == "agent token one":
			oneToken = c.ID
		case c.Purpose == "session":
			session = c.ID
		}
	}
	if ok, err := gw.RevokeBearerByID(ctx, root.ID, root.ID, oneToken); err != nil || !ok {
		t.Fatalf("revoke token = (%v, %v)", ok, err)
	}
	if ok, err := gw.RevokeBearerByID(ctx, root.ID, root.ID, session); err != nil || !ok {
		t.Fatalf("revoke session = (%v, %v)", ok, err)
	}
	revoked := rows("revoke")
	if len(revoked) != 1 || revoked[0].ResourceID != oneToken || revoked[0].ActorID != root.ID {
		t.Fatalf("revoke rows after one-at-a-time = %+v, want one for the token, by root", revoked)
	}

	// The bulk revoke of tokens writes a row per token it ended (the owner's own
	// bootstrap token is kept, as the API keeps the caller's credential).
	n, err := gw.RevokeBearersByPurposeExcept(ctx, root.ID, root.ID, "token", [][]byte{ownerHash})
	if err != nil || n != 1 {
		t.Fatalf("bulk revoke = (%d, %v), want 1", n, err)
	}
	if got := len(rows("revoke")); got != 2 {
		t.Fatalf("revoke rows after bulk = %d, want 2", got)
	}
}
