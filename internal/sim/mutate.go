package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// mutation is a pre-compiled state write. path is always a template so
// authors can interpolate request fields ({{ .Request.Body.index }})
// into the target path. valueTmpl is non-nil when the YAML value is a
// string (and thus subject to template expansion + JSON coercion);
// rawValue carries non-string YAML scalars (bool, int, nested maps)
// through unchanged.
//
// enum and pattern carry per-mutation validation declared in the
// list-of-objects mutation shape. enum, when non-empty, requires the
// substituted value (post-template, pre-JSON-coercion) to match one of
// the listed strings case-sensitively. pattern, when non-nil, is a
// regex compiled at profile-load time so per-request work is just one
// MatchString call.
type mutation struct {
	path      *template.Template
	valueTmpl *template.Template
	rawValue  any
	enum      []string
	pattern   *regexp.Regexp
	// patternSrc is the original regex source kept for error messages
	// so operators see what they wrote, not the compiled internal form.
	patternSrc string
	// optional marks the mutation as best-effort. When true, a failure
	// logs a warning but does not cause the route handler to return 400.
	// Default (false) is fail-closed: any failure surfaces to the client.
	optional bool
}

// ValidationError is returned by applyMutations when a mutation's
// substituted value fails an enum or pattern check declared in the
// profile. The route handler distinguishes these from generic mutation
// errors (template render failure, bad path, etc.) so it can render a
// validation-specific 400 body listing the offending fields.
type ValidationError struct {
	Path    string   // rendered state path the mutation targeted
	Value   string   // substituted value that failed validation
	Enum    []string // enum the value was checked against (nil if not set)
	Pattern string   // regex source (empty if not set)
}

// MutationError is the per-mutation failure wrapper returned by
// applyMutations. It carries the rendered state path, the underlying
// error (which may itself be a *ValidationError), and the Optional flag
// pulled from the mutation declaration. The route handler uses Optional
// to decide whether the failure should produce a 400 (required) or just
// a logged warning (optional). Path is the best-effort rendered path:
// if path-template rendering itself failed, Path holds the raw path
// template source so operators can still trace back to the profile.
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

func (e *ValidationError) Error() string {
	switch {
	case len(e.Enum) > 0 && e.Pattern != "":
		return fmt.Sprintf("mutation %q: value %q does not match enum %v or pattern %q", e.Path, e.Value, e.Enum, e.Pattern)
	case len(e.Enum) > 0:
		return fmt.Sprintf("mutation %q: value %q not in enum %v", e.Path, e.Value, e.Enum)
	case e.Pattern != "":
		return fmt.Sprintf("mutation %q: value %q does not match pattern %q", e.Path, e.Value, e.Pattern)
	default:
		return fmt.Sprintf("mutation %q: value %q failed validation", e.Path, e.Value)
	}
}

// compileMutations turns a profile's raw mutate map into pre-compiled
// mutations. Returns nil for empty input so the per-request fast path
// can skip all mutate work with a simple len() check. The legacy map
// shape carries no validation knobs and no optional flag; every entry
// is required. Use compileMutationList for the list-of-objects shape
// that supports enum, pattern, and optional.
func compileMutations(raw map[string]any) ([]mutation, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]mutation, 0, len(raw))
	for path, value := range raw {
		m, err := compileOne(path, value, nil, "", false)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// compileMutationList turns the list-of-objects mutation shape into
// pre-compiled mutations. Each item may declare enum, pattern, and / or
// optional; all three are optional and may be combined. Pattern regex
// is compiled here (once at profile load), not per-request.
func compileMutationList(items []profile.Mutation) ([]mutation, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]mutation, 0, len(items))
	for _, item := range items {
		if item.Path == "" {
			return nil, fmt.Errorf("mutation missing required field 'path'")
		}
		m, err := compileOne(item.Path, item.Value, item.Enum, item.Pattern, item.Optional)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// compileMerged combines the legacy map shape with the list-of-objects
// shape into a single ordered slice. List items run first (their
// declaration order is preserved); legacy map entries run after in
// arbitrary order. Both nil produces nil.
func compileMerged(raw map[string]any, items []profile.Mutation) ([]mutation, error) {
	if len(raw) == 0 && len(items) == 0 {
		return nil, nil
	}
	listMuts, err := compileMutationList(items)
	if err != nil {
		return nil, err
	}
	mapMuts, err := compileMutations(raw)
	if err != nil {
		return nil, err
	}
	out := make([]mutation, 0, len(listMuts)+len(mapMuts))
	out = append(out, listMuts...)
	out = append(out, mapMuts...)
	return out, nil
}

// compileOne builds a single mutation using the package-wide
// templateFuncs. Routes call this via compileMutations /
// compileMutationList; behaviors call compileOneFuncs with a richer
// FuncMap that adds device-bound helpers (randStep, etc).
func compileOne(path string, value any, enum []string, pattern string, optional bool) (mutation, error) {
	return compileOneFuncs(path, value, enum, pattern, optional, templateFuncs)
}

// compileOneFuncs builds a single mutation against an explicit FuncMap
// so behaviors can supply per-device closures (e.g. a randStep bound to
// a device-seeded *rand.Rand) without forking the rest of the mutate
// pipeline. Path is always a template; value may be a string (template)
// or any other YAML scalar (passed through). enum / pattern are
// optional validators applied after template substitution. optional
// marks the mutation as best-effort so a runtime failure logs but does
// not block the request.
func compileOneFuncs(path string, value any, enum []string, pattern string, optional bool, funcs template.FuncMap) (mutation, error) {
	pathT, err := template.New("mutate-path").Funcs(funcs).Parse(path)
	if err != nil {
		return mutation{}, fmt.Errorf("parsing mutate path %q: %w", path, err)
	}
	m := mutation{path: pathT, enum: enum, patternSrc: pattern, optional: optional}
	if s, ok := value.(string); ok {
		valT, err := template.New("mutate-value").Funcs(funcs).Parse(s)
		if err != nil {
			return mutation{}, fmt.Errorf("parsing mutate value %q: %w", s, err)
		}
		m.valueTmpl = valT
	} else {
		m.rawValue = value
	}
	if pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return mutation{}, fmt.Errorf("compiling mutate pattern %q: %w", pattern, err)
		}
		m.pattern = re
	}
	return m, nil
}

