// Package settings is the pure resolution engine of the settings store. It deep
// merges ordered layers (embedded code defaults, an operator file, the DB
// override) into an effective document, tracks per-key provenance, and enforces
// locks. It performs no I/O beyond reading the operator file: the DB layer is
// supplied by the caller (the Storage Gateway). This is the primitive; the API and
// the SPA consume it.
package settings

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

//go:embed defaults.yaml
var defaultsYAML []byte

// Doc is a settings document: namespace to key to value. Values stay as decoded
// generic types so merge is presence-based (a key present overrides; a key absent
// inherits the lower layer).
type Doc = map[string]map[string]any

// Namespace declares a settings namespace and its policy. Domain is "profile" (it
// cascades to groups and users and is client-visible) or "platform" (global-only,
// admin-only-read). ClientVisible is true when any authenticated user may read the
// effective values (fed to the SPA via /settings/me).
type Namespace struct {
	Name          string
	Domain        string
	ClientVisible bool
}

// Namespaces is the slice-0 registry. Platform-domain namespaces (retention,
// integrations) land with their features.
func Namespaces() []Namespace {
	return []Namespace{
		{Name: "ui", Domain: "profile", ClientVisible: true},
		{Name: "keybindings", Domain: "profile", ClientVisible: true},
	}
}

// ClientVisibleNamespaces indexes the registry by client-visibility.
func ClientVisibleNamespaces() map[string]bool {
	out := map[string]bool{}
	for _, n := range Namespaces() {
		out[n.Name] = n.ClientVisible
	}
	return out
}

// Defaults returns the parsed embedded code-default document. It panics on a
// malformed embed: defaults.yaml is a compile-time asset, so a parse failure is a
// build defect, not a runtime condition.
func Defaults() Doc {
	var d Doc
	if err := yaml.Unmarshal(defaultsYAML, &d); err != nil {
		panic(fmt.Sprintf("settings: parse embedded defaults: %v", err))
	}
	return d
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
