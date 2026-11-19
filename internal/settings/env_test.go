package settings

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestLoadEnvReadsScalarKey covers the simple case: a variable naming a
// platform namespace and a scalar key lands as that key's value.
func TestLoadEnvReadsScalarKey(t *testing.T) {
	doc, err := LoadEnv([]string{
		"OMNIGLASS_SETTING__ASSISTANT__PROVIDER=openai",
		"OMNIGLASS_SETTING__ASSISTANT__MODEL=openai.gpt-oss-20b-1:0",
	})
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if doc["assistant"]["provider"] != "openai" {
		t.Errorf("provider = %v, want openai", doc["assistant"]["provider"])
	}
	if doc["assistant"]["model"] != "openai.gpt-oss-20b-1:0" {
		t.Errorf("model = %v, want openai.gpt-oss-20b-1:0", doc["assistant"]["model"])
	}
}

// TestLoadEnvKeepsUnderscoreKeysUnambiguous shows the double-underscore
// separator: the single underscore in key_secret stays part of the key name.
func TestLoadEnvKeepsUnderscoreKeysUnambiguous(t *testing.T) {
	doc, err := LoadEnv([]string{"OMNIGLASS_SETTING__ASSISTANT__KEY_SECRET=bedrock-key"})
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if got := doc["assistant"]["key_secret"]; got != "bedrock-key" {
		t.Errorf("assistant.key_secret = %v, want bedrock-key", got)
	}
}

// TestLoadEnvParsesScalarTypes proves the env value reaches the typed struct
// as the field's Go kind, not as a string: an int is an int, a bool is a
// bool.
func TestLoadEnvParsesScalarTypes(t *testing.T) {
	doc, err := LoadEnv([]string{
		"OMNIGLASS_SETTING__DATABASE__SLOW_QUERY_MS=750",
		"OMNIGLASS_SETTING__HEARTBEAT__INTERVAL_SECONDS=120",
	})
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if got := doc["database"]["slow_query_ms"]; got != 750 {
		t.Errorf("database.slow_query_ms = %T(%v), want int(750)", got, got)
	}
	if got := doc["heartbeat"]["interval_seconds"]; got != 120 {
		t.Errorf("heartbeat.interval_seconds = %T(%v), want int(120)", got, got)
	}
}

// TestLoadEnvParsesListOnCommas mirrors the default-tag convention: a list
// value splits on commas and coerces each element.
func TestLoadEnvParsesListOnCommas(t *testing.T) {
	doc, err := LoadEnv([]string{"OMNIGLASS_SETTING__LABEL__ACRONYMS=AV,DSP,HDMI"})
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	got, ok := doc["label"]["acronyms"].([]string)
	if !ok {
		t.Fatalf("acronyms = %T, want []string", doc["label"]["acronyms"])
	}
	want := []string{"AV", "DSP", "HDMI"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("acronyms = %v, want %v", got, want)
	}
}

// TestLoadEnvRejectsUnknownNamespace fails the boot on a variable naming a
// namespace the Settings struct does not declare.
func TestLoadEnvRejectsUnknownNamespace(t *testing.T) {
	_, err := LoadEnv([]string{"OMNIGLASS_SETTING__NOPE__THEME=omniglass-light"})
	if err == nil {
		t.Fatal("LoadEnv accepted an unknown namespace")
	}
	if !errors.Is(err, ErrEnvUnknownKey) {
		t.Errorf("err = %v, want wraps ErrEnvUnknownKey", err)
	}
	if !contains(err.Error(), "OMNIGLASS_SETTING__NOPE__THEME") {
		t.Errorf("err %q does not name the variable", err)
	}
}

// TestLoadEnvRejectsUnknownKey fails the boot on a namespace's missing key.
func TestLoadEnvRejectsUnknownKey(t *testing.T) {
	_, err := LoadEnv([]string{"OMNIGLASS_SETTING__ASSISTANT__TURTLES=yes"})
	if err == nil {
		t.Fatal("LoadEnv accepted an unknown key")
	}
	if !errors.Is(err, ErrEnvUnknownKey) {
		t.Errorf("err = %v, want wraps ErrEnvUnknownKey", err)
	}
	if !contains(err.Error(), "OMNIGLASS_SETTING__ASSISTANT__TURTLES") {
		t.Errorf("err %q does not name the variable", err)
	}
}

