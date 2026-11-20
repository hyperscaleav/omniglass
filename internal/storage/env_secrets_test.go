package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/jackc/pgx/v5"
)

// envSecretGateway is secretGateway with the given env secrets declared, as
// the server's boot installs them (#2223).
func envSecretGateway(t *testing.T, env ...string) (*storage.PG, string) {
	t.Helper()
	gw, dsn := secretGateway(t)
	pg := gw.(*storage.PG)
	decl, err := secret.ParseEnv(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.InstallEnvSecrets(context.Background(), decl); err != nil {
		t.Fatalf("install env secrets: %v", err)
	}
	return pg, dsn
}

const envBedrock = `OMNIGLASS_SECRET__BEDROCK_KEY={"type":"api-key","fields":{"key":"sk-bedrock"}}`
const envRelay = `OMNIGLASS_SECRET__RELAY_LOGIN={"type":"basic-auth","fields":{"username":"mailer","password":"pw-relay"}}`

// An env secret is listed with its name, type and source, never its fields,
// under the all read scope only, and nothing of it reaches Postgres.
func TestEnvSecretListsWithoutFieldsAndIsNeverStored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw, dsn := envSecretGateway(t, envBedrock)

	list, err := gw.ListSecrets(ctx, all, false)
	if err != nil {
		t.Fatal(err)
	}
	var got *storage.Secret
	for i := range list {
		if list[i].Name == "bedrock-key" {
			got = &list[i]
		}
	}
	if got == nil {
		t.Fatalf("env secret not listed: %+v", list)
	}
	if got.Source != storage.SecretSourceEnv || got.SourceVar != "OMNIGLASS_SECRET__BEDROCK_KEY" || got.SecretType != "api-key" || got.OwnerKind != "platform" || len(got.Fields) != 0 {
		t.Fatalf("env secret = %+v", got)
	}
	scoped := scope.Set{IDs: []string{"00000000-0000-0000-0000-000000000001"}}
	if list, _ := gw.ListSecrets(ctx, scoped, true); len(list) != 0 {
		t.Fatalf("a scoped reader sees %+v", list)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from secret where name = 'bedrock-key' or value::text like '%sk-bedrock%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("env secret rows in postgres = %d, %v", n, err)
	}
}

// An update, delete, reveal or copy of an env secret, or a stored platform
// secret created under its name, is refused naming the variable.
func TestEnvSecretWritesAreRefusedNamingTheVariable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw, _ := envSecretGateway(t, envBedrock)
	check := func(what string, err error) {
		t.Helper()
		var fe *storage.SecretFromEnvError
		if !errors.Is(err, storage.ErrSecretFromEnv) || !errors.As(err, &fe) || fe.Var != "OMNIGLASS_SECRET__BEDROCK_KEY" {
			t.Fatalf("%s = %v, want the env refusal", what, err)
		}
		if strings.Contains(err.Error(), "sk-bedrock") {
			t.Fatalf("%s quotes the value: %v", what, err)
		}
	}
	_, err := gw.UpdateSecret(ctx, "", "bedrock-key", storage.SecretPatch{Fields: map[string]string{"key": "x"}}, all, all, true, true)
	check("update", err)
	check("delete", gw.DeleteSecret(ctx, "", "$platform/bedrock-key", all, all, true, true))
	check("delete by id", gw.DeleteSecret(ctx, "", "env:bedrock-key", all, all, true, true))
	_, err = gw.RevealSecret(ctx, "", "bedrock-key", all, all, true)
	check("reveal", err)
	_, err = gw.CopySecret(ctx, "", "bedrock-key", all, all, true)
	check("copy", err)
	_, err = gw.CreateSecret(ctx, "", storage.SecretSpec{Name: "bedrock-key", SecretType: "api-key", OwnerKind: "platform", Fields: map[string]string{"key": "x"}}, all, true)
	check("create", err)

	// A scoped caller cannot see it, so it reads as absent, not as env.
	scoped := scope.Set{IDs: []string{"00000000-0000-0000-0000-000000000001"}}
	if err := gw.DeleteSecret(ctx, "", "bedrock-key", scoped, scoped, true, true); !errors.Is(err, storage.ErrSecretNotFound) {
		t.Fatalf("scoped delete = %v, want not found", err)
	}
}

// The boot refuses a declaration the catalog cannot hold, naming the variable
// and never quoting the value.
func TestInstallEnvSecretsRefusesABadDeclaration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw, _ := secretGateway(t)
	pg := gw.(*storage.PG)
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: "stored-key", SecretType: "api-key", OwnerKind: "platform", Fields: map[string]string{"key": "x"}}, all, true); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct{ entry, want string }{
		"unknown type": {`OMNIGLASS_SECRET__K={"type":"nope","fields":{"key":"sk-hidden"}}`, "OMNIGLASS_SECRET__K"},
		"wrong fields": {`OMNIGLASS_SECRET__K={"type":"api-key","fields":{"password":"sk-hidden"}}`, "OMNIGLASS_SECRET__K"},
		"missing":      {`OMNIGLASS_SECRET__K={"type":"basic-auth","fields":{"username":"sk-hidden"}}`, `"password"`},
		"collision":    {`OMNIGLASS_SECRET__STORED_KEY={"type":"api-key","fields":{"key":"sk-hidden"}}`, `stored platform secret "stored-key"`},
	} {
		t.Run(name, func(t *testing.T) {
			decl, err := secret.ParseEnv([]string{c.entry})
			if err != nil {
				t.Fatal(err)
			}
			err = pg.InstallEnvSecrets(ctx, decl)
			if !errors.Is(err, secret.ErrEnvSecret) || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), "sk-hidden") {
				t.Fatalf("install = %v, want a refusal naming %s", err, c.want)
			}
		})
	}
}

