package storage

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"
)

// bytesType is []byte, the Go type a jsonb column scans into raw.
var bytesType = reflect.TypeFor[[]byte]()

// inlineJSONBytes rewrites b, the marshaled audit image of v, so that each
// top-level []byte field of v holding JSON carries that JSON rather than the
// base64 encoding/json gives a []byte (#1088). A jsonb column read back raw
// (an endpoint's params and inputs, a driver's spec, a command type's params
// schema) is JSON, and an audit row recording it as base64 reads as noise and
// hides the change. Bytes that are not JSON marshal as before, and a field
// nested inside another value is left as it is: the images are rows, whose
// jsonb columns sit at the top or are promoted there by an embed (the retry's
// pinned endpoint).
//
// Nothing is revealed that the row did not already carry: base64 is an
// encoding, not a redaction. Redaction stays the write's own (sealed secret
// material and credential hashes never enter an image).
func inlineJSONBytes(v any, b []byte) ([]byte, error) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return b, nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return b, nil
	}
	var image map[string]json.RawMessage
	inlined := false
	for _, f := range reflect.VisibleFields(rv.Type()) {
		if f.Type != bytesType || !f.IsExported() || !promotedToTop(rv.Type(), f.Index) {
			continue
		}
		name, ok := jsonFieldName(f)
		if !ok {
			continue
		}
		fv, err := rv.FieldByIndexErr(f.Index)
		if err != nil {
			continue // reached through a nil embedded pointer: not in the image
		}
		raw := fv.Bytes()
		if !jsonbAccepts(raw) {
			continue
		}
		if image == nil {
			if err := json.Unmarshal(b, &image); err != nil {
				return b, nil
			}
		}
		// The key is this field's only if encoding/json wrote the field
		// there: a tagged field can own the name instead, and its value
		// stays.
		written, err := json.Marshal(raw)
		if err != nil || !bytes.Equal(image[name], written) {
			continue
		}
		image[name] = json.RawMessage(raw)
		inlined = true
	}
	if !inlined {
		return b, nil
	}
	return json.Marshal(image)
}

// jsonbAccepts reports bytes that are JSON a jsonb column takes: valid
// JSON, valid UTF-8 (Go's decoder lets invalid bytes through a string), and
// no NUL escape (which jsonb refuses). Anything else stays base64, so an
// image never fails the audit insert and the write with it.
func jsonbAccepts(raw []byte) bool {
	return len(raw) > 0 && json.Valid(raw) && utf8.Valid(raw) && !bytes.Contains(raw, []byte(`\u0000`))
}

// jsonFieldName is the key encoding/json writes a field under, false for a
// field it skips.
func jsonFieldName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name, true
	}
	return f.Name, true
}

// promotedToTop reports whether the field at index lands at the top of the
// marshaled object: every embed on its path is promoted (an embedded struct
// with no json name), not nested under a key of its own.
func promotedToTop(t reflect.Type, index []int) bool {
	for depth := 0; depth < len(index)-1; depth++ {
		f := t.Field(index[depth])
		if !f.Anonymous {
			return false
		}
		if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" {
			return false
		}
		t = f.Type
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
	}
	return true
}
