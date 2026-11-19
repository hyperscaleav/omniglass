package settings

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// EnvPrefix is the prefix every env-set setting variable carries. The two
// underscores after SETTING are the namespace delimiter; the two underscores
// between namespace and key keep an identifier with its own underscore
// (key_secret) unambiguous. The convention is the ASP.NET / Dynaconf one,
// chosen so a Helm chart's extraEnv list can supply one key per entry.
const EnvPrefix = "OMNIGLASS_SETTING__"

// EnvSep separates path segments inside an env name. Doubled, so an identifier
// may itself contain a single underscore.
const EnvSep = "__"

// ErrEnvUnknownKey, ErrEnvProfileNamespace and ErrEnvBadValue are the
// classified boot failures LoadEnv returns, wrapped by the full message the
// server prints. A variable name is always in the message; a value never is
// (it may be a secret bare name or a URL that should not reach the log).
var (
	ErrEnvUnknownKey        = errors.New("names no setting")
	ErrEnvProfileNamespace  = errors.New("profile-domain namespaces cannot be set by environment")
	ErrEnvBadValue          = errors.New("value does not parse")
	ErrEnvMalformedVariable = errors.New("variable name does not parse")
)

// LoadEnv parses the env settings layer out of a process environment. Each
// recognized variable (OMNIGLASS_SETTING__<NAMESPACE>__<KEY>=<value>) sets one
// leaf of the Settings struct: the namespace lowercases to its json name, the
// key likewise, and the value coerces through the key's reflected Go type
// (string as-is, bool/number parsed, list split on commas as the default tag
// is). A variable that names a profile-domain namespace, an unknown key, or a
// value that will not parse fails with the variable's NAME; the value is
// never quoted because it may be a bare secret name or a URL.
//
// env is a list of KEY=VALUE strings, as os.Environ returns. Variables that do
// not start with the prefix are ignored.
func LoadEnv(env []string) (Doc, error) {
	namespaces := indexNamespaces()
	fieldsByNS := indexSettingFields()
	out := Doc{}
	for _, entry := range env {
		name, value, ok := splitEnv(entry)
		if !ok || !strings.HasPrefix(name, EnvPrefix) {
			continue
		}
		tail := strings.TrimPrefix(name, EnvPrefix)
		// Exactly one double-underscore separates the namespace from the key.
		// A name with no separator, or more than two segments, is ill-formed.
		idx := strings.Index(tail, EnvSep)
		if idx < 0 || idx == 0 || idx == len(tail)-len(EnvSep) {
			return nil, fmt.Errorf("settings: %s: %w (expected %s<NAMESPACE>__<KEY>)", name, ErrEnvMalformedVariable, EnvPrefix)
		}
		ns := strings.ToLower(tail[:idx])
		key := strings.ToLower(tail[idx+len(EnvSep):])
		if strings.Contains(key, EnvSep) {
			return nil, fmt.Errorf("settings: %s: %w (key contains %q; a nested key is not a setting path)", name, ErrEnvMalformedVariable, EnvSep)
		}
		nsDecl, ok := namespaces[ns]
		if !ok {
			return nil, fmt.Errorf("settings: %s: %w (unknown namespace %q)", name, ErrEnvUnknownKey, ns)
		}
		if nsDecl.Domain != "platform" {
			return nil, fmt.Errorf("settings: %s: %w (%q is a %s-domain namespace)", name, ErrEnvProfileNamespace, ns, nsDecl.Domain)
		}
		field, ok := fieldsByNS[ns][key]
		if !ok {
			return nil, fmt.Errorf("settings: %s: %w (%q has no key %q)", name, ErrEnvUnknownKey, ns, key)
		}
		coerced, err := coerceEnvValue(value, field.Type)
		if err != nil {
			return nil, fmt.Errorf("settings: %s: %w (%s)", name, ErrEnvBadValue, err)
		}
		if out[ns] == nil {
			out[ns] = map[string]any{}
		}
		out[ns][key] = coerced
	}
	return out, nil
}

// splitEnv is os.LookupEnv's split applied to an Environ entry: the KEY=VALUE
// pair, or false if the entry has no equals (which the C runtime does not
// emit but Go's testing shim can).
func splitEnv(entry string) (string, string, bool) {
	i := strings.IndexByte(entry, '=')
	if i < 0 {
		return "", "", false
	}
	return entry[:i], entry[i+1:], true
}

