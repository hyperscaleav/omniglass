package httpapi

import (
	"bytes"
	"encoding/json"
)

// Document renders a typed value as the JSON document a device's API answers:
// two-space indented, one key to a line, markup left as written, ending in a
// newline. A struct's field order is the key order.
func Document(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return []byte("{}\n")
	}
	return buf.Bytes()
}

// Spaced renders a typed value as the one-line acknowledgment a control route
// answers: compact, a space after every colon and comma ({"ok": true,
// "power": "on"}), markup left as written, ending in a newline.
func Spaced(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte("{}\n")
	}
	compact := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	out := make([]byte, 0, len(compact)+16)
	inString, escaped := false, false
	for _, c := range compact {
		out = append(out, c)
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && (c == ':' || c == ','):
			out = append(out, ' ')
		}
	}
	return append(out, '\n')
}
