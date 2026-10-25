// Package logline holds the facts every writer of a component's log line
// shares (#1145): the canonical severity (RFC 5424's eight levels, one alias
// table for every spelling a component or a library uses), and the caps on a
// line's message and attributes. Pure, no I/O: the node's interpreter, the
// push lane and the ingest consumer all call it, so a line reads the same
// whichever path brought it.
package logline

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// levels is the canonical set, most severe first: the index is the RFC 5424
// numeric severity, so a "this level or worse" filter is a comparison.
var levels = []string{"emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"}

// aliases maps every accepted spelling to its canonical level. The syslog
// short keywords, the numeric codes, the slog and OpenTelemetry words; a
// word outside it is not guessed at.
var aliases = func() map[string]string {
	m := map[string]string{
		"emerg": "emergency", "panic": "emergency",
		"crit": "critical", "fatal": "critical",
		"err":           "error",
		"warn":          "warning",
		"informational": "info", "information": "info",
		"trace": "debug",
	}
	for i, l := range levels {
		m[l] = l
		m[string(rune('0'+i))] = l
	}
	return m
}()

// Severity maps a word to its canonical level, case and surrounding space
// ignored; ok is false for a word outside the table (and for an empty one).
func Severity(word string) (string, bool) {
	l, ok := aliases[strings.ToLower(strings.TrimSpace(word))]
	return l, ok
}

// Levels is the canonical set, most severe first.
func Levels() []string { return append([]string(nil), levels...) }

// Rank is a canonical level's RFC 5424 code (0 most severe); ok is false for
// any other word, an alias included.
func Rank(level string) (int, bool) {
	for i, l := range levels {
		if l == level {
			return i, true
		}
	}
	return 0, false
}

// AtLeast is the levels at or above threshold (a canonical level), most
// severe first.
func AtLeast(threshold string) ([]string, bool) {
	r, ok := Rank(threshold)
	if !ok {
		return nil, false
	}
	return append([]string(nil), levels[:r+1]...), true
}

// Normalize maps a line's severity onto the canonical set. A word the table
// does not know leaves the severity empty and is kept on the attributes as
// severity_raw, so nothing the component said is lost and a retention window
// by severity never meets a stray word. Attributes that are not a JSON
// object are kept whole beside it.
func Normalize(word string, attrs []byte) (string, []byte) {
	if strings.TrimSpace(word) == "" {
		return "", attrs
	}
	if l, ok := Severity(word); ok {
		return l, attrs
	}
	return "", withAttribute(attrs, map[string]any{"severity_raw": word})
}

// The caps on one line (#1145 decision 4): past them a message is cut and
// marked, never refused.
const (
	MaxMessageBytes    = 16 << 10
	MaxAttributesBytes = 16 << 10
)

// Cap holds a line to its caps. A message past MaxMessageBytes is cut on a
// character boundary and the attributes mark it (truncated, with the
// original length in bytes); attributes past MaxAttributesBytes are replaced
// by a marker naming their length. A line inside both caps is returned
// unchanged.
func Cap(message string, attrs []byte) (string, []byte) {
	if len(attrs) > MaxAttributesBytes {
		attrs = mustJSON(map[string]any{"attributes_truncated": true, "attributes_length": len(attrs)})
	}
	if len(message) <= MaxMessageBytes {
		return message, attrs
	}
	n := len(message)
	cut := MaxMessageBytes
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut], withAttribute(attrs, map[string]any{"truncated": true, "length": n})
}

// withAttribute merges extra into a JSON object of attributes. Empty
// attributes become the extra alone; a non-object is kept whole under
// "attributes"; attributes the merge pushes past the cap give way to the
// marker, so the marker always survives.
func withAttribute(attrs []byte, extra map[string]any) []byte {
	m := map[string]any{}
	if len(attrs) > 0 {
		if err := json.Unmarshal(attrs, &m); err != nil || m == nil {
			var whole any
			if json.Unmarshal(attrs, &whole) == nil {
				m = map[string]any{"attributes": whole}
			} else {
				m = map[string]any{}
			}
		}
	}
	for k, v := range extra {
		m[k] = v
	}
	out := mustJSON(m)
	if len(out) > MaxAttributesBytes {
		extra["attributes_truncated"] = true
		extra["attributes_length"] = len(attrs)
		return mustJSON(extra)
	}
	return out
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// Maps of strings, numbers and booleans always encode.
		panic(err)
	}
	return b
}

// Clean replaces every NUL in a string with the replacement character: a
// Postgres text column refuses byte 0x00 and jsonb refuses \u0000, and a
// component's text may carry one (telnet's CR NUL, NUL padding), so a NUL
// reaching an insert would fail the whole batch (adversarial review of #1145).
func Clean(s string) string {
	if !strings.ContainsRune(s, 0) {
		return s
	}
	return strings.ReplaceAll(s, "\x00", "\uFFFD")
}

// CleanAttributes is Clean over every string, key and value, in a JSON
// document of attributes. Attributes that do not parse are returned as they
// came (the insert refuses them as it always did).
func CleanAttributes(attrs []byte) []byte {
	if len(attrs) == 0 || !strings.Contains(string(attrs), `\u0000`) && !strings.ContainsRune(string(attrs), 0) {
		return attrs
	}
	var v any
	if err := json.Unmarshal(attrs, &v); err != nil {
		return attrs
	}
	out, err := json.Marshal(cleanValue(v))
	if err != nil {
		return attrs
	}
	return out
}

func cleanValue(v any) any {
	switch t := v.(type) {
	case string:
		return Clean(t)
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, x := range t {
			m[Clean(k)] = cleanValue(x)
		}
		return m
	case []any:
		for i := range t {
			t[i] = cleanValue(t[i])
		}
		return t
	}
	return v
}
