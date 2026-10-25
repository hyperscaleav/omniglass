// Package file holds the pure validation for a file handle: the rules that
// decide whether an upload is well-formed, independent of any storage. The bytes
// themselves are content-addressed by the blob primitive; this package guards
// the metadata (name, content type) and the non-empty payload on the way in.
package file

import (
	"errors"
	"regexp"
	"strings"
)

// MaxNameLen bounds a file name, so a handle stays a label and not a payload.
const MaxNameLen = 255

var (
	// ErrNameInvalid is a blank, oversize, or path-bearing name, or one that reads
	// as another reference (a uuid, a dot segment). A file name is a label, not a
	// path: separators are rejected so a handle can never smuggle a filesystem
	// traversal.
	ErrNameInvalid = errors.New("file: name invalid")
	// ErrContentTypeInvalid is a blank content type.
	ErrContentTypeInvalid = errors.New("file: content type invalid")
	// ErrEmpty is an upload with no bytes.
	ErrEmpty = errors.New("file: empty upload")
)

// uuidShaped is the canonical uuid form, either case: a filename that matches it
// would be read as a file's id rather than its name.
var uuidShaped = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

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
	// A file is addressed by its uuid or its filename, the uuid tried first
	// (#1133), so a uuid-shaped filename would reach a different file when typed
	// back; and `.` or `..` is a dot segment a path normalizer rewrites into the
	// route above it. Neither is a label anybody needs.
	if uuidShaped.MatchString(name) || name == "." || name == ".." {
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
