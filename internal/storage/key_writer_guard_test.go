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

// sampleTable is a sample table (metric, property) or event, as SQL may name
// it: optionally schema-qualified, optionally quoted, read whole so
// metric_type, property_type and event_rule never match.
const sampleTable = `(?:"?public"?\s*\.\s*)?"?(?:metric|property|event)"?`

// sampleWriteSQL matches SQL that writes a sample or event row, whatever
// follows the table: an insert (a column list, values, a select, an alias), an
// update (with or without an alias, then set), a merge, or a copy.
var sampleWriteSQL = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\binsert\s+into\s+` + sampleTable + `(?:[^\w"]|$)`),
	regexp.MustCompile(`(?is)\bupdate\s+(?:only\s+)?` + sampleTable + `(?:\s+(?:as\s+)?\w+)?\s+set\b`),
	regexp.MustCompile(`(?is)\bmerge\s+into\s+` + sampleTable + `(?:[^\w"]|$)`),
	regexp.MustCompile(`(?is)\bcopy\s+` + sampleTable + `(?:[^\w"]|$)`),
}

// dynamicTableSQL matches a write whose table is spliced in rather than
// written (a literal ending at the verb, or a Sprintf verb as the table), which
// no pattern can read; such a function is ruled in dynamicTableWriters.
var dynamicTableSQL = regexp.MustCompile(`(?is)\b(insert\s+into|update|merge\s+into|copy)\s+(%(\[\d+\])?s\b|$)`)

func writesSample(sql string) bool {
	for _, re := range sampleWriteSQL {
		if re.MatchString(sql) {
			return true
		}
	}
	return false
}

// dynamicTableWriters are the functions whose SQL splices its table in, each
// with the tables it reaches: none is a sample table, and a new one must say
// which it reaches (or be a registered write path) before this guard passes.
var dynamicTableWriters = map[string]string{
	"writeLabelChanges": "the label column of location, system and component (a scopeTable)",
	"stamp":             "a liveness stamp table (endpoint_live), never a sample table",
	"RenameRegistry":    "the name column of a renameable registry (renameableRegistries: vendor, driver, product, standard, location_type), never a sample table",
	"setTypeRetention":  "the retention_days column of a type catalog (metric_type, property_type, #417), never a sample table",
}

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
			if gen, ok := decl.(*ast.GenDecl); ok {
				// SQL held at package level is read by no write path's body,
				// so the registry cannot see who runs it: keep it in one.
				ast.Inspect(gen, func(n ast.Node) bool {
					if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if sql, err := strconv.Unquote(lit.Value); err == nil && writesSample(sql) {
							t.Errorf("%s declares sample-writing SQL at package level: write it in a registered write path's body", name)
						}
					}
					return true
				})
				continue
			}
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			writes, admits := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						if sql, err := strconv.Unquote(x.Value); err == nil {
							if writesSample(sql) {
								writes = true
							}
							if dynamicTableSQL.MatchString(sql) {
								if _, ruled := dynamicTableWriters[fn.Name.Name]; !ruled {
									t.Errorf("%s splices the table of a write into its SQL: rule it in dynamicTableWriters with the tables it reaches", fn.Name.Name)
								}
							}
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
	"20260805170000_state_becomes_property.sql":              "reshapes the value column of every existing property row in place (#591); no key, provenance or writer changes",
	"20260805210000_command_status_and_intended_lineage.sql": "repoints the lineage of existing intended rows onto the command that opened them (command_id); no key, value or writer changes",
	"20261024120100_legacy_writer_backfill.sql":              "holds legacy rows to the ruling (ADR-1100): stamps the source of a declared pin its audited :retry wrote, moves rows the ruling refuses into retired_sample, and its down restores them as they were; no key or value changes",
	"20261025130100_command_outcome_backfill.sql":            "holds the newly reserved command outcome events to their class (ADR-1155): moves those no command's outcome wrote into retired_sample, and its down restores them as they were; no key or value changes",
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
		if writesSample(string(raw)) {
			if why, ok := sampleMigrations[name]; !ok || strings.TrimSpace(why) == "" {
				t.Errorf("migration %s writes sample or event rows: state in sampleMigrations why it leaves every key's writer intact", name)
			}
		} else if _, ok := sampleMigrations[name]; ok {
			t.Errorf("sampleMigrations rules %s, which writes no sample row", name)
		}
	}
}

