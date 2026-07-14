// Package file holds the pure validation for a file handle: the rules that
// decide whether an upload is well-formed, independent of any storage. The bytes
// themselves are content-addressed by the blob primitive; this package guards
// the metadata (name, content type) and the non-empty payload on the way in.
package file

import (
	"errors"
	"strings"
)

// MaxNameLen bounds a file name, so a handle stays a label and not a payload.
const MaxNameLen = 255

var (
	// ErrNameInvalid is a blank, oversize, or path-bearing name. A file name is a
	// label, not a path: separators are rejected so a handle can never smuggle a
	// filesystem traversal.
	ErrNameInvalid = errors.New("file: name invalid")
	// ErrContentTypeInvalid is a blank content type.
	ErrContentTypeInvalid = errors.New("file: content type invalid")
	// ErrEmpty is an upload with no bytes.
	ErrEmpty = errors.New("file: empty upload")
)

// Validate reports whether an upload is well-formed: a non-blank, bounded,
// separator-free name, a non-blank content type, and a non-empty payload.
func Validate(name, contentType string, data []byte) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(name) > MaxNameLen {
		return ErrNameInvalid
	}
	if strings.ContainsAny(name, `/\`) {
		return ErrNameInvalid
	}
	if strings.TrimSpace(contentType) == "" {
		return ErrContentTypeInvalid
	}
	if len(data) == 0 {
		return ErrEmpty
	}
	return nil
}
