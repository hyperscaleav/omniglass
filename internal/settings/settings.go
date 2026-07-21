// Package settings is the pure resolution engine of the settings store. It deep
// merges ordered layers (the type's declared defaults, an operator file, the
// platform DB override) into an effective document, tracks per-key provenance,
// and enforces locks. It performs no I/O beyond reading the operator file: the DB
// layer is supplied by the caller (the Storage Gateway). This is the primitive; the
// API and the SPA consume it.
package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Doc is a settings document: namespace to key to value. Values stay as decoded
// generic types so merge is presence-based (a key present overrides; a key absent
// inherits the lower layer).
type Doc = map[string]map[string]any

// Namespace declares a settings namespace and its policy. Domain is "profile" (it
// cascades to groups and users and is client-visible) or "platform" (platform-only,
// admin-only-read). ClientVisible is true when any authenticated user may read the
// effective values (fed to the SPA via /settings/me).
type Namespace struct {
	Name          string
	Domain        string
	ClientVisible bool
}

// Namespaces reflects the namespace registry from the Settings top-level fields:
// the json tag names the namespace, the settings tag carries the domain and
// client-visibility. Platform-domain namespaces (retention, integrations) land as
// fields with their features.
func Namespaces() []Namespace {
	var out []Namespace
	t := reflect.TypeOf(Settings{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		ns := Namespace{Name: jsonName(f)}
		for _, tok := range splitComma(f.Tag.Get("settings")) {
			switch tok {
			case "profile", "platform":
				ns.Domain = tok
			case "client":
				ns.ClientVisible = true
			}
		}
		out = append(out, ns)
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// ClientVisibleNamespaces indexes the registry by client-visibility.
func ClientVisibleNamespaces() map[string]bool {
	out := map[string]bool{}
	for _, n := range Namespaces() {
		out[n.Name] = n.ClientVisible
	}
	return out
}

// Defaults reflects the off-axis default layer from the Settings struct: each leaf's
// default tag, coerced to the field's Go kind. This is the single declaration point
// for a setting's default. A field with no default tag contributes nothing.
func Defaults() Doc {
	d := Doc{}
	t := reflect.TypeOf(Settings{})
	for i := 0; i < t.NumField(); i++ {
		nsField := t.Field(i)
		nsType := nsField.Type
		m := map[string]any{}
		for j := 0; j < nsType.NumField(); j++ {
			f := nsType.Field(j)
			tag, ok := f.Tag.Lookup("default")
			if !ok {
				continue
			}
			v, err := coerceDefault(tag, f.Type.Kind())
			if err != nil {
				panic("settings: bad default tag on " + nsType.Name() + "." + f.Name + ": " + err.Error())
			}
			m[jsonName(f)] = v
		}
		if len(m) > 0 {
			d[jsonName(nsField)] = m
		}
	}
	return d
}

// coerceDefault parses a default tag string into the field's Go kind, so the code
// layer holds typed values (an int default merges and reads as an int, not a
// string).
func coerceDefault(s string, k reflect.Kind) (any, error) {
	switch k {
	case reflect.String:
		return s, nil
	case reflect.Bool:
		return strconv.ParseBool(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		switch k {
		case reflect.Int:
			return int(i), nil
		case reflect.Int8:
			return int8(i), nil
		case reflect.Int16:
			return int16(i), nil
		case reflect.Int32:
			return int32(i), nil
		default:
			return i, nil
		}
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		if k == reflect.Float32 {
			return float32(f), nil
		}
		return f, nil
	default:
		return s, nil
	}
}

// LoadFile reads and parses an operator settings file (JSON or YAML; YAML is a
// superset, so one parser covers both). A missing path is not an error: many
// deployments (a laptop) have no file, and the base layer is then just the code
// defaults. A present-but-malformed file is an error the caller surfaces at boot.
func LoadFile(path string) (Doc, error) {
	if path == "" {
		return Doc{}, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Doc{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("settings: read file %q: %w", path, err)
	}
	var d Doc
	if err := yaml.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("settings: parse file %q: %w", path, err)
	}
	if d == nil {
		d = Doc{}
	}
	return d, nil
}