// TestAdmitWrite pins the pure check every path runs, beyond the class table
// writerclass holds: a node writes only on the component its task collects
// for, and a node-probe key only under the endpoint its task ran (never with
// none); and a calc rule writes only a key it outputs.
func TestAdmitWrite(t *testing.T) {
	probe := keyWriterOf{class: writerclass.NodeProbe}
	calc := keyWriterOf{class: writerclass.Calc, rules: map[string]string{"r-1": "temp-avg"}}
	collected := keyWriterOf{class: writerclass.Collected}
	asRule := func(id string) KeyWriter { return calcWriter(&sampleLineage{ruleID: id, ruleName: "x", version: 1}) }
	const compA, compB = "aaaaaaaa-0000-0000-0000-000000000001", "bbbbbbbb-0000-0000-0000-000000000002"
	// node is a node writer as admitWrites leaves it: bound to compA, or
	// unbound when it names no component or endpoint.
	node := func(ep string, held bool) KeyWriter {
		w := NodeWriter(compA, ep)
		if held {
			w.bound = compA
		}
		return w
	}
	on := func(comp, key, instance string) keyedWrite {
		return keyedWrite{key: key, instance: instance, ownerKind: "component", owner: comp}
	}
	cases := []struct {
		name  string
		kw    keyWriterOf
		w     KeyWriter
		row   keyedWrite
		admit bool
	}{
		{"node on its endpoint", probe, node("ssh", true), on(compA, "tcp-open", "ssh"), true},
		{"node on another endpoint", probe, node("ssh", true), on(compA, "tcp-open", "panel"), false},
		{"node with no instance", probe, node("ssh", true), on(compA, "tcp-open", ""), false},
		{"node bound to no endpoint", probe, node("", false), on(compA, "tcp-open", ""), false},
		{"node on another component's same-named endpoint", probe, node("ssh", true), on(compB, "tcp-open", "ssh"), false},
		{"node writing for a system", collected, node("ssh", true), keyedWrite{key: "uptime", ownerKind: "system", owner: compA}, false},
		{"push on a probe key", probe, PushWriter(), on(compA, "tcp-open", "ssh"), false},
		{"the pin on the host key", keyWriterOf{class: writerclass.NodeProbe}, gatewayWriter(writerclass.KeyPin), on(compA, "ssh-host-key", "ssh"), true},
		{"the pin on the gate", probe, gatewayWriter(writerclass.KeyPin), on(compA, "endpoint-rejection", "ssh"), false},
		{"its own rule", calc, asRule("r-1"), keyedWrite{key: "avg-temp"}, true},
		{"another rule", calc, asRule("r-2"), keyedWrite{key: "avg-temp"}, false},
		{"a push on a calc key", calc, PushWriter(), keyedWrite{key: "avg-temp"}, false},
		{"an operator on a calc key", calc, gatewayWriter(writerclass.Operator), keyedWrite{key: "avg-temp"}, true},
		{"a rule on a collected key", collected, asRule("r-1"), keyedWrite{key: "video-input"}, false},
		{"a driver's instanced series", collected, node("ssh", true), on(compA, "uptime", "if-3"), true},
		{"a driver's series on another component", collected, node("ssh", true), on(compB, "uptime", "if-3"), false},
		{"a command's lifecycle event by a push", keyWriterOf{class: writerclass.CommandLifecycle}, PushWriter(), on(compA, "command-issued", ""), false},
		{"a command's lifecycle event by its command", keyWriterOf{class: writerclass.CommandLifecycle}, gatewayWriter(writerclass.Command), on(compA, "command-issued", ""), true},
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

// TestSampleWriteSQLReadsEveryForm pins the guard's reading of SQL: every form
// that writes a sample or event row matches, and the neighbouring tables and
// the error strings that name a table do not.
func TestSampleWriteSQLReadsEveryForm(t *testing.T) {
	for _, sql := range []string{
		`insert into property (ts, owner_kind) values ($1, $2)`,
		`insert into metric values ($1)`,
		`INSERT INTO event SELECT * FROM x`,
		`insert into property as p (ts) values ($1)`,
		`insert into public.property (ts) values ($1)`,
		`insert into "property" (ts) values ($1)`,
		`insert into public."metric"(ts) values ($1)`,
		"insert into\n\t\tproperty\n(ts)",
		`update property p set command_id = 1`,
		`update metric set value = 1`,
		`update only public.event e set message = ''`,
		`merge into property using x on true when matched then do nothing`,
		`copy metric from stdin`,
	} {
		if !writesSample(sql) {
			t.Errorf("writesSample(%q) = false, want the write read", sql)
		}
	}
	for _, sql := range []string{
		`insert into property_type (name) values ($1)`,
		`insert into metric_type (name) values ($1)`,
		`insert into event_type (name) values ($1)`,
		`insert into event_rule_condition (rule_id) values ($1)`,
		`update metric_type set unit = $1`,
		`update property_type p set label = $1`,
		`storage: update property %q: %w`,
		`storage: begin update metric type: %w`,
		`storage: insert property sample %s/%s: %w`,
		`select * from property where provenance = 'observed'`,
		`delete from metric where ts < $1`,
	} {
		if writesSample(sql) {
			t.Errorf("writesSample(%q) = true, want no write read", sql)
		}
	}
	for _, sql := range []string{"\n\t\tupdate ", "insert into %[1]s (a)", "update %s set", "insert into "} {
		if !dynamicTableSQL.MatchString(sql) {
			t.Errorf("dynamicTableSQL misses %q", sql)
		}
	}
	for _, sql := range []string{"update", "select id from t where id = $1 for update", "for no key update"} {
		if dynamicTableSQL.MatchString(sql) {
			t.Errorf("dynamicTableSQL reads %q as a spliced table", sql)
		}
	}
}
