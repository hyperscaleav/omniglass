package api

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// mapKeyWriterErr is the 422 for a write the Storage Gateway refused because
// the key's writer class does not admit its writer (ADR-0161): the request
// named a key that is not this route's to write, and the message names the key
// and who does write it.
func mapKeyWriterErr(err error) (error, bool) {
	var refused *storage.KeyWriterError
	if errors.As(err, &refused) {
		return huma.Error422UnprocessableEntity(refused.Error()), true
	}
	return nil, false
}
