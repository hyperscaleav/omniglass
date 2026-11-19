package storage_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// omnibot's model key (#2109) is bound as the mail relay's secret is: a
// platform api-key secret by bare name, judged on the resulting settings, and
// moving base_url while a key is bound takes secret:bind on it, since every
// run hands the key to whatever base_url names.
func TestTheModelKeyIsBoundLikeAnyCredential(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t),
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x9}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	for _, s := range []storage.SecretSpec{
		{Name: "models-key", SecretType: "api-key", OwnerKind: "platform", Fields: map[string]string{"key": "ghp-real"}},
		{Name: "relay-login", SecretType: "basic-auth", OwnerKind: "platform", Fields: map[string]string{"username": "u", "password": "p"}},
	} {
		if _, err := gw.CreateSecret(ctx, "", s, all, true); err != nil {
			t.Fatal(err)
		}
	}
	noBind := storage.SecretUse{Read: all}
	bind := storage.SecretUse{Read: all, Bind: all, CanAdmin: true}

	m, err := gw.OmnibotModel(ctx)
	if err != nil || m != nil {
		t.Fatalf("no provider configured = %+v, %v, want none", m, err)
	}
	if _, err := gw.MergePatchAssistantSettings(ctx, "", map[string]any{"provider": "openai", "key_secret": "relay-login"}, bind); !errors.Is(err, storage.ErrModelKeyInvalid) {
		t.Fatalf("a basic-auth secret as the key = %v, want invalid", err)
	}
	if _, err := gw.MergePatchAssistantSettings(ctx, "", map[string]any{"provider": "openai", "key_secret": "models-key"}, noBind); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Fatalf("binding the key without secret:bind = %v, want forbidden", err)
	}
	if _, err := gw.MergePatchAssistantSettings(ctx, "", map[string]any{"provider": "openai", "key_secret": "models-key"}, bind); err != nil {
		t.Fatalf("binding the key = %v", err)
	}
	if _, err := gw.MergePatchAssistantSettings(ctx, "", map[string]any{"base_url": "https://evil.example.com/v1"}, noBind); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Fatalf("moving base_url under the bound key without secret:bind = %v, want forbidden", err)
	}
	if _, err := gw.MergePatchAssistantSettings(ctx, "", map[string]any{"model": "openai/gpt-4.1"}, noBind); err != nil {
		t.Fatalf("changing only the model = %v, want no bind needed", err)
	}
	m, err = gw.OmnibotModel(ctx)
	if err != nil || m == nil || m.Provider != "openai" || m.Model != "openai/gpt-4.1" || m.BaseURL != "https://openrouter.ai/api/v1" || m.Key != "ghp-real" {
		t.Fatalf("the configured model = %+v, %v", m, err)
	}
	if err := gw.DeleteSecret(ctx, "", "models-key", all, all, true, true); err != nil {
		t.Fatal(err)
	}
	if m, err := gw.OmnibotModel(ctx); !errors.Is(err, storage.ErrModelKeyInvalid) || m != nil {
		t.Fatalf("a configured provider whose key is gone = %+v, %v, want the reason", m, err)
	}
}
