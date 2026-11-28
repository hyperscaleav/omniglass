package httpapi

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// Element is a POST route that sets fields of one element of a list in state,
// the element named by a whole-number field of the body: {"index": 2, "mute":
// true} writes <List>.2.mute. It is the typed form of a YAML route whose
// mutation path is templated from the body (a mixer's channel strips, a
// lighting controller's zones).
//
// The index is read as the text a templated path would carry, so a text "2" and
// a whole 2.0 address the same element. One that names no element (missing,
// null, a word, a boolean, empty, negative, past the end) is refused with the
// mutation-failed document, naming the path and why the state cannot take it, and
// nothing is written. One with a decimal point (1.5) named a key no element has
// and is refused as unreadable. Each field is checked before any is written.
type Element struct {
	Match, Summary, Example string
	// IndexField is the body field naming the element, List the state path of
	// the list.
	IndexField, List string
	// Fields are the element's fields the route sets; each Path is the field's
	// name inside the element ("mute"). A Choice is not an element field.
	Fields []Field
	// Ack is the document answered, read from the device after the writes.
	Ack func(d spec.Device) any
}

// Route is the element control as a family route.
func (e Element) Route() spec.HTTPRoute {
	return spec.HTTPRoute{Match: e.Match, Summary: e.Summary, Example: e.Example, Handle: e.handle}
}

func (e Element) handle(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	if r.Body == nil && len(bytes.TrimSpace(r.Raw)) > 0 {
		return unreadable()
	}
	index := fieldText(r.Body[e.IndexField])
	if strings.Contains(index, ".") {
		return spec.HTTPResponse{Status: 400, ContentType: "application/json", Body: struct {
			Error string `json:"error"`
		}{fmt.Sprintf("%s must be a whole number", e.IndexField)}}
	}
	if why := e.refuse(d, index); why != "" {
		path := e.List + "." + index + "." + e.Fields[0].Path
		return spec.HTTPResponse{Status: 400, ContentType: "application/json", Body: mutationFailed{
			Error:  "mutation failed",
			Failed: []failedMutation{{Path: path, Error: fmt.Sprintf("setting %q: %s", path, why)}},
		}}
	}
	changes := make([]surface.Change, 0, len(e.Fields))
	for _, f := range e.Fields {
		raw := r.Body[f.Name]
		path := e.List + "." + index + "." + f.Path
		switch f.Kind {
		case Number:
			n, ok := asNumber(raw)
			if !ok {
				return badField(f, "a number")
			}
			changes = append(changes, surface.Change{Path: path, Value: n})
		case Flag:
			b, ok := asFlag(raw)
			if !ok {
				return badField(f, "true or false")
			}
			changes = append(changes, surface.Change{Path: path, Value: b})
		default:
			return spec.Failure(500, "an element field is a number or a flag")
		}
	}
	if err := d.WriteMany(changes...); err != nil {
		return spec.Failure(500, err.Error())
	}
	return jsonResponse(Spaced(e.Ack(d)))
}

// refuse says why the list holds no element at index, in the words the state's
// own write gives, or is empty when it does.
func (e Element) refuse(d spec.Device, index string) string {
	segment := strings.Count(e.List, ".") + 1
	n, err := strconv.Atoi(index)
	if err != nil {
		return fmt.Sprintf("segment %d (%q): %q is not an integer index", segment, index, index)
	}
	list, _ := d.Read(e.List).([]any)
	if n < 0 || n >= len(list) {
		return fmt.Sprintf("segment %d (%q): index %d out of range [0,%d)", segment, index, n, len(list))
	}
	return ""
}

type mutationFailed struct {
	Error  string           `json:"error"`
	Failed []failedMutation `json:"failed_mutations"`
}

type failedMutation struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}