// omnibot's key and the mail relay's login resolve from the environment, and
// naming either in the settings takes secret:bind on it as a stored one does.
func TestEnvSecretFeedsTheServersCredentialsBehindBind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw, _ := envSecretGateway(t, envBedrock, envRelay)
	noBind := storage.SecretUse{Read: all}
	bind := storage.SecretUse{Read: all, Bind: all, CanAdmin: true}

	if _, err := gw.MergePatchAssistantSettings(ctx, "", map[string]any{"provider": "openai", "key_secret": "bedrock-key"}, noBind); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Fatalf("key_secret without secret:bind = %v, want forbidden", err)
	}
	if _, err := gw.MergePatchAssistantSettings(ctx, "", map[string]any{"provider": "openai", "key_secret": "relay-login"}, bind); !errors.Is(err, storage.ErrModelKeyInvalid) {
		t.Fatalf("a basic-auth env secret as the key = %v, want invalid", err)
	}
	if _, err := gw.MergePatchAssistantSettings(ctx, "", map[string]any{"provider": "openai", "key_secret": "bedrock-key"}, bind); err != nil {
		t.Fatalf("binding the env key = %v", err)
	}
	m, err := gw.OmnibotModel(ctx)
	if err != nil || m == nil || m.Key != "sk-bedrock" {
		t.Fatalf("model = %+v, %v, want the env key", m, err)
	}

	if _, err := gw.MergePatchNotificationSettings(ctx, "", map[string]any{"mail_relay": "relay.example.com:587", "mail_from": "og@example.com", "mail_secret": "relay-login"}, noBind); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Fatalf("mail_secret without secret:bind = %v, want forbidden", err)
	}
	if _, err := gw.MergePatchNotificationSettings(ctx, "", map[string]any{"mail_relay": "relay.example.com:587", "mail_from": "og@example.com", "mail_secret": "relay-login"}, bind); err != nil {
		t.Fatalf("binding the env relay login = %v", err)
	}
	relay, refused, err := gw.MailRelay(ctx)
	if err != nil || refused != "" || relay.Username != "mailer" || relay.Password != "pw-relay" {
		t.Fatalf("relay = %+v, %q, %v", relay, refused, err)
	}
}

// An endpoint's secret input takes an env secret as it takes a stored
// platform one: the attach judges it under secret:bind, the cascade shows it
// at the platform band, and the worklist delivers its fields to the outpost.
func TestEnvSecretFeedsAnEndpointsSecretInput(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, _ := envSecretGateway(t, `OMNIGLASS_SECRET__FLEET_COMMUNITY={"type":"snmp-community","fields":{"community":"env-ro"}}`)
	outpost, drv, comp := "edge-env", "snmp-scalar", "cmp-env"
	if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: outpost}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: drv, Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatal(err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	spec := storage.EndpointSpec{Driver: &drv, Component: &comp, Outpost: &outpost,
		Inputs: map[string]string{"host": "10.20.4.40", "community": "fleet-community"}, SecretRead: all, CanAdmin: true}
	if _, err := gw.CreateEndpoint(ctx, "", spec, all); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Fatalf("attach without secret:bind = %v, want forbidden", err)
	}
	spec.SecretBind = all
	ep, err := gw.CreateEndpoint(ctx, "", spec, all)
	if err != nil {
		t.Fatalf("attach with secret:bind: %v", err)
	}

	resolved, err := gw.ResolveSecrets(ctx, c.ID, all, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Name != "fleet-community" || !resolved[0].Winner || resolved[0].OwnerKind != "platform" ||
		len(resolved[0].Fields) != 1 || resolved[0].Fields[0].Value != secret.Masked {
		t.Fatalf("cascade = %+v", resolved)
	}

	wl, err := gw.OutpostWorklist(ctx, outpost)
	if err != nil {
		t.Fatal(err)
	}
	delivered := false
	for _, task := range wl.Tasks {
		if task.EndpointID != ep.ID {
			continue
		}
		for _, fields := range task.Secrets {
			delivered = delivered || fields["community"] == "env-ro"
		}
		masked := false
		for _, m := range task.Mask {
			masked = masked || m == "env-ro"
		}
		if !masked {
			t.Errorf("task %s does not mask the env community: %v", task.ID, task.Mask)
		}
	}
	if !delivered {
		t.Fatalf("worklist did not deliver the env community: %+v", wl.Tasks)
	}
}
