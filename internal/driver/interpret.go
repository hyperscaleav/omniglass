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
	// The zone database rides the binary, so a payload time read in a
	// declared zone parses on a node whose image carries none.
	_ "time/tzdata"

	"github.com/hyperscaleav/omniglass/internal/logline"
)

// The compiled runtime unit and its interpreter (#814). Attach bakes each spec
// function into a task spec (internal/storage); the node parses that spec back
// into a BakedFunction and interprets fetched payloads against it: locate each
// emit, apply its declared transform, type it by its baked lane. Interpretation
// is pure (payload in, samples and faults out); fetching lives with the
// transport clients in internal/collection.

// BakedEmit is one emit with its lane resolved at attach: the compile step,
// so interpretation never consults a catalog. A log line's lane is "log" and
// it has no name. The fields beside the extract (#1145) are omitted when
// unset, so a spec that declares none bakes to the bytes it always did and
// its task keeps its content hash.
type BakedEmit struct {
	Name       string           `json:"name,omitempty"`
	Lane       string           `json:"lane"`
	Extract    Extract          `json:"extract,omitzero"`
	Transform  *Transform       `json:"transform,omitempty"`
	Message    *Field           `json:"message,omitempty"`
	Attributes map[string]Field `json:"attributes,omitempty"`
	Log        *LogEmit         `json:"log,omitempty"`
	TS         *TimeField       `json:"ts,omitempty"`
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
	// Unmatched rides listen tasks: the spec's unmatched-line policy (#817).
	Unmatched string `json:"unmatched,omitempty"`
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
// Text for regex capture, JSON for path extraction, and Body in its declared
// Format (with the Namespaces its xpaths bind) for an xpath over an xml or
// html answer (#902). A fetcher fills the forms its transport produces and
// leaves the rest zero.
type Payload struct {
	Values     map[string]string
	Text       string
	JSON       []byte
	Body       []byte
	Format     string
	Namespaces map[string]string
	// docs caches the parsed documents, so a payload is parsed once however
	// many emits read it.
	docs *parsedDocs
}

// HTTPPayload is an http answer's body in every form its request's format
// admits: always the text a regex reads, the JSON a jsonpath reads when the
// format is json, and the body an xpath reads when it is xml or html.
func HTTPPayload(body []byte, req *Request) Payload {
	p := Payload{Text: string(body), Body: body, Format: FormatJSON}
	if req != nil {
		p.Format, p.Namespaces = req.FormatOf(), req.Namespaces
	}
	if p.Format == FormatJSON {
		p.JSON = body
	}
	return p
}

// Emitted is one interpreted emit: its catalog name (none for a log line),
// its baked lane, and the typed value (Value on the metric lane, Text
// elsewhere: a property's value, an event's or a log line's message). An
// event's payload and a log line's attributes ride Attributes as a JSON
// object; a log line's severity (canonical), facility and source beside
// them. Clamped marks a payload time ahead of arrival past the window,
// landed at arrival instead.
type Emitted struct {
	Name       string
	Lane       string
	Value      float64
	Text       string
	IsText     bool
	TS         time.Time
	Attributes json.RawMessage
	Severity   string
	Facility   string
	Source     string
	Clamped    bool
}

// ClampWindow is how far ahead of its arrival a payload's time may claim to
// be before it lands at arrival instead (#1145 decision 3): the node-probe
// cap's figure (ADR-0161 point 7).
const ClampWindow = 5 * time.Minute

// Interpret locates and transforms every emit against one payload. Emits that
// resolve land in the returned samples; each one that does not is a fault
// naming the emit, so the caller lands a collection-failed event rather than
// silently dropping it or shipping a wrong-lane sample. The two lists are
// independent: a payload missing one emit still lands the rest.
func (fn *BakedFunction) Interpret(p Payload, ts time.Time) ([]Emitted, []error) {
	if p.docs == nil {
		p.docs = &parsedDocs{}
	}
	var out []Emitted
	var faults []error
	for _, em := range fn.Emits {
		e, landed, err := interpretEmit(em, p, ts)
		if err != nil {
			faults = append(faults, fmt.Errorf("driver: %s/%s emit %q: %w", fn.Driver, fn.Function, em.label(), err))
		}
		if landed {
			out = append(out, e)
		}
	}
	return out, faults
}

// label names a baked emit in a fault: its key, or "log" for a log line.
func (em BakedEmit) label() string {
	if em.Lane == LaneLog && em.Name == "" {
		return "log"
	}
	return em.Name
}

// interpretEmit reads one emit off a payload. landed reports an emit that
// lands; err is a fault, which may come with a landed emit (a payload time
// that does not parse lands the record at arrival and says so).
func interpretEmit(em BakedEmit, p Payload, arrival time.Time) (Emitted, bool, error) {
	if em.Lane != LaneLog {
		if err := emittable(em.Name); err != nil {
			return Emitted{}, false, err
		}
	}
	ts, claimed, tsErr := emitTime(em.TS, p, arrival)
	clamped := claimed != ""
	if em.Lane == LaneEvent {
		e, err := interpretEvent(em, p)
		if err != nil {
			return Emitted{}, false, err
		}
		e.TS, e.Clamped = ts, clamped
		return e, true, tsErr
	}
	if em.Lane == LaneLog {
		e, missed := interpretLogLine(em, p)
		e.TS, e.Clamped = ts, clamped
		if clamped {
			e.Attributes = mergeAttributes(e.Attributes, map[string]any{"ts_claimed": claimed})
		}
		e.Text, e.Attributes = logline.Cap(e.Text, e.Attributes)
		if tsErr != nil {
			missed = append(missed, tsErr.Error())
		}
		if len(missed) > 0 {
			return e, true, fmt.Errorf("the line landed with %s", strings.Join(missed, "; "))
		}
		return e, true, nil
	}
	e, err := interpretSample(em, p)
	if err != nil {
		return Emitted{}, false, err
	}
	e.TS, e.Clamped = ts, clamped
	return e, true, tsErr
}

// interpretSample reads a metric or a property: locate, transform, type.
func interpretSample(em BakedEmit, p Payload) (Emitted, error) {
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

	e := Emitted{Name: em.Name, Lane: em.Lane}
	if em.Lane == LaneMetric {
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
	text, err := numericText(raw, em.Transform)
	if err != nil {
		return Emitted{}, err
	}
	e.Text, e.IsText = text, true
	return e, nil
}

// numericText applies a text lane's scale and int cast to a raw value (the
// map has already run): a scale or an int cast means the value is numeric,
// so it is parsed, shaped, and rendered back.
func numericText(raw string, tr *Transform) (string, error) {
	if tr != nil && tr.Scale != 0 {
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return "", fmt.Errorf("value %q is not numeric, and the transform declares a scale", raw)
		}
		raw = strconv.FormatFloat(v*tr.Scale, 'f', -1, 64)
	}
	if tr != nil && tr.Cast == "int" {
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return "", fmt.Errorf("value %q is not numeric, and the transform declares an int cast", raw)
		}
		raw = strconv.FormatInt(int64(math.Trunc(v)), 10)
	}
	return raw, nil
}

