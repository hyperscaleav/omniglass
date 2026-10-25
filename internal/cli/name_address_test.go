package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestCLIAddressesByName is #1133's CLI acceptance, end to end: the generated
// commands, a real server over real Postgres, and nothing typed but names.
// `endpoint retry <component address>/https` retries the endpoint, `secret
// update demo-panel-login --fields ...` rotates the secret, and the uuid each
// one answers with still addresses it. Skipped under -short.
func TestCLIAddressesByName(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
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
	campus, room := "hq", "huddle"
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: campus, LocationType: "campus"}, all); err != nil {
		t.Fatalf("campus: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: room, LocationType: "room", ParentName: &campus}, all); err != nil {
		t.Fatalf("room: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "touch-panel-1", LocationName: &room}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	panel := "hq.huddle.$comp.touch-panel-1"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "https", Component: &panel}, all)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: "demo-panel-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": "newtron"}}, all, true); err != nil {
		t.Fatalf("secret: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	run := func(args ...string) map[string]any {
		t.Helper()
		out, err := runCLI(t, append(args, "--server", srv.URL, "--token", tok)...)
		if err != nil {
			t.Fatalf("omniglass %s: %v (%s)", strings.Join(args, " "), err, out)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(out), &body); err != nil {
			t.Fatalf("omniglass %s printed %q, not a JSON object", strings.Join(args, " "), out)
		}
		return body
	}

	retried := run("endpoint", "retry", panel+"/https")
	if retried["id"] != ep.ID || retried["name"] != "https" {
		t.Errorf("endpoint retry by address answered %v, want endpoint %s", retried, ep.ID)
	}
	if got := run("endpoint", "get", ep.ID); got["id"] != ep.ID {
		t.Errorf("endpoint get by the uuid it answered with = %v", got)
	}

	rotated := run("secret", "update", "demo-panel-login", "--fields", `{"password":"rotated"}`)
	id, _ := rotated["id"].(string)
	if rotated["name"] != "demo-panel-login" || id == "" {
		t.Fatalf("secret update by name answered %v", rotated)
	}
	revealed := run("secret", "reveal", id)
	if fields, _ := revealed["fields"].(map[string]any); fields["password"] != "rotated" {
		t.Errorf("reveal by the uuid it answered with = %v, want the rotated password", revealed)
	}
}

// TestPathArgumentsAreOneSegment pins how a positional reaches the URL: as ONE
// path segment, whatever it holds. A slash (an endpoint's or a secret's
// qualified name) and a colon (a filename may carry one) are both
// percent-encoded, so neither splits the segment nor reads as the start of a
// :verb custom method; the server decodes them back.
func TestPathArgumentsAreOneSegment(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"endpoint", "retry", "hq.huddle.$comp.touch-panel-1/https"}, "/api/v1/endpoints/hq.huddle.$comp.touch-panel-1%2Fhttps:retry"},
		{[]string{"file", "download", "notes:v2.txt"}, "/api/v1/files/notes%3Av2.txt:download"},
		{[]string{"secret", "reveal", "$platform/demo-panel-login"}, "/api/v1/secrets/$platform%2Fdemo-panel-login:reveal"},
	} {
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.RequestURI
			_, _ = w.Write([]byte(`{}`))
		}))
		if out, err := runCLI(t, append(tc.args, "--server", srv.URL)...); err != nil {
			t.Fatalf("omniglass %v: %v (%s)", tc.args, err, out)
		}
		srv.Close()
		if got != tc.want {
			t.Errorf("omniglass %v requested %s, want %s", tc.args, got, tc.want)
		}
	}
}