// TestLoadEnvRefusesProfileNamespace fails the boot when a variable names a
// profile-domain namespace (ui, keybindings): a profile key is per-person
// and has no meaning as a process-wide default.
func TestLoadEnvRefusesProfileNamespace(t *testing.T) {
	_, err := LoadEnv([]string{"OMNIGLASS_SETTING__UI__THEME=omniglass-light"})
	if err == nil {
		t.Fatal("LoadEnv accepted a profile-domain namespace")
	}
	if !errors.Is(err, ErrEnvProfileNamespace) {
		t.Errorf("err = %v, want wraps ErrEnvProfileNamespace", err)
	}
	if !contains(err.Error(), "OMNIGLASS_SETTING__UI__THEME") {
		t.Errorf("err %q does not name the variable", err)
	}
}

// TestLoadEnvRejectsBadValue surfaces a value that will not coerce: here an
// int field set to a non-numeric value.
func TestLoadEnvRejectsBadValue(t *testing.T) {
	_, err := LoadEnv([]string{"OMNIGLASS_SETTING__DATABASE__SLOW_QUERY_MS=soon"})
	if err == nil {
		t.Fatal("LoadEnv accepted a non-numeric int")
	}
	if !errors.Is(err, ErrEnvBadValue) {
		t.Errorf("err = %v, want wraps ErrEnvBadValue", err)
	}
	if !contains(err.Error(), "OMNIGLASS_SETTING__DATABASE__SLOW_QUERY_MS") {
		t.Errorf("err %q does not name the variable", err)
	}
	// The value must not appear in the error: it may be a bare secret name.
	if contains(err.Error(), "soon") {
		t.Errorf("err %q leaks the value; it must only name the variable", err)
	}
}

// TestLoadEnvValueNotQuotedForSecretVariable protects the spec invariant:
// the error names the variable, never its value.
func TestLoadEnvValueNotQuotedForSecretVariable(t *testing.T) {
	// base_url is a string, which never fails to coerce, so to exercise the
	// value-hiding rule we drive an int field with a bare secret-shaped
	// string.
	_, err := LoadEnv([]string{"OMNIGLASS_SETTING__ASSISTANT__CONVERSATION_DAYS=my-api-key-leak"})
	if err == nil {
		t.Fatal("LoadEnv accepted a non-numeric int")
	}
	if contains(err.Error(), "my-api-key-leak") {
		t.Errorf("err %q leaks the bare value", err)
	}
}

// TestLoadEnvIgnoresOtherVariables ignores anything without the prefix,
// including secret variables (handled separately).
func TestLoadEnvIgnoresOtherVariables(t *testing.T) {
	doc, err := LoadEnv([]string{
		"OMNIGLASS_DSN=postgres://x",
		"OMNIGLASS_SECRET__BEDROCK_KEY={}",
		"PATH=/usr/bin",
	})
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if len(doc) != 0 {
		t.Errorf("doc = %v, want empty", doc)
	}
}

// TestLoadEnvRejectsMalformedVariable names a shape that does not parse:
// the prefix with no separator, or a trailing separator.
func TestLoadEnvRejectsMalformedVariable(t *testing.T) {
	for _, name := range []string{
		"OMNIGLASS_SETTING__ASSISTANT",           // no key
		"OMNIGLASS_SETTING____MODEL",             // empty namespace
		"OMNIGLASS_SETTING__ASSISTANT__",         // empty key
		"OMNIGLASS_SETTING__ASSISTANT__MODEL__X", // too many segments
	} {
		_, err := LoadEnv([]string{name + "=x"})
		if err == nil {
			t.Errorf("%s: LoadEnv accepted", name)
			continue
		}
		if !contains(err.Error(), name) {
			t.Errorf("%s: err %q does not name the variable", name, err)
		}
	}
}

// TestLoadEnvIsLayerSource proves the parsed doc is a plain Doc, suitable to
// pass through Resolve as a Level: the resolver treats it like any other
// layer and reports its source as whatever name the caller gives it.
func TestLoadEnvIsLayerSource(t *testing.T) {
	envDoc, err := LoadEnv([]string{"OMNIGLASS_SETTING__ASSISTANT__MODEL=bedrock/foo"})
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	r := Resolve(
		Level{Name: "default", Doc: Doc{"assistant": {"model": "default"}}},
		Level{Name: "file", Doc: Doc{}},
		Level{Name: "env", Doc: envDoc},
	)
	if got := r.Values["assistant"]["model"]; got != "bedrock/foo" {
		t.Errorf("assistant.model = %v, want bedrock/foo", got)
	}
	if got := r.Sources["assistant.model"]; got != "env" {
		t.Errorf("source = %v, want env", got)
	}
}

