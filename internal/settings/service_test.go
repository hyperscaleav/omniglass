package settings

import (
	"context"
	"testing"
)

func TestServiceResolvesFileOverDefaultAndPlatformOverFile(t *testing.T) {
	file := Doc{"ui": {"theme": "omniglass-light"}}
	bindings := func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
		return Doc{"ui": {"default_landing": "/alarms"}}, nil, nil
	}
	svc := NewService(file, nil, bindings)
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

// TestServiceNamesLevelsDefaultFileEnvPlatform asserts the engine constructs the
// named levels, in broad-to-specific order. Each level contributes a key nothing
// more specific touches, so the reported source names the level that fed it: the
// off-axis declaration is "default", the operator file is "file", the process
// environment is "env" (#2223), and the DB binding is "platform".
func TestServiceNamesLevelsDefaultFileEnvPlatform(t *testing.T) {
	file := Doc{"ui": {"default_landing": "/alarms"}}
	bindings := func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
		return Doc{"ui": {"theme": "omniglass-light"}}, nil, nil
	}
	svc := NewService(file, Doc{"assistant": {"model": "from-env"}}, bindings)
	r, err := svc.Resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// keybindings.open_edit is declared by the type and set by nobody, so it is the
	// off-axis default; the other three are set at the three real rungs.
	for _, tc := range []struct{ path, want string }{
		{"keybindings.open_edit", "default"},
		{"ui.default_landing", "file"},
		{"assistant.model", "env"},
		{"ui.theme", "platform"},
	} {
		if got := r.Sources[tc.path]; got != tc.want {
			t.Fatalf("%s source = %q, want %q (full: %v)", tc.path, got, tc.want, r.Sources)
		}
	}
}

// TestServiceReadsBindingsAtPlatformScope pins the scope the engine asks the
// Gateway for. The migration moved every binding row to scope 'platform'; an
// engine still asking for 'global' would silently resolve to defaults and orphan
// every operator binding.
func TestServiceReadsBindingsAtPlatformScope(t *testing.T) {
	var got []string
	svc := NewService(Doc{}, nil, func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
		got = append(got, scope)
		return Doc{}, nil, nil
	})
	if _, err := svc.Resolve(context.Background()); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0] != "platform" {
		t.Fatalf("binding scopes read = %v, want [platform]", got)
	}
}

func TestClientEffectiveKeepsOnlyVisibleNamespaces(t *testing.T) {
	// keybindings and ui are both client-visible in slice-0; assert they survive.
	svc := NewService(Doc{}, nil, func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
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

// TestClientEffectiveForUserShadowsProfileOnly pins the user level (#2450): a
// person's binding shadows the platform on a profile-domain key, a platform
// lock still wins over it, and a user doc naming a platform-domain namespace
// contributes nothing (the domain says who writes it).
func TestClientEffectiveForUserShadowsProfileOnly(t *testing.T) {
	svc := NewService(Doc{}, nil, func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
		return Doc{"ui": {"default_landing": "/alarms"}, "keybindings": {"open_edit": "x"}},
			map[string][]string{"keybindings": {"open_edit"}}, nil
	})
	user := Doc{
		"ui":          {"theme": "omniglass-light"},
		"keybindings": {"open_edit": "y"},
		"learn":       {"done": []any{"device-to-samples"}},
		"label":       {"acronyms": []any{"XYZ"}},
	}
	eff, err := svc.ClientEffectiveFor(context.Background(), user)
	if err != nil {
		t.Fatalf("client effective for: %v", err)
	}
	if eff["ui"]["theme"] != "omniglass-light" || eff["ui"]["default_landing"] != "/alarms" {
		t.Fatalf("ui = %v, want the user's theme over the platform's landing", eff["ui"])
	}
	if eff["keybindings"]["open_edit"] != "x" {
		t.Fatalf("open_edit = %v, want the platform-locked x", eff["keybindings"]["open_edit"])
	}
	if done, _ := eff["learn"]["done"].([]any); len(done) != 1 {
		t.Fatalf("learn.done = %v, want the user's one lesson", eff["learn"]["done"])
	}
	if acr, _ := eff["label"]["acronyms"].([]any); len(acr) == 1 {
		t.Fatalf("label.acronyms = %v: a user binding on a platform-domain namespace must not apply", acr)
	}
}
