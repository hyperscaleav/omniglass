package driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The compiled runtime unit and its interpreter (#814). Attach bakes each spec
// function into a task spec (internal/storage); the node parses that spec back
// into a BakedFunction and interprets fetched payloads against it: locate each
// emit, apply its declared transform, type it by its baked lane. Interpretation
// is pure (payload in, samples and faults out); fetching lives with the
// transport clients in internal/collection.

// BakedEmit is one emit with its lane resolved at attach: the compile step,
// so interpretation never consults a catalog.
type BakedEmit struct {
	Name      string     `json:"name"`
	Lane      string     `json:"lane"`
	Extract   Extract    `json:"extract"`
	Transform *Transform `json:"transform,omitempty"`
}

// BakedFunction is one derived task's spec: the driver function carried whole.
// Schedule and Request ride poll tasks; Arm and Match ride listen tasks.
type BakedFunction struct {
	Driver   string      `json:"driver"`
	Version  string      `json:"version,omitempty"`
	Function string      `json:"function"`
	Schedule *Schedule   `json:"schedule,omitempty"`
	Request  *Request    `json:"request,omitempty"`
	Arm      []string    `json:"arm,omitempty"`
	Match    *Match      `json:"match,omitempty"`
	Emits    []BakedEmit `json:"emits"`
}

// ParseBaked decodes a derived task's spec. Strict like Parse: the writer is
// the attach path, so an unknown field here is corruption, not authoring.
func ParseBaked(raw []byte) (*BakedFunction, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var fn BakedFunction
	if err := dec.Decode(&fn); err != nil {
		return nil, fmt.Errorf("driver: parse baked function: %w", err)
	}
	return &fn, nil
}

// IsBaked reports whether a task spec is a baked driver function, which is
// what tells a driver task from the reachability probe's bare "{}" spec.
func IsBaked(raw []byte) bool {
	var probe struct {
		Driver string `json:"driver"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Driver != ""
}

// Payload is one fetched response in the forms extractors read: Values for
// keyed lookups (OID to value for SNMP, key to value for a key-value body),
// Text for regex capture, JSON for path extraction. A fetcher fills the forms
// its transport produces and leaves the rest zero.
type Payload struct {
	Values map[string]string
	Text   string
	JSON   []byte
}

// Emitted is one interpreted sample: the emit's catalog name, its baked lane,
// and the typed value (Value on the metric lane, Text elsewhere).
type Emitted struct {
	Name   string
	Lane   string
	Value  float64
	Text   string
	IsText bool
	TS     time.Time
}

// Interpret locates and transforms every emit against one payload. Emits that
// resolve land in the returned samples; each one that does not is a fault
// naming the emit, so the caller lands a collection-failed event rather than
// silently dropping it or shipping a wrong-lane sample. The two lists are
// independent: a payload missing one emit still lands the rest.
func (fn *BakedFunction) Interpret(p Payload, ts time.Time) ([]Emitted, []error) {
	var out []Emitted
	var faults []error
	for _, em := range fn.Emits {
		e, err := interpretEmit(em, p, ts)
		if err != nil {
			faults = append(faults, fmt.Errorf("driver: %s/%s emit %q: %w", fn.Driver, fn.Function, em.Name, err))
			continue
		}
		out = append(out, e)
	}
	return out, faults
}

func interpretEmit(em BakedEmit, p Payload, ts time.Time) (Emitted, error) {
	raw, err := locate(em.Extract, p)
	if err != nil {
		return Emitted{}, err
	}
	if em.Transform != nil && len(em.Transform.Map) > 0 {
		mapped, ok := em.Transform.Map[raw]
		if !ok {
			return Emitted{}, fmt.Errorf("value %q has no entry in the declared map", raw)
		}
		raw = mapped
	}

	e := Emitted{Name: em.Name, Lane: em.Lane, TS: ts}
	if em.Lane == "metric" {
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return Emitted{}, fmt.Errorf("value %q is not numeric, and the metric lane carries numbers", raw)
		}
		if em.Transform != nil && em.Transform.Scale != 0 {
			v *= em.Transform.Scale
		}
		if em.Transform != nil && em.Transform.Cast == "int" {
			v = math.Trunc(v)
		}
		e.Value = v
		return e, nil
	}

	// Every other lane carries the value as text. A scale on a text lane
	// still means "this is numeric": parse, scale, and render it back.
	if em.Transform != nil && em.Transform.Scale != 0 {
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return Emitted{}, fmt.Errorf("value %q is not numeric, and the transform declares a scale", raw)
		}
		raw = strconv.FormatFloat(v*em.Transform.Scale, 'f', -1, 64)
	}
	if em.Transform != nil && em.Transform.Cast == "int" {
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return Emitted{}, fmt.Errorf("value %q is not numeric, and the transform declares an int cast", raw)
		}
		raw = strconv.FormatInt(int64(math.Trunc(v)), 10)
	}
	e.Text, e.IsText = raw, true
	return e, nil
}

// locate finds an emit's raw value in the payload by its one extractor.
func locate(ex Extract, p Payload) (string, error) {
	switch {
	case ex.OID != "":
		// The getter normalizes a response OID by stripping a leading dot, so
		// strip it on the lookup side too: a spec authored in the standard MIB
		// ".1.3.6..." form matches, not only the no-dot form.
		v, ok := p.Values[strings.TrimPrefix(ex.OID, ".")]
		if !ok {
			return "", fmt.Errorf("the response carries no value for OID %s", ex.OID)
		}
		return v, nil
	case ex.Key != "":
		v, ok := p.Values[ex.Key]
		if !ok {
			return "", fmt.Errorf("the response carries no value for key %q", ex.Key)
		}
		return v, nil
	case ex.Regex != "":
		re, err := regexp.Compile(ex.Regex)
		if err != nil {
			return "", fmt.Errorf("uncompilable regex: %v", err)
		}
		m := re.FindStringSubmatch(p.Text)
		if m == nil {
			return "", fmt.Errorf("regex %q does not match the response", ex.Regex)
		}
		if len(m) > 1 {
			return m[1], nil
		}
		return m[0], nil
	case ex.JSONPath != "":
		return jsonPath(p.JSON, ex.JSONPath)
	default:
		return "", fmt.Errorf("no extractor declared")
	}
}

// jsonPath walks a dotted path into a JSON document (the declared thin cut of
// JSONPath: object keys only, no filters or wildcards) and renders the leaf as
// a string.
func jsonPath(doc []byte, path string) (string, error) {
	if len(doc) == 0 {
		return "", fmt.Errorf("the response carries no JSON body")
	}
	var cur any
	if err := json.Unmarshal(doc, &cur); err != nil {
		return "", fmt.Errorf("the response is not JSON: %v", err)
	}
	for _, step := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("path %q steps through a non-object", path)
		}
		cur, ok = obj[step]
		if !ok {
			return "", fmt.Errorf("path %q has no key %q", path, step)
		}
	}
	switch v := cur.(type) {
	case string:
		return v, nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		return "", fmt.Errorf("path %q lands on a non-scalar", path)
	}
}

// argPattern matches the request template's references: ${value} is the
// command's intended value, ${arg.key} a key of its params.
var argPattern = regexp.MustCompile(`\$\{([a-zA-Z0-9_.-]+)\}`)

// substituted renders one template value to a string and refuses a control
// character in it: a line protocol frames on \r\n, so a substituted value
// carrying a terminator would inject a second, unbound line onto the wire (a
// command outside the driver menu, invisible to the audit trail). A JSON number
// param renders without %v's %g exponent form, so a large or fractional preset
// is a plain decimal, never "1e+06"; an explicit JSON null is refused like an
// absent reference rather than rendering "<nil>".
func substituted(v any) (string, error) {
	var s string
	switch t := v.(type) {
	case nil:
		return "", fmt.Errorf("is null")
	case string:
		s = t
	case json.Number:
		s = t.String()
	case float64:
		s = strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		s = strconv.FormatBool(t)
	default:
		s = fmt.Sprintf("%v", t)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("carries a control character")
		}
	}
	return s, nil
}

// RenderRequest resolves a command binding's request template against one
// issued command: ${value} takes the intended value, ${arg.key} a param.
// A reference nothing supplies, or a value carrying a control character,
// refuses, so a half-rendered or injected line is never sent to a device.
func RenderRequest(req Request, value string, params map[string]any) (Request, error) {
	render := func(s string) (string, error) {
		var fault error
		out := argPattern.ReplaceAllStringFunc(s, func(m string) string {
			ref := m[2 : len(m)-1]
			if ref == "value" {
				if value == "" {
					fault = fmt.Errorf("driver: request references ${value}, and the command carries none")
					return m
				}
				rendered, err := substituted(value)
				if err != nil {
					fault = fmt.Errorf("driver: request's ${value} %v", err)
					return m
				}
				return rendered
			}
			if key, ok := strings.CutPrefix(ref, "arg."); ok {
				v, present := params[key]
				if !present {
					fault = fmt.Errorf("driver: request references ${arg.%s}, and the command's params carry no %q", key, key)
					return m
				}
				rendered, err := substituted(v)
				if err != nil {
					fault = fmt.Errorf("driver: request's ${arg.%s} %v", key, err)
					return m
				}
				return rendered
			}
			fault = fmt.Errorf("driver: request references unknown template field %q", ref)
			return m
		})
		return out, fault
	}
	line, err := render(req.Line)
	if err != nil {
		return Request{}, err
	}
	path, err := render(req.Path)
	if err != nil {
		return Request{}, err
	}
	return Request{Get: req.Get, Line: line, Path: path}, nil
}
