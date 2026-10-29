package storage_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// namespacedTables are the catalogs whose rows are keyed by (namespace, name)
// (#318). severity_level is not here: its names stay unique install-wide, so a
// name names one row.
var namespacedTables = []string{
	"metric_type", "property_type", "event_type", "command_type",
	"component_type", "system_type", "location_type", "secret_type",
	"notification_type", "event_rule", "action_rule", "calc_rule",
	"vendor", "product", "driver", "standard", "role",
}

// byNameAllowed are the statements that compare a namespaced table's name on
// purpose, across namespaces, keyed "func table". Each says why.
var byNameAllowed = map[string]string{
	"nameHolders metric_type":           "every holder of a name, in every namespace: the input the search path chooses from",
	"nameHolders property_type":         "every holder of a name, in every namespace: the input the search path chooses from",
	"nameHolders event_type":            "every holder of a name, in every namespace: the input the search path chooses from",
	"seriesKeysSQL property_type":       "the keys come from samples by id; the name only excludes keys already listed",
	"ownedIDByRef dyntable":             "secret and variable, owned entities, not namespaced catalogs",
	"settleOnObservation property_type": "a command type targets a key by id, possibly the og row a shipped command type was pinned to: any row of the name is the key's",
	"latestIntendedSQL property_type":   "the same: which command types target the key, by any row of its name",
	"collectedBy metric_type":           "the same: which command type targets the key, by any row of its name",
	"collectedBy property_type":         "the same: which command type targets the key, by any row of its name",
}

// positionalVerb is fmt's indexed verb (`%[1]s`): a spliced table name too.
var positionalVerb = regexp.MustCompile(`%\[\d+\]s`)

var sqlAliasStop = map[string]bool{
	"where": true, "on": true, "set": true, "using": true, "join": true, "left": true,
	"inner": true, "cross": true, "order": true, "group": true, "limit": true, "returning": true,
	"union": true, "for": true, "values": true, "select": true, "and": true, "or": true,
}

// sqlText renders a SQL expression the way the database sees its shape: a
// string literal verbatim, a `+` chain joined, registryRefCol(...) as `name`
// (it picks the name column for anything not a uuid), and any other operand as
// a placeholder. An operand that holds a table name renders as `dyntable`, so a
// by-name comparison against a table chosen at run time is caught too.
func sqlText(e ast.Expr) (string, bool) { return sqlTextIn(e, nil) }

// sqlTextIn is sqlText with the package's string constants expanded, so SQL
// assembled from a named fragment (`componentTypeResolved + " where ..."`) is
// read whole.
func sqlTextIn(e ast.Expr, consts map[string]ast.Expr) (string, bool) {
	sqlText := func(e ast.Expr) (string, bool) { return sqlTextIn(e, consts) }
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "?", false
		}
		s, err := strconv.Unquote(x.Value)
		if err != nil {
			return "?", false
		}
		return s, true
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "?", false
		}
		l, lok := sqlText(x.X)
		r, rok := sqlText(x.Y)
		return l + r, lok || rok
	case *ast.ParenExpr:
		return sqlText(x.X)
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "registryRefCol" {
			return "name", false
		}
		if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "refRows" || id.Name == "catalogRows") {
			return " resolved_rows ", false
		}
		return " ? ", false
	case *ast.Ident:
		if c, ok := consts[x.Name]; ok {
			delete(consts, x.Name) // a constant that names itself cannot loop
			s, lit := sqlText(c)
			consts[x.Name] = c
			return s, lit
		}
		if tableish(x.Name) {
			return " dyntable ", false
		}
		return " ? ", false
	case *ast.SelectorExpr:
		if tableish(x.Sel.Name) {
			return " dyntable ", false
		}
		return " ? ", false
	}
	return " ? ", false
}

func tableish(name string) bool {
	n := strings.ToLower(name)
	return n == "table" || n == "tbl" || n == "typetable" || n == "catalog" || strings.HasSuffix(n, "table")
}

type byNameSite struct {
	pos   string
	fn    string
	table string
}