// interpretEvent reads an event (#1145): its message (a constant or an
// extract, by default the whole payload text) and its attributes, rendered
// into its payload. An event is a semantic assertion, so a field that does
// not locate lands nothing, as a metric that does not locate lands nothing.
func interpretEvent(em BakedEmit, p Payload) (Emitted, error) {
	e := Emitted{Name: em.Name, Lane: em.Lane, IsText: true, Text: p.Text}
	if em.Message != nil {
		text, _, err := em.Message.resolve(p)
		if err != nil {
			return Emitted{}, fmt.Errorf("message: %w", err)
		}
		e.Text = text
	}
	attrs := map[string]any{}
	for _, k := range sortedKeys(em.Attributes) {
		_, typed, err := em.Attributes[k].resolve(p)
		if err != nil {
			return Emitted{}, fmt.Errorf("attribute %q: %w", k, err)
		}
		attrs[k] = typed
	}
	if len(attrs) > 0 {
		e.Attributes = mustJSON(attrs)
	}
	if e.Text == "" && len(attrs) == 0 {
		return Emitted{}, fmt.Errorf("the event carries no message and no attributes, so it says nothing")
	}
	return e, nil
}

// interpretLogLine reads a log line (#1145): its message, attributes,
// severity, facility and source. A log line is raw arrival, so it always
// lands: a message extract that misses falls back to the whole payload text,
// and a severity, facility or attribute that misses is left out. missed
// names every field that did, which the caller reports as a fault.
func interpretLogLine(em BakedEmit, p Payload) (Emitted, []string) {
	e := Emitted{Lane: LaneLog, IsText: true, Text: p.Text}
	var missed []string
	if em.Message != nil {
		if text, _, err := em.Message.resolve(p); err != nil {
			missed = append(missed, "its whole text, its message missing: "+err.Error())
		} else {
			e.Text = text
		}
	}
	attrs := map[string]any{}
	for _, k := range sortedKeys(em.Attributes) {
		_, typed, err := em.Attributes[k].resolve(p)
		if err != nil {
			missed = append(missed, fmt.Sprintf("no attribute %q: %v", k, err))
			continue
		}
		attrs[k] = typed
	}
	if len(attrs) > 0 {
		e.Attributes = mustJSON(attrs)
	}
	if em.Log != nil {
		if em.Log.Severity != nil {
			if word, _, err := em.Log.Severity.resolve(p); err != nil {
				missed = append(missed, "no severity: "+err.Error())
			} else {
				e.Severity, e.Attributes = logline.Normalize(word, e.Attributes)
			}
		}
		if em.Log.Facility != nil {
			if fac, _, err := em.Log.Facility.resolve(p); err != nil {
				missed = append(missed, "no facility: "+err.Error())
			} else {
				e.Facility = fac
			}
		}
		e.Source = em.Log.Source
	}
	return e, missed
}

