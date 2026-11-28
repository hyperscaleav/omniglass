package httpapi

import (
	"encoding/json"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Unreadable is the 400 a body that was sent and is not a JSON object is
// answered with, for a family's own route (a control route answers it itself).
func Unreadable() spec.HTTPResponse { return unreadable() }

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
