package secret

import (
	"fmt"
	"regexp"
	"strings"
)

// Masked is the fixed placeholder a secret field renders as anywhere it is
// displayed (the resolve view, a log line, an error). The real value only ever
// materializes on the interpolation-into-a-request path or an audited read.
const Masked = "••••••"

// refPattern matches an interpolation token: $var:name[.path...] or
// $sec:name[.path...]. The name and each path segment are identifier-shaped
// (letters, digits, underscore, dash); dots separate the name from the path and
// the path segments from each other.
var refPattern = regexp.MustCompile(`\$(sec|var):([a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)*)`)

// Ref is one parsed interpolation token: its kind (sec|var), the value name, an
// optional dot-path into a structured value, and the exact source text.
type Ref struct {
	Kind string
	Name string
	Path []string
	Raw  string
}

// ParseRefs extracts every interpolation token from s, in order.
func ParseRefs(s string) []Ref {
	ms := refPattern.FindAllStringSubmatch(s, -1)
	out := make([]Ref, 0, len(ms))
	for _, m := range ms {
		parts := strings.Split(m[2], ".")
		ref := Ref{Kind: m[1], Name: parts[0], Raw: m[0]}
		if len(parts) > 1 {
			ref.Path = parts[1:]
		}
		out = append(out, ref)
	}
	return out
}

// Interpolate replaces every $var:/$sec: token in s with the string the resolve
// callback returns for it. A resolve error aborts the whole interpolation (a
// half-filled request is never emitted).
func Interpolate(s string, resolve func(Ref) (string, error)) (string, error) {
	var firstErr error
	out := refPattern.ReplaceAllStringFunc(s, func(tok string) string {
		if firstErr != nil {
			return tok
		}
		refs := ParseRefs(tok)
		if len(refs) != 1 {
			firstErr = fmt.Errorf("secret: malformed ref %q", tok)
			return tok
		}
		v, err := resolve(refs[0])
		if err != nil {
			firstErr = err
			return tok
		}
		return v
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// DotGet navigates a structured value by path, descending map[string]any /
// map[string]string at each step. An empty path returns the value itself.
func DotGet(v any, path []string) (any, error) {
	cur := v
	for i, seg := range path {
		switch m := cur.(type) {
		case map[string]any:
			next, ok := m[seg]
			if !ok {
				return nil, fmt.Errorf("secret: no field %q at %s", seg, strings.Join(path[:i+1], "."))
			}
			cur = next
		case map[string]string:
			next, ok := m[seg]
			if !ok {
				return nil, fmt.Errorf("secret: no field %q at %s", seg, strings.Join(path[:i+1], "."))
			}
			cur = next
		default:
			return nil, fmt.Errorf("secret: cannot descend into %s at %q", strings.Join(path[:i], "."), seg)
		}
	}
	return cur, nil
}

// MaskField renders a field's value for display: the value for a non-secret
// field, the Masked placeholder for a secret one. An unknown field fails closed
// (masked), so a shape/value drift never leaks a value.
func MaskField(shape Shape, field, value string) string {
	f, ok := shape.Field(field)
	if !ok || f.Secret {
		return Masked
	}
	return value
}
