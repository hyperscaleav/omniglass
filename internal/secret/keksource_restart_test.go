package secret_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// chartKey is a key in the shape the Helm chart generates: 32 random
// alphanumeric characters, base64-encoded (randAlphaNum 32 | b64enc), so 32
// bytes once decoded.
const chartKey = "dDNzdEtleUZyb21UaGVDaGFydDB4MDEyMzQ1Njc4OWE="

// openWithEnvKey is one server boot: a fresh LoadKEK over the environment and an
// empty data directory (a new pod, nothing on disk), and a Gateway sealing under
// whatever key it resolved.
func openWithEnvKey(t *testing.T, dsn string, getenv func(string) string) storage.Gateway {
	t.Helper()
	warned := ""
	kek, src, err := secret.LoadKEK(getenv, t.TempDir(), func(m string) { warned = m })
	if err != nil {
		t.Fatalf("load kek: %v", err)
	}
	if src != secret.SourceEnv || warned != "" {
		t.Fatalf("source = %q (warned %q), want env with no warning: the chart never takes the generated-key path", src, warned)
	}
	gw, err := storage.NewPG(context.Background(), dsn, storage.WithSecretProvider(secret.NewStaticProvider(kek)))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	return gw
}

// TestSecretFromEnvKeySurvivesARestart pins the path a chart deployment relies
// on (#1286): the key arrives as OMNIGLASS_ENCRYPTION_KEY from a Kubernetes
// Secret, so a restarted pod with no data-directory state resolves the same key
// and reads back every secret the previous pod sealed.
func TestSecretFromEnvKeySurvivesARestart(t *testing.T) {
	t.Parallel()
	if raw, err := base64.StdEncoding.DecodeString(chartKey); err != nil || len(raw) != 32 {
		t.Fatalf("chartKey decodes to %d bytes (err %v), want 32", len(raw), err)
	}
	ctx := context.Background()
	all := scope.Set{All: true}
	dsn := storagetest.NewDSN(t)
	getenv := func(k string) string {
		if k == "OMNIGLASS_ENCRYPTION_KEY" {
			return chartKey
		}
		return ""
	}

	before := openWithEnvKey(t, dsn, getenv)
	if err := seed.Run(ctx, before); err != nil {
		t.Fatalf("seed: %v", err)
	}
	created, err := before.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "snmp", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "s3cr3t-ro"},
	}, all, true)
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}
	before.Close()

	after := openWithEnvKey(t, dsn, getenv)
	got, err := after.RevealSecret(ctx, "", created.ID, all, all, true)
	if err != nil {
		t.Fatalf("reveal after restart: %v", err)
	}
	if got["community"] != "s3cr3t-ro" {
		t.Errorf("community after restart = %q, want s3cr3t-ro", got["community"])
	}
}