// resolve reads a field: its constant, or its extract located in the
// payload, shaped by its transform (the map, then the scale and int cast a
// text lane applies). typed is the value as a payload carries it: a JSON
// number for a cast to int or float, a string otherwise.
func (f Field) resolve(p Payload) (string, any, error) {
	raw := f.Value
	if f.Extract != nil {
		v, err := locate(*f.Extract, p)
		if err != nil {
			return "", nil, err
		}
		raw = v
	}
	tr := f.Transform
	if tr != nil && len(tr.Map) > 0 {
		mapped, ok := tr.Map[raw]
		if !ok {
			return "", nil, fmt.Errorf("value %q has no entry in the declared map", raw)
		}
		raw = mapped
	}
	text, err := numericText(raw, tr)
	if err != nil {
		return "", nil, err
	}
	if tr != nil && (tr.Cast == "int" || tr.Cast == "float") {
		v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return "", nil, fmt.Errorf("value %q is not numeric, and the transform declares a %s cast", text, tr.Cast)
		}
		return text, v, nil
	}
	return text, text, nil
}

// emitTime is an emit's time: arrival when it declares none, else the
// payload's time in its layout. claimed is set (the time as the payload
// stated it) when that time is past ClampWindow ahead of arrival, which
// lands the record at arrival instead. A time that does not parse lands the
// record at arrival and is a fault naming the layout.
func emitTime(tf *TimeField, p Payload, arrival time.Time) (ts time.Time, claimed string, err error) {
	if tf == nil {
		return arrival, "", nil
	}
	raw, err := locate(tf.Extract, p)
	if err != nil {
		return arrival, "", fmt.Errorf("ts: %w", err)
	}
	t, err := parseTime(strings.TrimSpace(raw), tf.Layout, tf.Zone, arrival)
	if err != nil {
		return arrival, "", fmt.Errorf("ts %q does not parse as %s: %w", raw, tf.Layout, err)
	}
	if t.After(arrival.Add(ClampWindow)) {
		return arrival, t.UTC().Format(time.RFC3339Nano), nil
	}
	return t.UTC(), "", nil
}

