// Package views is the read-side primitive: a view is a named, parameterized,
// scope-checked query returning the uniform ViewResult every renderer and
// generated client consumes. Default views ship with the binary as declared Go
// (PR-governed like seed data, no view table); the registry validates each
// declaration at construction so a misdeclared view fails at boot, never at
// run time. The package holds no I/O of its own: a view's Run receives the
// Storage Gateway and the caller's resolved read scope, and every query it
// issues runs in the Gateway's scoped mode.
package views

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The two errors a view's Run may return that are the CALLER's fault, not the
// server's. Everything else surfaces as a 500, so a view can never leak the
// shape of a failure it did not classify.
var (
	// ErrTargetNotFound reports that a parameterized target does not exist or
	// is outside the caller's scope. The two are deliberately one error: the
	// route answers a non-disclosing 404 either way, so a probe cannot map the
	// estate by watching which names 403 and which 404.
	ErrTargetNotFound = errors.New("views: target not found")
	// ErrInvalidParam reports a binding a view rejects at run time, after
	// BindParams has type-checked it (an unusable page token, a value outside
	// the range the view accepts). The route answers 400.
	ErrInvalidParam = errors.New("views: invalid parameter")
)

// Column describes one ViewResult column: its name, its value type ("string",
// "number", or "time"), and an optional role hint ("value", "label", "time",
// "series") the field-mapping anchors for renderers.
type Column struct {
	Name string `json:"name" doc:"The column name, the key a field-mapping addresses"`
	Type string `json:"type" doc:"The cell value type: string, number, or time"`
	Role string `json:"role,omitempty" doc:"An optional role hint: value, label, time, or series"`
}

// ViewResult is the uniform shape every view run returns: the declared
// columns, the rows as positional cells, and the cursor for the next page
// (empty when the result is complete). One shape, one renderer contract.
type ViewResult struct {
	Columns []Column `json:"columns" doc:"The columns, in cell order"`
	Rows    [][]any  `json:"rows" doc:"The result rows; each cell is positional against columns"`
	// NextPageToken is the opaque cursor a paged view returns; a follow-up run
	// passing it as page_token continues where this page ended.
	NextPageToken string `json:"next_page_token,omitempty" doc:"The cursor for the next page; absent on the last page"`
}

// Param declares one typed run-time parameter of a view. Binding is strict:
// an undeclared name, a missing required one, or an unparseable value is a
// clean error naming the parameter.
type Param struct {
	Name     string `json:"name" doc:"The parameter name"`
	Type     string `json:"type" doc:"The value type: string, int, duration (Go syntax), or time (RFC3339)"`
	Required bool   `json:"required" doc:"Whether a run without this parameter is rejected"`
	Doc      string `json:"doc,omitempty" doc:"What the parameter selects"`
}

// paramTypes is the closed set of declarable parameter types and their
// parsers. A declaration outside it fails registration.
var paramTypes = map[string]func(raw string) (any, error){
	"string": func(raw string) (any, error) { return raw, nil },
	"int": func(raw string) (any, error) {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("not an integer")
		}
		return n, nil
	},
	"duration": func(raw string) (any, error) {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("not a duration (Go syntax, e.g. 24h)")
		}
		return d, nil
	},
	"time": func(raw string) (any, error) {
		ts, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, fmt.Errorf("not an RFC3339 time")
		}
		return ts, nil
	},
}

// RunFunc executes a view: it receives the Gateway, the caller's resolved read
// scope for the view's subject resource, the bound params, and the page cursor,
// and returns the rows (positional against the declared columns) plus the next
// cursor. Every query it issues MUST run in the Gateway's scoped mode; the
// scope is resolved by the caller, never inside the view.
//
// A view's query MUST impose a TOTAL order (break every tie, ultimately on a
// unique column), not merely a sort key. Postgres may return equal-keyed rows
// in any order between runs, and the watch detector reads a re-run's row order
// as content: a partial order makes the hash flap, so every watcher refetches
// the whole view every interval forever, which is exactly the load the seam
// exists to avoid.
type RunFunc func(ctx context.Context, gw storage.Gateway, read scope.Set, params map[string]any, pageToken string) (rows [][]any, nextPageToken string, err error)

