package storage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// sampleWriteSQL matches SQL that writes a row of a sample table (metric,
// property) or of event: an insert into one, or an update of one. It reads the
// table name whole, so metric_type, property_type and event_rule never match.
var sampleWriteSQL = regexp.MustCompile(`(?is)\b(insert\s+into|update)\s+(metric|property|event)\b\s*(\(|set\b|select\b)`)

// sampleWriters reads this package's non-test source and returns every
// function whose body carries SQL writing a sample or event row, with whether
// that body calls admitWrites. A closure's write is its enclosing function's.
func sampleWriters(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	out := map[string]bool{}
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
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			writes, admits := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						if sql, err := strconv.Unquote(x.Value); err == nil && sampleWriteSQL.MatchString(sql) {
							writes = true
						}
					}
				case *ast.CallExpr:
					if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "admitWrites" {
						admits = true
					}
				}
				return true
			})
			if writes {
				if _, dup := out[fn.Name.Name]; dup {
					t.Fatalf("two functions named %s write sample rows; the registry keys on the name", fn.Name.Name)
				}
				out[fn.Name.Name] = admits
			}
		}
	}
	return out
}

// TestEverySampleWritePathAdmitsItsWriter is the write-side twin of the
// unscoped-read guard (ADR-0161): every gateway function that writes a sample
// or an event row is a registered write path naming its writers, and holds
// every row to the key's writer class (calls admitWrites) before writing. A new
// path that skips either fails here, naming the function.
func TestEverySampleWritePathAdmitsItsWriter(t *testing.T) {
	found := sampleWriters(t)
	if len(found) == 0 {
		t.Fatal("found no sample write: the guard is reading the wrong files")
	}
	for name, admits := range found {
		if _, ok := SampleWritePaths[name]; !ok {
			t.Errorf("%s writes a sample or event row but is not a registered write path: add it to SampleWritePaths with its writers, and a refusal case to TestWriterMismatchRefused", name)
		}
		if !admits {
			t.Errorf("%s writes a sample or event row without admitWrites: every row is held to its key's writer class before it lands", name)
		}
	}
	for name, writers := range SampleWritePaths {
		if _, ok := found[name]; !ok {
			t.Errorf("SampleWritePaths registers %s, which writes no sample or event row", name)
		}
		if len(writers) == 0 {
			t.Errorf("write path %s names no writer", name)
		}
	}
	// Every writer is some path's, so the ruling names no writer nothing uses.
	for _, w := range writerclass.Writers() {
		used := false
		for _, writers := range SampleWritePaths {
			used = used || slices.Contains(writers, w)
		}
		if !used {
			t.Errorf("writer %s is no registered path's", w)
		}
	}
}

// sampleMigrations are the applied migrations that write sample rows, each
// with why it is no writer of a key's value. Migrations run once and are never
// edited, so this list only grows by a new migration, which must state here
// why its write leaves every key's writer intact (a one-time backfill that
// moved values between keys would not).
var sampleMigrations = map[string]string{
	"20260805170000_state_becomes_property.sql": "reshapes the value column of every existing property row in place (#591); no key, provenance or writer changes",
}

// TestMigrationsWriteNoSamples: a migration is a write path that runs outside
// the gateway, so one writing sample or event rows must be ruled here.
func TestMigrationsWriteNoSamples(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "db", "migrations", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("list migrations: %v (%d files)", err, len(files))
	}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		name := filepath.Base(path)
		if sampleWriteSQL.Match(raw) {
			if why, ok := sampleMigrations[name]; !ok || strings.TrimSpace(why) == "" {
				t.Errorf("migration %s writes sample or event rows: state in sampleMigrations why it leaves every key's writer intact", name)
			}
		} else if _, ok := sampleMigrations[name]; ok {
			t.Errorf("sampleMigrations rules %s, which writes no sample row", name)
		}
	}
}

// TestAdmitWrite pins the pure check every path runs, beyond the class table
// writerclass holds: a node writes a node-probe key only under its task's
// endpoint (never with none), and a calc rule writes only a key it outputs.
func TestAdmitWrite(t *testing.T) {
	probe := keyWriterOf{class: writerclass.NodeProbe}
	calc := keyWriterOf{class: writerclass.Calc, rules: map[string]string{"r-1": "temp-avg"}}
	collected := keyWriterOf{class: writerclass.Collected}
	asRule := func(id string) KeyWriter { return calcWriter(&sampleLineage{ruleID: id, ruleName: "x", version: 1}) }
	cases := []struct {
		name  string
		kw    keyWriterOf
		w     KeyWriter
		row   keyedWrite
		admit bool
	}{
		{"node on its endpoint", probe, NodeWriter("ssh"), keyedWrite{"tcp-open", "ssh"}, true},
		{"node on another endpoint", probe, NodeWriter("ssh"), keyedWrite{"tcp-open", "panel"}, false},
		{"node with no instance", probe, NodeWriter("ssh"), keyedWrite{"tcp-open", ""}, false},
		{"node bound to no endpoint", probe, NodeWriter(""), keyedWrite{"tcp-open", ""}, false},
		{"push on a probe key", probe, PushWriter(), keyedWrite{"tcp-open", "ssh"}, false},
		{"the pin on the host key", keyWriterOf{class: writerclass.NodeProbe}, gatewayWriter(writerclass.HostKeyPin), keyedWrite{"ssh-host-key", "ssh"}, true},
		{"the pin on the gate", probe, gatewayWriter(writerclass.HostKeyPin), keyedWrite{"endpoint-rejection", "ssh"}, false},
		{"its own rule", calc, asRule("r-1"), keyedWrite{"avg-temp", ""}, true},
		{"another rule", calc, asRule("r-2"), keyedWrite{"avg-temp", ""}, false},
		{"a push on a calc key", calc, PushWriter(), keyedWrite{"avg-temp", ""}, false},
		{"an operator on a calc key", calc, gatewayWriter(writerclass.Operator), keyedWrite{"avg-temp", ""}, true},
		{"a rule on a collected key", collected, asRule("r-1"), keyedWrite{"video-input", ""}, false},
		{"a driver's instanced series", collected, NodeWriter("ssh"), keyedWrite{"uptime", "if-3"}, true},
	}
	for _, c := range cases {
		err := admitWrite(c.kw, c.w, c.row)
		if (err == nil) != c.admit {
			t.Errorf("%s: admitWrite = %v, want admitted %v", c.name, err, c.admit)
		}
		if err != nil && !strings.Contains(err.Error(), c.row.key) {
			t.Errorf("%s: refusal %q does not name %s", c.name, err, c.row.key)
		}
	}
}