// indexNamespaces is Namespaces() keyed by the lowercase json name, so a
// variable's lower-cased segment looks up the declared namespace directly.
func indexNamespaces() map[string]Namespace {
	out := map[string]Namespace{}
	for _, ns := range Namespaces() {
		out[ns.Name] = ns
	}
	return out
}

// indexSettingFields indexes each platform-domain namespace's leaf fields by
// their json name, so a variable's key segment looks up the struct field
// whose type and parser it coerces through. Profile-domain namespaces are
// indexed too so a profile-namespace variable reports a specific message
// rather than an "unknown namespace" one.
func indexSettingFields() map[string]map[string]reflect.StructField {
	out := map[string]map[string]reflect.StructField{}
	t := reflect.TypeOf(Settings{})
	for i := 0; i < t.NumField(); i++ {
		nsField := t.Field(i)
		nsName := jsonName(nsField)
		fields := map[string]reflect.StructField{}
		nsType := nsField.Type
		for j := 0; j < nsType.NumField(); j++ {
			f := nsType.Field(j)
			fields[jsonName(f)] = f
		}
		out[nsName] = fields
	}
	return out
}

// EnvSettingFact is one settable key rendered into the env-var reference
// (#2223): the variable name an operator sets, its one-line doc (the key's
// doc tag on Settings), and the default when the key has one. Scope is
// always the server and family is always OMNIGLASS_SETTING__; the generator
// fills those in. Profile-domain keys are excluded, since a profile key is
// per-person and has no meaning as a process environment.
type EnvSettingFact struct {
	Key     string
	Default string
	Doc     string
}

// EnvSettingFacts reflects one fact per settable key off the Settings struct,
// so the env-var reference carries one row per platform-domain leaf. The
// variable name is the struct's shape translated into the double-underscore
// convention: namespace json name, then the key's. Walked in struct order so
// the reference reads namespace by namespace.
func EnvSettingFacts() []EnvSettingFact {
	var out []EnvSettingFact
	t := reflect.TypeOf(Settings{})
	for i := 0; i < t.NumField(); i++ {
		nsField := t.Field(i)
		ns := jsonName(nsField)
		domain := namespaceDomain(nsField.Tag.Get("settings"))
		if domain != "platform" {
			continue
		}
		nsType := nsField.Type
		for j := 0; j < nsType.NumField(); j++ {
			f := nsType.Field(j)
			key := jsonName(f)
			name := EnvPrefix + strings.ToUpper(ns) + EnvSep + strings.ToUpper(key)
			out = append(out, EnvSettingFact{
				Key:     name,
				Default: f.Tag.Get("default"),
				Doc:     f.Tag.Get("doc"),
			})
		}
	}
	return out
}

// namespaceDomain parses the settings tag ("profile" or "platform", with
// optional ",client") to just its domain token.
func namespaceDomain(tag string) string {
	for _, tok := range strings.Split(tag, ",") {
		switch tok {
		case "platform", "profile":
			return tok
		}
	}
	return ""
}

// coerceEnvValue parses a raw env value into the target's Go kind. A list
// (reflect.Slice) splits on commas and coerces each element, matching the way
// a default tag parses so the two agree: a variable that overrides a list
// default has the same shape as the default itself. A map, struct or other
// non-scalar is refused: the env layer is a flat per-key interface and a
// variable that would have to parse structured JSON belongs in the file
// layer or the DB.
//
// Every error message names ONLY the expected kind, never the input value:
// an env value may be a bare secret name, a URL, or any other string an
// operator would rather not see echoed back in a log line. The caller
// (LoadEnv) composes this message with the variable's name.
func coerceEnvValue(s string, t reflect.Type) (any, error) {
	switch t.Kind() {
	case reflect.Slice:
		v, err := listDefault(s, t)
		if err != nil {
			return nil, fmt.Errorf("expected a comma-separated list of %s", t.Elem().Kind())
		}
		return v, nil
	case reflect.Map, reflect.Struct, reflect.Interface, reflect.Ptr:
		return nil, fmt.Errorf("%s is not a scalar or list; set it in the file layer or the platform binding", t)
	}
	v, err := coerceDefault(s, t.Kind())
	if err != nil {
		return nil, fmt.Errorf("expected a %s", t.Kind())
	}
	return v, nil
}