// byNameSites finds every SQL expression in a package's non-test files that
// compares a namespaced table's name: `<alias>.name =`, `from <table> [alias]
// where [alias.]name =`, a bare `name =` where the statement reads that table
// alone, `on conflict (name)`, or any of these against a table chosen at run
// time (reported as table "dyntable").
func byNameSites(t *testing.T, dir string) []byNameSite {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	tables := append(append([]string{}, namespacedTables...), "dyntable")
	refRe := map[string]*regexp.Regexp{}
	for _, tb := range tables {
		refRe[tb] = regexp.MustCompile(`(?:from|join|update|into)\s+` + tb + `\b(?:\s+(?:as\s+)?([a-z_][a-z0-9_]*))?`)
	}
	anyRef := regexp.MustCompile(`(?:from|join|update|into)\s+[a-z_][a-z0-9_]*\b`)
	cmp := `\s*(?:=|<>|!=|in\s*\(|=\s*any|\?)`
	bare := regexp.MustCompile(`(?:^|[^.\w])name` + cmp)
	setName := regexp.MustCompile(`\bset\s+name\s*=\s*\$\d+`)
	namespaceCmp := regexp.MustCompile(`\bnamespace\s*(?:=|in\s*\()`)
	var out []byNameSite
	fset := token.NewFileSet()
	// The package's top-level string constants and vars, by name, so a
	// fragment they hold is expanded where it is spliced.
	consts := map[string]ast.Expr{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, sp := range gd.Specs {
				if vs, ok := sp.(*ast.ValueSpec); ok && len(vs.Names) == len(vs.Values) {
					for i, n := range vs.Names {
						consts[n.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		check := func(owner string, pos token.Pos, raw string) {
			s := setName.ReplaceAllString(strings.ToLower(raw), "set ")
			// A statement that names the namespace it reads is deliberate: it
			// is not resolving a bare name.
			if namespaceCmp.MatchString(s) {
				return
			}
			refs := len(anyRef.FindAllString(s, -1))
			for _, tb := range tables {
				for _, m := range refRe[tb].FindAllStringSubmatch(s, -1) {
					alias := m[1]
					if alias == "" || sqlAliasStop[alias] {
						alias = tb
					}
					direct := regexp.MustCompile(`from\s+` + tb + `\b(?:\s+(?:as\s+)?` + regexp.QuoteMeta(alias) + `)?\s+where\s+(?:` + regexp.QuoteMeta(alias) + `\.)?name` + cmp)
					qualified := regexp.MustCompile(`\b` + regexp.QuoteMeta(alias) + `\.name` + cmp)
					if direct.MatchString(s) || qualified.MatchString(s) || strings.Contains(s, "on conflict (name)") ||
						(refs == 1 && bare.MatchString(s)) {
						p := fset.Position(pos)
						out = append(out, byNameSite{pos: filepath.Base(p.Filename) + ":" + strconv.Itoa(p.Line), fn: owner, table: tb})
						break
					}
				}
			}
		}
		for _, decl := range f.Decls {
			owner := ""
			switch d := decl.(type) {
			case *ast.FuncDecl:
				owner = d.Name.Name
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					if vs, ok := sp.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
						owner = vs.Names[0].Name
					}
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BinaryExpr, *ast.BasicLit:
					if s, ok := sqlTextIn(x.(ast.Expr), consts); ok {
						check(owner, x.Pos(), s)
					}
					return false // a + chain is read whole, once
				case *ast.CallExpr:
					// fmt.Sprintf(format, ...): a %s in the format is where a
					// table name is spliced in.
					if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(x.Args) > 0 {
						if s, ok := sqlTextIn(x.Args[0], consts); ok {
							check(owner, x.Pos(), positionalVerb.ReplaceAllString(strings.ReplaceAll(s, "%s", " dyntable "), " dyntable "))
						}
					}
				}
				return true
			})
		}
	}
	return out
}

// TestNoByNameLookupSkipsTheSearchPath holds every by-name read of a namespaced
// catalog to the search path (#1432). Once `local/x` and `og/x` coexist, a
// `where name = $1` against the table matches both rows: a QueryRow picks one
// arbitrarily, a join fans out, a delete removes both. A by-name read goes
// through `<table>_in_effect()` (what a bare name resolves to) or names the
// namespace; one that means to look across namespaces is listed in
// byNameAllowed with its reason.
func TestNoByNameLookupSkipsTheSearchPath(t *testing.T) {
	if _, err := os.Stat("name_lane.go"); err != nil {
		t.Fatal("run from the storage package directory")
	}
	var bad []string
	for _, s := range byNameSites(t, ".") {
		if _, ok := byNameAllowed[s.fn+" "+s.table]; ok {
			continue
		}
		bad = append(bad, s.pos+" "+s.fn+" "+s.table)
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d by-name reads of a namespaced catalog skip the search path:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}