func TestCoerceRefusesANonFiniteNumber(t *testing.T) {
	for _, v := range []string{"NaN", "Inf", "-Infinity"} {
		if _, err := coerceDefault(v, reflect.Float64); err == nil {
			t.Errorf("coerceDefault(%q, float64) = nil error, want a refusal", v)
		}
	}
}

// An env value meets the declared constraints a console patch meets: an
// enum value outside its list fails the boot naming the variable and never
// the value, and an in-list value still loads.
func TestLoadEnvHoldsTheDeclaredEnum(t *testing.T) {
	_, err := LoadEnv([]string{"OMNIGLASS_SETTING__NOTIFICATION__MAIL_SECURITY=sometimes"})
	if !errors.Is(err, ErrEnvBadValue) {
		t.Fatalf("err = %v, want ErrEnvBadValue", err)
	}
	if !contains(err.Error(), "OMNIGLASS_SETTING__NOTIFICATION__MAIL_SECURITY") || contains(err.Error(), "sometimes") {
		t.Errorf("err = %q, want the variable named and the value left out", err)
	}
	doc, err := LoadEnv([]string{"OMNIGLASS_SETTING__NOTIFICATION__MAIL_SECURITY=tls"})
	if err != nil {
		t.Fatalf("an in-list value: %v", err)
	}
	if got := doc["notification"]["mail_security"]; got != "tls" {
		t.Errorf("mail_security = %v, want tls", got)
	}
}

// The console still wins (#2223): the service's own level stack puts env under
// the platform binding, so a key set at both resolves to the binding, sourced
// "platform", and a key set only by env still reads from env.
func TestPlatformBindingBeatsEnv(t *testing.T) {
	envDoc, err := LoadEnv([]string{
		"OMNIGLASS_SETTING__ASSISTANT__MODEL=from-env",
		"OMNIGLASS_SETTING__ASSISTANT__BASE_URL=https://env.example/v1",
	})
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	svc := NewService(Doc{}, envDoc, func(context.Context, string) (Doc, map[string][]string, error) {
		return Doc{"assistant": {"model": "from-console"}}, nil, nil
	})
	r, err := svc.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, src := r.Values["assistant"]["model"], r.Sources["assistant.model"]; got != "from-console" || src != "platform" {
		t.Errorf("assistant.model = %v from %v, want from-console from platform", got, src)
	}
	if got, src := r.Values["assistant"]["base_url"], r.Sources["assistant.base_url"]; got != "https://env.example/v1" || src != "env" {
		t.Errorf("assistant.base_url = %v from %v, want the env value from env", got, src)
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || indexOfSub(s, sub) >= 0 }

func indexOfSub(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestEnvReferenceListsOnlyLoadableKeys pins the generated env reference to
// what LoadEnv accepts: a map key such as agent.roles is refused at boot, so
// it must not appear as a variable an operator could set.
func TestEnvReferenceListsOnlyLoadableKeys(t *testing.T) {
	facts := EnvSettingFacts()
	if len(facts) == 0 {
		t.Fatal("no env facts reflected")
	}
	for _, f := range facts {
		switch f.Key {
		case EnvPrefix + "AGENT" + EnvSep + "ROLES", EnvPrefix + "AGENT" + EnvSep + "TOOLS":
			t.Errorf("reference lists %s, which the env layer refuses", f.Key)
		}
	}
	fields := indexSettingFields()
	for _, f := range facts {
		rest := strings.TrimPrefix(f.Key, EnvPrefix)
		ns, key, ok := strings.Cut(rest, EnvSep)
		if !ok {
			t.Fatalf("fact %s has no separator", f.Key)
		}
		sf, ok := fields[strings.ToLower(ns)][strings.ToLower(key)]
		if !ok {
			t.Fatalf("fact %s names no field", f.Key)
		}
		if !envSettable(sf.Type) {
			t.Errorf("reference lists %s, a %s the env layer refuses", f.Key, sf.Type)
		}
	}
}
