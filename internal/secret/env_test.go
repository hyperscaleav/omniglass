package secret_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/secret"
)

const bedrock = `{"type":"api-key","fields":{"key":"sk-very-secret"}}`

func TestParseEnvDeclaresASecretPerVariable(t *testing.T) {
	got, err := secret.ParseEnv([]string{
		"PATH=/bin",
		"OMNIGLASS_SECRET__BEDROCK_KEY=" + bedrock,
		`OMNIGLASS_SECRET__RELAY={"type":"basic-auth","fields":{"username":"u","password":"p"}}`,
		"OMNIGLASS_SETTING__ASSISTANT__MODEL=x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d secrets, want 2: %+v", len(got), got)
	}
	if got[0].Name != "bedrock-key" || got[0].Var != "OMNIGLASS_SECRET__BEDROCK_KEY" || got[0].Type != "api-key" || got[0].Fields["key"] != "sk-very-secret" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Name != "relay" || got[1].Fields["username"] != "u" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestParseEnvRefusesNamingTheVariableNeverTheValue(t *testing.T) {
	cases := map[string]string{
		"not json":       "OMNIGLASS_SECRET__K=sk-very-secret",
		"trailing":       "OMNIGLASS_SECRET__K=" + bedrock + " sk-very-secret",
		"unknown member": `OMNIGLASS_SECRET__K={"type":"api-key","fields":{"key":"sk-very-secret"},"extra":"sk-very-secret"}`,
		"no type":        `OMNIGLASS_SECRET__K={"fields":{"key":"sk-very-secret"}}`,
		"number field":   `OMNIGLASS_SECRET__K={"type":"api-key","fields":{"key":12}}`,
		"bad name":       "OMNIGLASS_SECRET__K__X=" + bedrock,
		"empty name":     "OMNIGLASS_SECRET__=" + bedrock,
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := secret.ParseEnv([]string{entry})
			if !errors.Is(err, secret.ErrEnvSecret) {
				t.Fatalf("err = %v, want ErrEnvSecret", err)
			}
			if strings.Contains(err.Error(), "sk-very-secret") {
				t.Fatalf("error quotes the value: %v", err)
			}
			if !strings.Contains(err.Error(), "OMNIGLASS_SECRET__") {
				t.Fatalf("error does not name the variable: %v", err)
			}
		})
	}
}

func TestParseEnvRefusesTwoVariablesForOneName(t *testing.T) {
	_, err := secret.ParseEnv([]string{"OMNIGLASS_SECRET__A_B=" + bedrock, "OMNIGLASS_SECRET__a_b=" + bedrock})
	if !errors.Is(err, secret.ErrEnvSecret) || !strings.Contains(err.Error(), `"a-b"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnvVarInvertsTheName(t *testing.T) {
	if got := secret.EnvVar("bedrock-key"); got != "OMNIGLASS_SECRET__BEDROCK_KEY" {
		t.Fatalf("EnvVar = %q", got)
	}
}

func TestEnvSecretCheckHoldsTheShape(t *testing.T) {
	apiKey := secret.Shape{Name: "api-key", Fields: []secret.Field{{Name: "key", Secret: true, Origin: secret.OriginOperator}}}
	minted := secret.Shape{Name: "webhook-token", Fields: []secret.Field{{Name: "token", Secret: true, Origin: secret.OriginLifecycle}}}
	ok := secret.EnvSecret{Name: "k", Var: "OMNIGLASS_SECRET__K", Type: "api-key", Fields: map[string]string{"key": "sk-very-secret"}}
	if err := ok.Check(apiKey); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		e     secret.EnvSecret
		shape secret.Shape
	}{
		"missing": {secret.EnvSecret{Var: "OMNIGLASS_SECRET__K", Fields: map[string]string{}}, apiKey},
		"unknown": {secret.EnvSecret{Var: "OMNIGLASS_SECRET__K", Fields: map[string]string{"key": "sk-very-secret", "user": "sk-very-secret"}}, apiKey},
		"minted":  {secret.EnvSecret{Var: "OMNIGLASS_SECRET__K", Fields: map[string]string{"token": "sk-very-secret"}}, minted},
	} {
		t.Run(name, func(t *testing.T) {
			err := c.e.Check(c.shape)
			if !errors.Is(err, secret.ErrEnvSecret) || !strings.Contains(err.Error(), "OMNIGLASS_SECRET__K") || strings.Contains(err.Error(), "sk-very-secret") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestEnvSecretVersionMovesWithTheValueAndKey(t *testing.T) {
	a := secret.EnvSecret{Name: "k", Type: "api-key", Fields: map[string]string{"key": "one"}}
	b := secret.EnvSecret{Name: "k", Type: "api-key", Fields: map[string]string{"key": "two"}}
	key := []byte("key-1")
	if a.Version(key) != a.Version(key) {
		t.Fatal("version is not stable")
	}
	if a.Version(key) == b.Version(key) {
		t.Fatal("a rotated value keeps its version")
	}
	if a.Version(key) == a.Version([]byte("key-2")) {
		t.Fatal("the version does not depend on the key")
	}
	if strings.Contains(a.Version(key), "one") {
		t.Fatal("version carries the value")
	}
}

func TestStaticProviderVersionKeyIsStableAndNotTheKEK(t *testing.T) {
	kek := make([]byte, 32)
	p := secret.NewStaticProvider(kek)
	if string(p.VersionKey()) != string(secret.NewStaticProvider(kek).VersionKey()) {
		t.Fatal("version key differs for one KEK")
	}
	if string(p.VersionKey()) == string(kek) {
		t.Fatal("version key is the KEK")
	}
}
