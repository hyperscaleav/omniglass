package storage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// metricFromSQL matches SQL that reads the raw metric table by name, as a
// from or a join, optionally schema-qualified or quoted, read whole so
// metric_series, metric_block and the rest never match.
var metricFromSQL = regexp.MustCompile(`(?is)\b(?:from|join)\s+(?:"?public"?\s*\.\s*)?"?metric"?(?:[^\w"]|$)`)

// directMetricReaders is every declaration outside a test that may name the
// raw metric table in a from or join, each with why (ADR-1530): every other
// reader of a series' samples reads through the series read, so a folded day
// reads the same as its raw rows. Keyed by the file's path from the module
// root and the declaration (a function, or a package-level var or const);
// a whole package is keyed by its directory and "*".
var directMetricReaders = map[string]string{
	"internal/storage/metric_read.go:metricReadFromSQL":  "the series read's raw arm",
	"internal/storage/metric_read.go:metricNewestRawSQL": "the series read's newest raw row, the current value's first arm",
	"internal/storage/metric_read.go:metricStoredSQL":    "whether a series holds a raw row or a block, a probe of each, no value read",
	"internal/storage/metric_block.go:blockDaysSQL":      "the day fold, which writes the blocks from the raw rows",
	"internal/storage/metric_block.go:blockWeekSQL":      "the week fold, which writes the blocks from the raw rows",
	"internal/storage/metric_block.go:verifyBlocksSQL":   "the fold's verify, which checks a block against the raw rows it holds",
	"internal/storage/metric_trend.go:foldTrendHour":     "the trend fold of an hour no block can cover yet (the open hour, before its day folds)",
	"internal/storage/rule_eval.go:ruleMarksSQL":         "the reconcile's arrival feed, an id cursor over the rows inserted, no sample's value",
	"internal/storage/rule_eval.go:ruleCandidatesSQL":    "the reconcile's arrival feed (the series past the id mark); their values are the series read's",
	"internal/storage/storagetest:*":                     "test support that plants and counts raw rows",
	"internal/labfootprint:*":                            "the lab footprint tool, which measures the raw tier itself",
	"internal/compressbench:*":                           "the compression bench, which measures the raw tier against candidate formats",
}

// directMetricReads returns the declarations of a parsed file whose string
// literals name the raw metric table in a from or join, each keyed as
// directMetricReaders is (path the file's from the module root).
func directMetricReads(path string, f *ast.File) []string {
	var out []string
	named := func(n ast.Node, decl string) {
		ast.Inspect(n, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if sql, err := strconv.Unquote(lit.Value); err == nil && metricFromSQL.MatchString(sql) {
					if k := path + ":" + decl; !slices.Contains(out, k) {
						out = append(out, k)
					}
				}
			}
			return true
		})
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			named(d, d.Name.Name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					for _, n := range v.Names {
						named(v, n.Name)
					}
				}
			}
		}
	}
	return out
}

// allowedMetricRead is the directMetricReaders entry that rules key, by its
// declaration or its package, and whether there is one.
func allowedMetricRead(key string) (string, bool) {
	if _, ok := directMetricReaders[key]; ok {
		return key, true
	}
	pkg := filepath.Dir(key[:strings.LastIndex(key, ":")]) + ":*"
	_, ok := directMetricReaders[pkg]
	return pkg, ok
}

// TestNoDirectMetricReads holds every reader of a metric series' samples to
// the series read (#1573, ADR-1530): non-test code under internal/ and cmd/
// names the raw metric table only in a declaration directMetricReaders
// rules, and every entry there still reads it, so the list never outlives
// what it rules.
func TestNoDirectMetricReads(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	used := map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			for _, k := range directMetricReads(filepath.ToSlash(rel), f) {
				entry, ok := allowedMetricRead(k)
				used[entry] = true
				if !ok {
					t.Errorf("%s reads the metric table directly: read a series' samples through the series read (metricReadSQL, metricNewestSQL), or rule it in directMetricReaders with why", k)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	for k := range directMetricReaders {
		if !used[k] {
			t.Errorf("directMetricReaders rules %s, which no longer reads the metric table: drop the entry", k)
		}
	}
}

// TestDirectMetricReadGuardCatchesAPlantedRead is the guard's own proof: a
// direct read planted in a function no entry rules is caught, by a from or a
// join, schema-qualified or not, and a read through the series read, or of a
// table whose name only begins with metric, is not.
func TestDirectMetricReadGuardCatchesAPlantedRead(t *testing.T) {
	t.Parallel()
	src := `package storage

func plantedFrom() string { return "select m.value from metric m where m.series_id = $1" }

func plantedJoin() string {
	return ` + "`" + `select s.id from metric_series s
		join public.metric m on m.series_id = s.id` + "`" + `
}

func plantedTail() string { return "select count(*) from metric" }

var plantedVar = "select 1 from \"metric\" where true"

func throughTheRead() string { return "select r.ts from metric_series s cross join lateral (" + metricReadSQL("s.id", "$1", "$2", "14") + ") r" }

func otherTables() string {
	return "select 1 from metric_series s join metric_block b on b.series_id = s.id join metric_trend t on true from metric_declared"
}
`
	f, err := parser.ParseFile(token.NewFileSet(), "planted.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := directMetricReads("internal/storage/planted.go", f)
	want := []string{
		"internal/storage/planted.go:plantedFrom",
		"internal/storage/planted.go:plantedJoin",
		"internal/storage/planted.go:plantedTail",
		"internal/storage/planted.go:plantedVar",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("caught %v, want %v", got, want)
	}
	for _, k := range got {
		if _, ok := allowedMetricRead(k); ok {
			t.Errorf("%s is allowed, want it caught", k)
		}
	}
}
