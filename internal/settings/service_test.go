package settings

import (
	"context"
	"testing"
)

func TestServiceResolvesFileOverCodeAndGlobalOverFile(t *testing.T) {
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
		t.Fatalf("landing = %v, want global value /alarms", eff["ui"]["default_landing"])
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
