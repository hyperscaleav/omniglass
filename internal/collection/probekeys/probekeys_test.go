package probekeys

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// declaredKeys reads every exported string constant this package declares, from
// its source, so the guards below see a key the moment it is written, whether
// or not anyone remembered to list it. Every such constant is a sample key.
func declaredKeys(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	var keys []string
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if !id.IsExported() {
						continue
					}
					if i >= len(vs.Values) {
						t.Fatalf("%s: constant %s has no literal value; the key guard reads literals only", name, id.Name)
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: constant %s is not a string literal; the key guard reads literals only", name, id.Name)
					}
					key, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s: unquote %s: %v", name, id.Name, err)
					}
					keys = append(keys, key)
				}
			}
		}
	}
	if len(keys) == 0 {
		t.Fatal("found no declared key: the guard is reading the wrong files")
	}
	slices.Sort(keys)
	return keys
}

// TestEveryKeyRuled is #1080's guard: every key this package declares is ruled
// for the push lane exactly once, node-only (refused) or pushable with the
// reason a push may write it. A key added without a ruling fails here, rather
// than shipping pushable by omission.
func TestEveryKeyRuled(t *testing.T) {
	declared := declaredKeys(t)
	for _, key := range declared {
		only := slices.Contains(nodeOnly, key)
		_, push := pushable[key]
		switch {
		case only && push:
			t.Errorf("%s is ruled both node-only and pushable; pick one", key)
		case !only && !push:
			t.Errorf("%s is declared but not ruled: add it to nodeOnly (the push lane refuses it), "+
				"or to pushable with the one-line reason a push may write it", key)
		}
	}
	for key, why := range pushable {
		if strings.TrimSpace(why) == "" {
			t.Errorf("pushable %s carries no reason", key)
		}
	}
	for _, key := range Native() {
		if !slices.Contains(declared, key) {
			t.Errorf("%s is ruled but no constant declares it", key)
		}
	}
	if sorted := slices.Sorted(slices.Values(nodeOnly)); len(slices.Compact(sorted)) != len(nodeOnly) {
		t.Errorf("nodeOnly lists a key twice: %v", nodeOnly)
	}
}

// The native probes' sample keys are declared once, here: the probes emit
// these constants and the calc rule's collected-output check reads Native.
func TestNative(t *testing.T) {
	got := Native()
	if want := declaredKeys(t); !slices.Equal(got, want) {
		t.Fatalf("Native() = %v, want every declared key %v", got, want)
	}
	got[0] = "mutated"
	if Native()[0] == "mutated" {
		t.Error("Native shares its backing array")
	}
	if !IsNative(TCPOpen) || IsNative("uptime") {
		t.Error("IsNative disagrees with Native")
	}
}

// IsNodeOnly is the push lane's refusal: every node-only key, under any casing
// or surrounding space, and nothing pushable or foreign.
func TestIsNodeOnly(t *testing.T) {
	for _, key := range nodeOnly {
		for _, variant := range []string{key, strings.ToUpper(key), " " + key, key + "\t"} {
			if !IsNodeOnly(variant) {
				t.Errorf("IsNodeOnly(%q) = false, want true", variant)
			}
		}
	}
	for key := range pushable {
		if IsNodeOnly(key) {
			t.Errorf("IsNodeOnly(%q) = true for a pushable key", key)
		}
	}
	for _, foreign := range []string{"uptime", "video-input", "", "tcp", "tcp-open-x"} {
		if IsNodeOnly(foreign) {
			t.Errorf("IsNodeOnly(%q) = true, want false", foreign)
		}
	}
}

// The tls keys (#1035) are the node's alone: its pin steers what it attempts,
// as the ssh host key does, and its handshake timing and certificate facts are
// observations only the probing node makes.
func TestTheTLSKeysAreNodeOnly(t *testing.T) {
	for _, key := range []string{TLSCertificate, TLSHandshakeTime, TLSCertificateExpiry, TLSCertificateDaysLeft} {
		if !IsNodeOnly(key) {
			t.Errorf("%s is pushable", key)
		}
	}
}

// The collection telemetry keys (#1369) are the node's account of its own
// collection: how long its polls took, how many failed, and how many pushed
// messages its listeners did or did not claim. Only the collecting node
// observes them, so a push may not forge them.
func TestTheCollectionTelemetryKeysAreNodeOnly(t *testing.T) {
	for _, key := range []string{PollDuration, PollFailureCount, PushMessageCount, PushUnclaimedCount} {
		if !IsNodeOnly(key) {
			t.Errorf("IsNodeOnly(%q) = false: a push could forge the node's account of its own collection", key)
		}
	}
}