// The payload time layouts.
const (
	LayoutRFC3339 = "rfc3339"
	LayoutRFC3164 = "rfc3164"
	LayoutUnix    = "unix"
	LayoutUnixMS  = "unix-ms"
	layoutGo      = "go:"
)

// checkLayout refuses a layout the interpreter cannot read: one of the named
// layouts, or go: and a Go reference layout that reads back what it writes.
func checkLayout(layout string) error {
	switch layout {
	case LayoutRFC3339, LayoutRFC3164, LayoutUnix, LayoutUnixMS:
		return nil
	}
	l, ok := strings.CutPrefix(layout, layoutGo)
	if !ok || l == "" {
		return fmt.Errorf("layout %q is not one of rfc3339, rfc3164, unix, unix-ms or go:<a Go reference layout>", layout)
	}
	ref := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)
	if _, err := time.Parse(l, ref.Format(l)); err != nil {
		return fmt.Errorf("layout %q does not read back what it writes: %v", layout, err)
	}
	return nil
}

// parseTime reads raw in a layout. A time with no year (rfc3164, or a Go
// layout without one) takes the year that puts it nearest arrival, so a
// line from 31 December read on 1 January lands in the year it was written;
// a time with no zone is read in zone (UTC when empty).
func parseTime(raw, layout, zone string, arrival time.Time) (time.Time, error) {
	loc := time.UTC
	if zone != "" {
		l, err := time.LoadLocation(zone)
		if err != nil {
			return time.Time{}, err
		}
		loc = l
	}
	switch layout {
	case LayoutRFC3339:
		return time.Parse(time.RFC3339Nano, raw)
	case LayoutRFC3164:
		t, err := time.ParseInLocation("Jan 2 15:04:05", strings.Join(strings.Fields(raw), " "), loc)
		if err != nil {
			return time.Time{}, err
		}
		return nearestYear(t, arrival, loc), nil
	case LayoutUnix:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return time.Time{}, err
		}
		sec, frac := math.Modf(f)
		return time.Unix(int64(sec), int64(math.Round(frac*1e9))).UTC(), nil
	case LayoutUnixMS:
		ms, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.UnixMilli(ms).UTC(), nil
	}
	l, _ := strings.CutPrefix(layout, layoutGo)
	t, err := time.ParseInLocation(l, raw, loc)
	if err != nil {
		return time.Time{}, err
	}
	if t.Year() == 0 {
		t = nearestYear(t, arrival, loc)
	}
	return t, nil
}

// nearestYear places a yearless time in the year (arrival's, or one either
// side) that puts it nearest arrival.
func nearestYear(t, arrival time.Time, loc *time.Location) time.Time {
	y := arrival.In(loc).Year()
	best := time.Time{}
	for _, cand := range []int{y - 1, y, y + 1} {
		c := time.Date(cand, t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
		if best.IsZero() || absDuration(c.Sub(arrival)) < absDuration(best.Sub(arrival)) {
			best = c
		}
	}
	return best
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// mergeAttributes adds fields to a JSON object of attributes.
func mergeAttributes(attrs json.RawMessage, extra map[string]any) json.RawMessage {
	m := map[string]any{}
	if len(attrs) > 0 {
		_ = json.Unmarshal(attrs, &m)
	}
	for k, v := range extra {
		m[k] = v
	}
	return mustJSON(m)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // maps of strings, numbers and booleans always encode
	}
	return b
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
		re, err := compiled(ex.Regex)
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
	case ex.XPath != "":
		return xpathValue(p, ex.XPath)
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
	return RenderRequestTyped(req, value, nil, params)
}

// RenderRequestTyped is RenderRequest with the intended value's own JSON
// (typed): an http JSON body leaf that is exactly ${value} takes it with its
// type (an intended metric's number), where any other placement renders the
// text (#902). nil typed renders the text everywhere.
func RenderRequestTyped(req Request, value string, typed json.RawMessage, params map[string]any) (Request, error) {
	if req.hasHTTPFields() {
		return renderHTTP(req, value, typed, params)
	}
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
	return Request{Get: req.Get, Line: line, Path: path, Reply: req.Reply, Retries: req.Retries}, nil
}