// applyMutations writes every mutation into root. Runs under the
// device's state lock; callers must hold it. Returns the subset of
// mutations that actually applied (rendered path → stored value),
// which is exactly the diff the caller needs to ship upstream. Errors
// are accumulated per mutation as *MutationError but do not abort the
// batch: a malformed request body should leave state untouched for that
// one mutation rather than poison the whole batch.
//
// Each returned error wraps the underlying cause (path render failure,
// value render failure, validation failure, state write failure) and
// carries the mutation's Optional flag so the caller can decide whether
// a failure should fail the request closed (default) or only log.
func applyMutations(root map[string]any, muts []mutation, data templateData) (diffs map[string]any, errs []error) {
	if len(muts) == 0 {
		return nil, nil
	}
	diffs = make(map[string]any, len(muts))
	for _, m := range muts {
		path, err := renderTmpl(m.path, data)
		if err != nil {
			// Path template failed before we could render a path. Use
			// the raw path-template source so operators still see what
			// profile entry blew up, and so the error body is not blank.
			rawPath, _ := rawPathSource(m.path)
			errs = append(errs, &MutationError{
				Path:     rawPath,
				Err:      fmt.Errorf("rendering path: %w", err),
				Optional: m.optional,
			})
			continue
		}
		rendered, err := renderValue(m, data)
		if err != nil {
			errs = append(errs, &MutationError{
				Path:     path,
				Err:      fmt.Errorf("resolving value for %q: %w", path, err),
				Optional: m.optional,
			})
			continue
		}
		if vErr := validateRendered(path, rendered, m); vErr != nil {
			errs = append(errs, &MutationError{
				Path:     path,
				Err:      vErr,
				Optional: m.optional,
			})
			continue
		}
		value := coerceValue(m, rendered)
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

// rawPathSource returns the original template source for a mutation's
// path, used in error reporting when the template fails to render. Best
// effort: if the template is nil or has no parse tree, returns "" and
// false so the caller knows to fall back.
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

// renderValue returns the substituted-but-uncoerced text of a string
// mutation, or a sentinel for non-string raw values. Validation runs
// against the string form; coerceValue does the JSON-decode step that
// turns "true" into bool, etc.
//
// The bool return is true when the mutation produced a string we can
// validate (template-rendered or empty). It is false for raw non-string
// YAML scalars: enum / pattern do not apply to those, since they are
// authored values, not request-derived.
func renderValue(m mutation, data templateData) (rendered renderedValue, err error) {
	if m.valueTmpl == nil {
		return renderedValue{raw: m.rawValue, isString: false}, nil
	}
	s, err := renderTmpl(m.valueTmpl, data)
	if err != nil {
		return renderedValue{}, err
	}
	return renderedValue{text: s, isString: true}, nil
}

// renderedValue carries the post-substitution mutation result through
// the validation step. text is the rendered template text (only valid
// when isString is true); raw carries non-string YAML values that
// bypass both validation and JSON coercion.
type renderedValue struct {
	text     string
	raw      any
	isString bool
}

// validateRendered runs enum and pattern checks against a string-typed
// mutation. Non-string mutations (raw YAML scalars) bypass validation:
// they are author-supplied, not request-derived, so request-validation
// has nothing to guard against.
func validateRendered(path string, r renderedValue, m mutation) error {
	if !r.isString {
		return nil
	}
	if len(m.enum) == 0 && m.pattern == nil {
		return nil
	}
	enumOK := len(m.enum) == 0
	if !enumOK {
		for _, allowed := range m.enum {
			if r.text == allowed {
				enumOK = true
				break
			}
		}
	}
	patternOK := m.pattern == nil || m.pattern.MatchString(r.text)
	if enumOK && patternOK {
		return nil
	}
	return &ValidationError{
		Path:    path,
		Value:   r.text,
		Enum:    m.enum,
		Pattern: m.patternSrc,
	}
}

// coerceValue applies the JSON-decode coercion that turns "true" → bool,
// "42" → number, etc. Kept separate from the validation step so enum /
// pattern see the raw request text rather than a decoded form.
//
// Integer-shaped renders ("5", "-12") preserve their integer type as
// int64 so a mutation that increments an int field doesn't silently
// flip it to float64, json.Unmarshal would otherwise decode every
// number to float64, and a snapshot serializer downstream would then
// emit "5.0" where the profile declared "5".
func coerceValue(m mutation, r renderedValue) any {
	if !r.isString {
		return r.raw
	}
	trimmed := strings.TrimSpace(r.text)
	if trimmed == "" {
		return ""
	}
	if !strings.ContainsAny(trimmed, ".eE") {
		if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return n
		}
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}
	return r.text
}

// resolveValue is retained for the existing test that exercises the
// JSON-coercion fast path. New callers should use renderValue +
// validateRendered + coerceValue.
func resolveValue(m mutation, data templateData) (any, error) {
	r, err := renderValue(m, data)
	if err != nil {
		return nil, err
	}
	return coerceValue(m, r), nil
}
