package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"text/template"
)

// mutation is a pre-compiled state write, the form an override's expression
// takes (override.go). path is a template; valueTmpl is non-nil when the
// value is a string (and thus subject to template expansion and JSON
// coercion); rawValue carries any other value through unchanged.
type mutation struct {
	path      *template.Template
	valueTmpl *template.Template
	rawValue  any
	// optional marks the mutation as best-effort: a failure logs a warning
	// and the rest of the batch still applies.
	optional bool
}

// MutationError is the per-mutation failure wrapper returned by
// applyMutations. It carries the rendered state path, the underlying error and
// the optional flag. Path is the best-effort rendered path: if path-template
// rendering itself failed, Path holds the raw path template source.
type MutationError struct {
	Path     string
	Err      error
	Optional bool
}

func (e *MutationError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *MutationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// compileOneFuncs builds a single mutation against an explicit FuncMap so an
// override can supply per-device closures (a randStep bound to a device-seeded
// source). Path is a template; value may be a string (a template) or any other
// value (passed through).
func compileOneFuncs(path string, value any, optional bool, funcs template.FuncMap) (mutation, error) {
	pathT, err := template.New("mutate-path").Funcs(funcs).Parse(path)
	if err != nil {
		return mutation{}, fmt.Errorf("parsing mutate path %q: %w", path, err)
	}
	m := mutation{path: pathT, optional: optional}
	if s, ok := value.(string); ok {
		valT, err := template.New("mutate-value").Funcs(funcs).Parse(s)
		if err != nil {
			return mutation{}, fmt.Errorf("parsing mutate value %q: %w", s, err)
		}
		m.valueTmpl = valT
	} else {
		m.rawValue = value
	}
	return m, nil
}

// applyMutations writes every mutation into root. Runs under the device's
// state lock; callers must hold it. Returns the subset of mutations that
// actually applied (rendered path to stored value), which is the diff the
// caller ships upstream. Errors are accumulated per mutation as *MutationError
// but do not abort the batch.
func applyMutations(root map[string]any, muts []mutation, data templateData) (diffs map[string]any, errs []error) {
	if len(muts) == 0 {
		return nil, nil
	}
	diffs = make(map[string]any, len(muts))
	for _, m := range muts {
		path, err := renderTmpl(m.path, data)
		if err != nil {
			rawPath, _ := rawPathSource(m.path)
			errs = append(errs, &MutationError{
				Path:     rawPath,
				Err:      fmt.Errorf("rendering path: %w", err),
				Optional: m.optional,
			})
			continue
		}
		value, err := resolveMutationValue(m, data)
		if err != nil {
			errs = append(errs, &MutationError{
				Path:     path,
				Err:      fmt.Errorf("resolving value for %q: %w", path, err),
				Optional: m.optional,
			})
			continue
		}
		if err := SetPath(root, path, value); err != nil {
			errs = append(errs, &MutationError{
				Path:     path,
				Err:      fmt.Errorf("setting %q: %w", path, err),
				Optional: m.optional,
			})
			continue
		}
		diffs[path] = value
	}
	if len(diffs) == 0 {
		diffs = nil
	}
	return diffs, errs
}

// rawPathSource returns the original template source for a mutation's path,
// used in error reporting when the template fails to render.
func rawPathSource(t *template.Template) (string, bool) {
	if t == nil || t.Tree == nil || t.Tree.Root == nil {
		return "", false
	}
	return t.Tree.Root.String(), true
}

func renderTmpl(t *template.Template, data templateData) (string, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// resolveMutationValue is the value a mutation writes: the raw value, or the
// rendered text coerced as JSON would read it ("true" is a bool, "42" a
// number). Integer-shaped text keeps its integer type as int64, so a write
// that increments an int field does not flip it to a float.
func resolveMutationValue(m mutation, data templateData) (any, error) {
	if m.valueTmpl == nil {
		return m.rawValue, nil
	}
	text, err := renderTmpl(m.valueTmpl, data)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	if !strings.ContainsAny(trimmed, ".eE") {
		if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return n, nil
		}
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded, nil
	}
	return text, nil
}

// sameValue is a state value unchanged by a write: equal numbers whatever
// their Go type (an int beside an int64), else deep equality.
func sameValue(a, b any) bool {
	if isNumber(a) && isNumber(b) {
		fa, _, _ := toFloat(a)
		fb, _, _ := toFloat(b)
		return fa == fb
	}
	return reflect.DeepEqual(a, b)
}

func isNumber(v any) bool {
	switch v.(type) {
	case int, int32, int64, uint, uint32, uint64, float32, float64:
		return true
	}
	return false
}
