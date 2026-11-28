package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// FieldKind is what a control route reads a body field as.
type FieldKind int

const (
	// Choice is one of a fixed list of values, written as text.
	Choice FieldKind = iota
	// Number is a number: a JSON number, or text that reads as one.
	Number
	// Flag is a boolean: true or false, or the text "true" or "false".
	Flag
	// Text is a string written as sent, whatever it says, the typed form of a
	// route's mutation that lists no values; a Pattern refuses what does not
	// match. A body field that is not a string is refused.
	Text
)

// Field is one state path a control route sets from a field of its JSON body.
type Field struct {
	// Path is the state path written, Name the body field it comes from.
	Path, Name string
	Kind       FieldKind
	// Values are the values a Choice accepts, in the order the console offers
	// them.
	Values []string
	// Pattern is the expression a Text must match, as the engine's validation
	// names it; empty accepts any string.
	Pattern string
}

// Control is a POST route that sets state from fields of its JSON body and
// answers an acknowledgment built from the state it left: the typed form of a
// YAML route's mutations and response template. Every field is checked before
// any is written, so a refused request changes nothing.
type Control struct {
	Match, Summary, Example string
	Fields                  []Field
	// Ack is the document answered, read from the device after the writes.
	Ack func(d spec.Device) any
	// Indented answers Ack as an indented document, as a GET of the same
	// thing does, instead of the one-line acknowledgment.
	Indented bool
	// patterns are the Text fields' compiled expressions, by field name.
	patterns map[string]*regexp.Regexp
}

// Route is the control as a family route. The quick sets the console offers
// are the Choice fields, so the listing and the handler cannot differ.
func (c Control) Route() spec.HTTPRoute {
	var sets []profile.Set
	c.patterns = map[string]*regexp.Regexp{}
	for _, f := range c.Fields {
		switch f.Kind {
		case Choice:
			sets = append(sets, profile.Set{Path: f.Path, Field: f.Name, Values: append([]string(nil), f.Values...)})
		case Text:
			if f.Pattern != "" {
				c.patterns[f.Name] = regexp.MustCompile(f.Pattern)
			}
		}
	}
	return spec.HTTPRoute{Match: c.Match, Summary: c.Summary, Example: c.Example, Sets: sets, Handle: c.handle}
}

func (c Control) handle(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	// A body that was sent but is not a JSON object cannot be read as fields:
	// it is refused as such, not as a field that is missing, and nothing is
	// written. An empty body is a request with every field missing.
	if r.Body == nil && len(bytes.TrimSpace(r.Raw)) > 0 {
		return unreadable()
	}
	var changes []surface.Change
	var refused []refusedField
	for _, f := range c.Fields {
		raw := r.Body[f.Name]
		switch f.Kind {
		case Choice:
			text := fieldText(raw)
			if !contains(f.Values, text) {
				refused = append(refused, refusedField{Path: f.Path, Value: text, Enum: f.Values})
				continue
			}
			changes = append(changes, surface.Change{Path: f.Path, Value: text})
		case Number:
			n, ok := asNumber(raw)
			if !ok {
				return badField(f, "a number")
			}
			changes = append(changes, surface.Change{Path: f.Path, Value: n})
		case Flag:
			b, ok := asFlag(raw)
			if !ok {
				return badField(f, "true or false")
			}
			changes = append(changes, surface.Change{Path: f.Path, Value: b})
		case Text:
			text, ok := raw.(string)
			if !ok {
				return badField(f, "text")
			}
			if re := c.patterns[f.Name]; re != nil && !re.MatchString(text) {
				refused = append(refused, refusedField{Path: f.Path, Value: text, Pattern: f.Pattern})
				continue
			}
			changes = append(changes, surface.Change{Path: f.Path, Value: text})
		}
	}
	if len(refused) > 0 {
		return refusal(refused)
	}
	if err := d.WriteMany(changes...); err != nil {
		return spec.Failure(500, err.Error())
	}
	if c.Indented {
		return jsonResponse(Document(c.Ack(d)))
	}
	return jsonResponse(Spaced(c.Ack(d)))
}

// Action is a POST route that runs a fixed change and answers a fixed
// acknowledgment: a reboot, a factory reset.
type Action struct {
	Match, Summary, Example string
	// Writes are the state changes the action makes, read from the device (its
	// clock, its defaults) as it runs.
	Writes func(d spec.Device) []surface.Change
	Ack    any
}

// Route is the action as a family route.
func (a Action) Route() spec.HTTPRoute {
	return spec.HTTPRoute{Match: a.Match, Summary: a.Summary, Example: a.Example, Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
		if a.Writes != nil {
			if err := d.WriteMany(a.Writes(d)...); err != nil {
				return spec.Failure(500, err.Error())
			}
		}
		return jsonResponse(Spaced(a.Ack))
	}}
}

// Reset is a change that restores each path to the value the device was built
// with.
func Reset(d spec.Device, paths ...string) []surface.Change {
	out := make([]surface.Change, len(paths))
	for i, p := range paths {
		out[i] = surface.Change{Path: p, Value: d.Default(p)}
	}
	return out
}

func jsonResponse(b []byte) spec.HTTPResponse {
	return spec.HTTPResponse{ContentType: "application/json", Body: b}
}

type refusedField struct {
	Path    string   `json:"path"`
	Value   string   `json:"value"`
	Enum    []string `json:"enum,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
}

// refusal is the 400 a simulated device answers a value outside a fixed list
// with: the engine's validation body, naming each path, the value it was given
// and the values it takes.
func refusal(fields []refusedField) spec.HTTPResponse {
	body, _ := json.Marshal(struct {
		Error  string         `json:"error"`
		Fields []refusedField `json:"fields"`
	}{"validation failed", fields})
	return spec.HTTPResponse{Status: 400, ContentType: "application/json", Body: body}
}

func unreadable() spec.HTTPResponse {
	body, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{"the request body is not a JSON object"})
	return spec.HTTPResponse{Status: 400, ContentType: "application/json", Body: body}
}

func badField(f Field, want string) spec.HTTPResponse {
	body, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{fmt.Sprintf("%s must be %s", f.Name, want)})
	return spec.HTTPResponse{Status: 400, ContentType: "application/json", Body: body}
}

// fieldText is a body field as a refusal names it: text as itself, a missing
// field as <no value>, any other value as written.
func fieldText(v any) string {
	switch x := v.(type) {
	case nil:
		return "<no value>"
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// asNumber reads a body field as a number, keeping a whole one whole.
func asNumber(v any) (any, bool) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case string:
		p, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return nil, false
		}
		f = p
	default:
		return nil, false
	}
	if f == float64(int64(f)) && f > -1e15 && f < 1e15 {
		return int64(f), true
	}
	return f, true
}

func asFlag(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.TrimSpace(x) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
