package settings

import (
	"context"
	"testing"
)

func TestServiceResolvesFileOverDefaultAndPlatformOverFile(t *testing.T) {
	file := Doc{"ui": {"theme": "omniglass-light"}}
	overrides := func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
		return Doc{"ui": {"default_landing": "/alarms"}}, nil, nil
	}
	svc := NewService(file, overrides)
	eff, err := svc.Effective(context.Background())
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if eff["ui"]["theme"] != "omniglass-light" {
		t.Fatalf("theme = %v, want file value omniglass-light", eff["ui"]["theme"])
	}
	if eff["ui"]["default_landing"] != "/alarms" {
		t.Fatalf("landing = %v, want platform value /alarms", eff["ui"]["default_landing"])
	}
}

// TestServiceNamesLevelsDefaultFilePlatform asserts the engine constructs the
// renamed levels, in broad-to-specific order. Each level contributes a key nothing
// more specific touches, so the reported source names the level that fed it: the
// off-axis declaration is "default", the operator file is "file", and the DB
// override is "platform".
func TestServiceNamesLevelsDefaultFilePlatform(t *testing.T) {
	file := Doc{"ui": {"default_landing": "/alarms"}}
	overrides := func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
		return Doc{"ui": {"theme": "omniglass-light"}}, nil, nil
	}
	svc := NewService(file, overrides)
	r, err := svc.Resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// keybindings.open_edit is declared by the type and set by nobody, so it is the
	// off-axis default; the other two are bindings at the two real rungs.
	for _, tc := range []struct{ path, want string }{
		{"keybindings.open_edit", "default"},
		{"ui.default_landing", "file"},
		{"ui.theme", "platform"},
	} {
		if got := r.Sources[tc.path]; got != tc.want {
			t.Fatalf("%s source = %q, want %q (full: %v)", tc.path, got, tc.want, r.Sources)
		}
	}
}

// TestServiceReadsOverridesAtPlatformScope pins the scope the engine asks the
// Gateway for. The migration moved every override row to scope 'platform'; an
// engine still asking for 'global' would silently resolve to defaults and orphan
// every operator override.
func TestServiceReadsOverridesAtPlatformScope(t *testing.T) {
	var got []string
	svc := NewService(Doc{}, func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
		got = append(got, scope)
		return Doc{}, nil, nil
	})
	if _, err := svc.Resolve(context.Background()); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0] != "platform" {
		t.Fatalf("override scopes read = %v, want [platform]", got)
	}
}

func TestClientEffectiveKeepsOnlyVisibleNamespaces(t *testing.T) {
	// keybindings and ui are both client-visible in slice-0; assert they survive.
	svc := NewService(Doc{}, func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
		return Doc{}, nil, nil
	})
	eff, err := svc.ClientEffective(context.Background())
	if err != nil {
		t.Fatalf("client effective: %v", err)
	}
	if _, ok := eff["ui"]; !ok {
		t.Fatalf("client effective missing ui")
	}
}
