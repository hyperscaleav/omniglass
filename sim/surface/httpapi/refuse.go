package httpapi

import (
	"encoding/json"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// The refusals a control route answers with, for a route whose fields cannot be
// declared as a Control (a path computed from the body, a pattern on a value):
// the engine's own validation body, the body it answers a write it could not make
// with, and the one for a body that is not a JSON object. A family writes its own
// route and answers these, so every device refuses a request in the same words.

// RefusedField is one field a request was refused for: the state path it would
// have written, the value it was given, and the values or the pattern it takes.
type RefusedField = refusedField

// Refuse is the 400 a value outside a fixed list or a pattern is answered with,
// naming each field refused.
func Refuse(fields ...RefusedField) spec.HTTPResponse { return refusal(fields) }

// Unreadable is the 400 a body that was sent and is not a JSON object is
// answered with.
func Unreadable() spec.HTTPResponse { return unreadable() }

// BadField is the 400 a field of the wrong type is answered with: "<name> must be
// <want>".
func BadField(name, want string) spec.HTTPResponse { return badField(Field{Name: name}, want) }

// FailedMutation is one write a request could not make: the state path and why.
type FailedMutation struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// MutationFailed is the 400 a request whose write could not be made is answered
// with, naming each write.
func MutationFailed(failed ...FailedMutation) spec.HTTPResponse {
	body, _ := json.Marshal(struct {
		Error  string           `json:"error"`
		Failed []FailedMutation `json:"failed_mutations"`
	}{"mutation failed", failed})
	return spec.HTTPResponse{Status: 400, ContentType: "application/json", Body: body}
}

// FieldText is a body field as a refusal names it: text as itself, a missing
// field as <no value>, any other value as written.
func FieldText(v any) string { return fieldText(v) }
