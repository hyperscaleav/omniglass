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
	"strings"

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
			v, err := fieldDefault(tag, f.Type)
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

// fieldDefault coerces a default tag into the field's TYPE. A list splits into
// elements and coerces each one; anything else is a single scalar and only its
// kind matters.
//
// The type is needed where the kind is not enough: a []string and a []int are
// both reflect.Slice, and the element type is the only thing that says which.
func fieldDefault(s string, t reflect.Type) (any, error) {
	if t.Kind() == reflect.Slice {
		return listDefault(s, t)
	}
	return coerceDefault(s, t.Kind())
}

// listDefault parses a comma-separated default tag into a typed slice.
//
// The comma form is not a choice made here, it is huma's: huma reflects the same
// tag into the JSON Schema, splitting a string array on commas and trimming each
// element. This layer follows it exactly so a list-valued setting's declared
// default and its published schema default are one value rather than two
// spellings of one fact that can disagree.
//
// Huma also accepts a JSON array when the tag starts with '['. This layer
// deliberately does not, and refuses it rather than guessing: a tag that parsed
// one way into the schema and another way into the merge layer would be exactly
// the drift the single-declaration struct exists to prevent, and the failure is
// a bad tag on a struct field, which is a programming defect Defaults() already
// panics on.
//
// An empty tag is an empty list, never a nil one: a nil slice marshals as JSON
// null, and null is the merge patch's delete sentinel.
func listDefault(s string, t reflect.Type) (any, error) {
	if strings.HasPrefix(s, "[") {
		return nil, fmt.Errorf("a list default is comma-separated (a,b,c), not a JSON array: %q", s)
	}
	out := reflect.MakeSlice(t, 0, 0)
	if s == "" {
		return out.Interface(), nil
	}
	for _, part := range strings.Split(s, ",") {
		v, err := coerceDefault(strings.TrimSpace(part), t.Elem().Kind())
		if err != nil {
			return nil, err
		}
		rv := reflect.ValueOf(v)
		if !rv.Type().AssignableTo(t.Elem()) {
			return nil, fmt.Errorf("element %q is a %s, not a %s", part, rv.Type(), t.Elem())
		}
		out = reflect.Append(out, rv)
	}
	return out.Interface(), nil
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
