package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

// inputScalar is one driver input value on an endpoint create or update body
// (#901): a JSON string, or a JSON number for an input the driver declares
// `kind: number`. Both land as the string the endpoint stores, so a number is
// its shortest decimal form (10161 is "10161"), and the storage-side value
// checks (resolveInputs) see one shape. A boolean, object, array or null is
// refused by the schema (a 422 naming the key) before it is ever unmarshalled.
type inputScalar string

// Schema is the oneOf string or number the generated OpenAPI, SPA client, CLI
// and JSONSchema carry, so a caller sees that a number input takes a number.
func (inputScalar) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{
		OneOf: []*huma.Schema{{Type: huma.TypeString}, {Type: huma.TypeNumber}},
	}
}

// UnmarshalJSON reads a string as itself and a number as its decimal string.
func (s *inputScalar) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*s = inputScalar(str)
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("an input is a string or a number: %w", err)
	}
	*s = inputScalar(strconv.FormatFloat(n, 'f', -1, 64))
	return nil
}

// inputScalars is the inputs map of an endpoint body.
type inputScalars map[string]inputScalar

// strings is the map the storage layer takes; nil stays nil.
func (m inputScalars) strings() map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = string(v)
	}
	return out
}
