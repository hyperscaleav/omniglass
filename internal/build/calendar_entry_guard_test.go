package build

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCalendarEntryIsOpenedOnlyThroughItsAccessors guards the lab calendar's
// sum type (#2798). A calendar.Entry carries one payload of its kind, read
// through AsMeeting and AsProblem, which report false for another kind. Outside
// sim/calendar nothing may reach a payload pointer, build an Entry by literal
// (which skips the check that its kind and payload agree), or import the test
// helper sim/calendar/caltest from non-test code. The compiler already refuses
// the payload field because it is unexported; this keeps the rule from being
// undone by exporting it again.
func TestCalendarEntryIsOpenedOnlyThroughItsAccessors(t *testing.T) {
	root := filepath.Join("..", "..")
	const calPkg = "github.com/hyperscaleav/omniglass/sim/calendar"
	var found []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "dist", ".git", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "sim/calendar/") && !strings.HasPrefix(rel, "sim/calendar/caltest/") {
			return nil
		}
		isTest := strings.HasSuffix(rel, "_test.go")
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			return nil
		}
		name := ""
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == calPkg+"/caltest" && !isTest {
				found = append(found, rel+": non-test code imports caltest")
			}
			if p == calPkg {
				name = "calendar"
				if imp.Name != nil {
					name = imp.Name.Name
				}
			}
		}
		if name == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				if sel, ok := x.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Entry" {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == name && len(x.Elts) > 0 {
						found = append(found, rel+": builds a calendar.Entry by literal; use NewEntry, MeetingEntry or ProblemEntry")
					}
				}
			case *ast.StarExpr:
				if sel, ok := x.X.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Meeting" || sel.Sel.Name == "Problem") {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == name {
						found = append(found, rel+": dereferences a calendar payload pointer; use AsMeeting or AsProblem")
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Fatalf("the calendar entry is opened outside its accessors:\n  %s", strings.Join(found, "\n  "))
	}
}
