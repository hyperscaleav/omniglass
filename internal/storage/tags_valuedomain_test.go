package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestTagAllowedValuesEnforced covers the enum value domain: a key with a
// non-empty allowed set admits only members; a free-text key admits anything.
func TestTagAllowedValuesEnforced(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	seedTree(t, gw)

	// An enum key and a free-text key, both universal.
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "environment", AllowedValues: []string{"prod", "staging", "dev"}}, all); err != nil {
		t.Fatalf("create enum key: %v", err)
	}
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "note"}, all); err != nil {
		t.Fatalf("create free key: %v", err)
	}
	// A duplicate allowed value is refused at create.
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "bad", AllowedValues: []string{"x", "x"}}, all); !errors.Is(err, storage.ErrTagValueInvalid) {
		t.Errorf("dup allowed value = %v, want ErrTagValueInvalid", err)
	}

	// A member binds; a non-member is refused.
	if _, err := gw.SetTagBinding(ctx, "", "environment", "component", strptr("codec-1"), "prod", all, all); err != nil {
		t.Fatalf("bind allowed value: %v", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "environment", "component", strptr("codec-1"), "qa", all, all); !errors.Is(err, storage.ErrTagValueNotAllowed) {
		t.Errorf("bind non-member = %v, want ErrTagValueNotAllowed", err)
	}
	// A free-text key admits any value.
	if _, err := gw.SetTagBinding(ctx, "", "note", "component", strptr("codec-1"), "anything at all", all, all); err != nil {
		t.Errorf("free key bind = %v, want ok", err)
	}

	// The allowed set round-trips on the key.
	tags, _ := gw.ListTags(ctx)
	for _, tg := range tags {
		if tg.Name == "environment" && (len(tg.AllowedValues) != 3 || tg.AllowedValues[0] != "prod") {
			t.Errorf("environment allowed_values = %v, want [prod staging dev]", tg.AllowedValues)
		}
		if tg.Name == "note" && len(tg.AllowedValues) != 0 {
			t.Errorf("note allowed_values = %v, want empty (free text)", tg.AllowedValues)
		}
	}

	// Narrowing the enum by update is enforced on the next bind.
	if _, err := gw.UpdateTag(ctx, "", "environment", storage.TagSpec{AllowedValues: []string{"prod"}}, all); err != nil {
		t.Fatalf("narrow enum: %v", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "environment", "component", strptr("codec-1"), "dev", all, all); !errors.Is(err, storage.ErrTagValueNotAllowed) {
		t.Errorf("bind after narrowing = %v, want ErrTagValueNotAllowed", err)
	}
}

// TestDistinctTagValues covers the free-key value autocomplete source: the
// distinct values bound for a key, ordered, and the unknown-key not-found.
func TestDistinctTagValues(t *testing.T) {
	gw := tagGateway(t)
	ctx := context.Background()
	comp := seedTree(t, gw)
	mustTag(t, gw, "environment", nil, true)

	mustBind(t, gw, "environment", "platform", nil, "prod")
	mustBind(t, gw, "environment", "location", strptr("campus"), "staging")
	mustBind(t, gw, "environment", "component", strptr("codec-1"), "prod") // duplicate value, collapses
	_ = comp

	got, err := gw.DistinctTagValues(ctx, "environment")
	if err != nil {
		t.Fatalf("distinct: %v", err)
	}
	if len(got) != 2 || got[0] != "prod" || got[1] != "staging" {
		t.Errorf("distinct = %v, want [prod staging] (deduped, sorted)", got)
	}
	if _, err := gw.DistinctTagValues(ctx, "ghost"); !errors.Is(err, storage.ErrTagNotFound) {
		t.Errorf("unknown key = %v, want ErrTagNotFound", err)
	}
}
