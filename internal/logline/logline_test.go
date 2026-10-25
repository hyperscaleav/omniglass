package logline

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSeverityCanonicalAndAliases: the eight RFC 5424 levels are their own
// canonical form, and every common spelling (the syslog short words, the
// slog and OpenTelemetry words, the numeric codes) maps onto one of them,
// whatever its case or surrounding space.
func TestSeverityCanonicalAndAliases(t *testing.T) {
	cases := map[string]string{
		"emergency": "emergency", "EMERG": "emergency", "panic": "emergency", "0": "emergency",
		"alert": "alert", "1": "alert",
		"critical": "critical", "crit": "critical", "fatal": "critical", "2": "critical",
		"error": "error", "err": "error", " ERROR ": "error", "3": "error",
		"warning": "warning", "warn": "warning", "WARN": "warning", "4": "warning",
		"notice": "notice", "5": "notice",
		"info": "info", "informational": "info", "Information": "info", "6": "info",
		"debug": "debug", "trace": "debug", "7": "debug",
	}
	for in, want := range cases {
		got, ok := Severity(in)
		if !ok || got != want {
			t.Errorf("Severity(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
}

// TestSeverityUnknown: a word outside the table is not guessed at.
func TestSeverityUnknown(t *testing.T) {
	for _, in := range []string{"", "loud", "8", "warning!"} {
		if got, ok := Severity(in); ok || got != "" {
			t.Errorf("Severity(%q) = %q, %v; want unknown", in, got, ok)
		}
	}
}

// TestLevelsAreRankedMostSevereFirst: the canonical set in RFC 5424 order,
// each ranked by its numeric code, so a "this level or worse" filter is a
// comparison.
func TestLevelsAreRankedMostSevereFirst(t *testing.T) {
	want := []string{"emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"}
	got := Levels()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Levels() = %v, want %v", got, want)
	}
	for i, l := range want {
		if r, ok := Rank(l); !ok || r != i {
			t.Errorf("Rank(%q) = %d, %v; want %d", l, r, ok, i)
		}
	}
	if _, ok := Rank("warn"); ok {
		t.Error("Rank takes the canonical word only")
	}
}

// TestAtLeast: the levels at or above a threshold, most severe first.
func TestAtLeast(t *testing.T) {
	got, ok := AtLeast("warning")
	if !ok || strings.Join(got, ",") != "emergency,alert,critical,error,warning" {
		t.Fatalf("AtLeast(warning) = %v, %v", got, ok)
	}
	if _, ok := AtLeast("loud"); ok {
		t.Fatal("an unknown threshold is refused")
	}
}

// TestNormalizeKeepsAnUnknownWord: a line's severity normalizes to the
// canonical set; a word outside it leaves the severity empty and is kept as
// the severity_raw attribute, so nothing the component said is lost and a
// retention window by severity never meets a stray word.
func TestNormalizeKeepsAnUnknownWord(t *testing.T) {
	sev, attrs := Normalize("WARN", nil)
	if sev != "warning" || attrs != nil {
		t.Fatalf("Normalize(WARN) = %q, %s", sev, attrs)
	}
	sev, attrs = Normalize("loud", []byte(`{"a":1}`))
	if sev != "" {
		t.Fatalf("an unknown word normalized to %q", sev)
	}
	var m map[string]any
	if err := json.Unmarshal(attrs, &m); err != nil || m["severity_raw"] != "loud" || m["a"] != float64(1) {
		t.Fatalf("attributes = %s, %v", attrs, err)
	}
	// Attributes that are not an object are kept whole beside the word.
	_, attrs = Normalize("loud", []byte(`[1,2]`))
	if err := json.Unmarshal(attrs, &m); err != nil || m["severity_raw"] != "loud" || m["attributes"] == nil {
		t.Fatalf("non-object attributes = %s, %v", attrs, err)
	}
	if sev, attrs := Normalize("", []byte(`{"a":1}`)); sev != "" || string(attrs) != `{"a":1}` {
		t.Fatalf("an empty severity changed the line: %q %s", sev, attrs)
	}
}

// TestCapTruncatesAndMarks: a message past 16 KiB is cut on a character
// boundary and marked with its original length; attributes past 16 KiB are
// replaced by a marker naming their length; a line inside both caps is
// untouched.
func TestCapTruncatesAndMarks(t *testing.T) {
	msg, attrs := Cap("short", []byte(`{"a":1}`))
	if msg != "short" || string(attrs) != `{"a":1}` {
		t.Fatalf("an in-cap line changed: %q %s", msg, attrs)
	}

	long := strings.Repeat("é", MaxMessageBytes) // two bytes each
	msg, attrs = Cap(long, nil)
	if len(msg) > MaxMessageBytes || !utf8.ValidString(msg) {
		t.Fatalf("truncated message is %d bytes, valid utf8 %v", len(msg), utf8.ValidString(msg))
	}
	var m map[string]any
	if err := json.Unmarshal(attrs, &m); err != nil || m["truncated"] != true || m["length"] != float64(len(long)) {
		t.Fatalf("truncation marker = %s, %v", attrs, err)
	}

	big := []byte(`{"blob":"` + strings.Repeat("x", MaxAttributesBytes) + `"}`)
	_, attrs = Cap("m", big)
	m = nil
	if err := json.Unmarshal(attrs, &m); err != nil || m["attributes_truncated"] != true || m["attributes_length"] != float64(len(big)) {
		t.Fatalf("attributes marker = %s, %v", attrs, err)
	}
}

// TestCleanReplacesEveryNul: Postgres text and jsonb refuse NUL, so Clean
// replaces it in a string and CleanAttributes in every key and value of a
// JSON document; text with none comes back as it was.
func TestCleanReplacesEveryNul(t *testing.T) {
	if got := Clean("a\x00b\r\x00"); strings.ContainsRune(got, 0) || got != "a�b\r�" {
		t.Fatalf("Clean = %q", got)
	}
	if got := Clean("plain"); got != "plain" {
		t.Fatalf("Clean changed clean text: %q", got)
	}
	got := CleanAttributes([]byte(`{"k\u0000":"v\u0000","n":[1,"x\u0000"],"m":{"y":"\u0000"}}`))
	if strings.Contains(string(got), `\u0000`) {
		t.Fatalf("CleanAttributes left a NUL: %s", got)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil || m["n"].([]any)[0] != float64(1) {
		t.Fatalf("CleanAttributes broke the document: %s %v", got, err)
	}
	if got := CleanAttributes([]byte(`{"a":1}`)); string(got) != `{"a":1}` {
		t.Fatalf("CleanAttributes re-encoded clean attributes: %s", got)
	}
}
