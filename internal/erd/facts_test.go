package erd

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestIntrospectColumnFacts pins the column-level detail the generated facts
// artifact claims (nullability, defaults, CHECK constraints, unique indexes)
// against the real migrated schema, using the tag pair as ground truth: tag.name
// is NOT NULL with no default, tag.propagates defaults true, and tag_binding
// carries the owner-kind CHECK and the platform partial unique index.
func TestIntrospectColumnFacts(t *testing.T) {
	dsn := storagetest.NewDSN(t) // skips under -short
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	s, err := Introspect(ctx, conn)
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	byName := make(map[string]Table, len(s.Tables))
	for _, tb := range s.Tables {
		byName[tb.Name] = tb
	}

	tag, ok := byName["tag"]
	if !ok {
		t.Fatal("tag table not introspected")
	}
	cols := map[string]Column{}
	for _, c := range tag.Columns {
		cols[c.Name] = c
	}
	if c := cols["name"]; c.Nullable || c.Default != "" {
		t.Errorf("tag.name: want NOT NULL with no default, got nullable=%v default=%q", c.Nullable, c.Default)
	}
	if c := cols["propagates"]; c.Nullable || c.Default != "true" {
		t.Errorf("tag.propagates: want NOT NULL default true, got nullable=%v default=%q", c.Nullable, c.Default)
	}

	tb, ok := byName["tag_binding"]
	if !ok {
		t.Fatal("tag_binding table not introspected")
	}
	hasKindCheck := false
	for _, ck := range tb.Checks {
		if ck.Name == "tag_binding_owner_kind_check" {
			hasKindCheck = true
		}
	}
	if !hasKindCheck {
		t.Errorf("tag_binding: owner-kind CHECK not introspected; checks=%+v", tb.Checks)
	}
	hasPlatformKey := false
	for _, u := range tb.Uniques {
		if u.Name == "tag_binding_platform_key" {
			hasPlatformKey = true
			if u.Definition == "" {
				t.Error("tag_binding_platform_key: empty definition")
			}
		}
	}
	if !hasPlatformKey {
		t.Errorf("tag_binding: platform partial unique index not introspected; uniques=%+v", tb.Uniques)
	}
}

// TestFactsMatchesCommitted is the drift gate for the generated facts artifact:
// the committed docs/src/generated/schema.json must equal a fresh
// introspection's render, or a schema change shipped without make gen.
func TestFactsMatchesCommitted(t *testing.T) {
	dsn := storagetest.NewDSN(t) // skips under -short
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	s, err := Introspect(ctx, conn)
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	fresh, err := FactsJSON(s, Subsystems)
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}

	committed, err := os.ReadFile("../../docs/src/generated/schema.json")
	if err != nil {
		t.Fatalf("read committed schema.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/schema.json drifted from the live schema: run make gen and commit the result")
	}

	// The artifact must parse and carry the ground-truth pair, so a structural
	// regression cannot hide behind byte equality of two broken renders.
	var doc struct {
		Subsystems map[string]map[string]json.RawMessage `json:"subsystems"`
	}
	if err := json.Unmarshal(fresh, &doc); err != nil {
		t.Fatalf("facts JSON does not parse: %v", err)
	}
	content, ok := doc.Subsystems["content"]
	if !ok {
		t.Fatal("facts JSON: no content subsystem")
	}
	for _, want := range []string{"tag", "tag_binding"} {
		if _, ok := content[want]; !ok {
			t.Errorf("facts JSON: content subsystem missing table %q", want)
		}
	}
}