// Definition declares one default view: its addressable name (ADR-0062), the
// resource permission its subject requires on top of view:read, the resource
// whose visible_set bounds its rows, its typed params, its columns, the
// field-mapping placing renderer roles onto columns, and the query itself.
type Definition struct {
	Name          string
	Summary       string
	Permission    []string // the declared permission tokens, e.g. ["component", "read"]
	ScopeResource string   // the resource whose visible_set scopes every query
	Params        []Param
	Columns       []Column
	FieldMapping  map[string]string // renderer role -> column name
	Run           RunFunc
}

// PermissionString renders the declared permission in its ":"-joined form
// ("component:read"), the form the directory publishes.
func (d Definition) PermissionString() string { return strings.Join(d.Permission, ":") }

// Registry is the set of default views shipped with the binary, validated at
// construction and addressed by name.
type Registry struct {
	byName map[string]Definition
	names  []string
}

// NewRegistry validates and indexes the given definitions. Any misdeclaration
// (a duplicate or empty name, a missing permission or scope resource, a
// field-mapping naming an absent column, a param of an unknown type, a nil
// Run) is an error, so a bad default view can never boot.
func NewRegistry(defs ...Definition) (*Registry, error) {
	r := &Registry{byName: make(map[string]Definition, len(defs))}
	for _, d := range defs {
		if err := validate(d); err != nil {
			return nil, fmt.Errorf("views: %w", err)
		}
		if _, dup := r.byName[d.Name]; dup {
			return nil, fmt.Errorf("views: duplicate view name %q", d.Name)
		}
		r.byName[d.Name] = d
		r.names = append(r.names, d.Name)
	}
	sort.Strings(r.names)
	return r, nil
}

// Get returns the named view.
func (r *Registry) Get(name string) (Definition, bool) {
	d, ok := r.byName[name]
	return d, ok
}

// All returns every view in name order, the directory's listing order.
func (r *Registry) All() []Definition {
	out := make([]Definition, 0, len(r.names))
	for _, n := range r.names {
		out = append(out, r.byName[n])
	}
	return out
}

// validate is the per-definition declaration check behind NewRegistry.
func validate(d Definition) error {
	if d.Name == "" {
		return fmt.Errorf("a view needs a name")
	}
	if len(d.Permission) == 0 {
		return fmt.Errorf("view %q declares no permission", d.Name)
	}
	if d.ScopeResource == "" {
		return fmt.Errorf("view %q declares no scope resource", d.Name)
	}
	if d.Run == nil {
		return fmt.Errorf("view %q has no Run", d.Name)
	}
	cols := make(map[string]bool, len(d.Columns))
	for _, c := range d.Columns {
		cols[c.Name] = true
	}
	for role, col := range d.FieldMapping {
		if !cols[col] {
			return fmt.Errorf("view %q maps role %q onto absent column %q", d.Name, role, col)
		}
	}
	for _, p := range d.Params {
		if _, ok := paramTypes[p.Type]; !ok {
			return fmt.Errorf("view %q param %q has unknown type %q", d.Name, p.Name, p.Type)
		}
	}
	return nil
}

// BindParams validates raw "name=value" pairs against a declaration and
// returns the typed values. Strict by contract: an undeclared name, a pair
// without "=", a duplicate, an unparseable value, or a missing required
// parameter is an error naming the parameter, which the run route surfaces as
// its 400.
func BindParams(decl []Param, pairs []string) (map[string]any, error) {
	byName := make(map[string]Param, len(decl))
	for _, p := range decl {
		byName[p.Name] = p
	}
	out := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		name, raw, ok := strings.Cut(pair, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("parameter binding %q is not name=value", pair)
		}
		p, declared := byName[name]
		if !declared {
			return nil, fmt.Errorf("parameter %q is not declared by this view", name)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("parameter %q is bound twice", name)
		}
		v, err := paramTypes[p.Type](raw)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %v", name, err)
		}
		out[name] = v
	}
	for _, p := range decl {
		if p.Required {
			if _, ok := out[p.Name]; !ok {
				return nil, fmt.Errorf("required parameter %q is missing", p.Name)
			}
		}
	}
	return out, nil
}
