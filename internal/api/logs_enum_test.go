package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/logline"
)

// TestLogSeverityFilterEnumIsTheCanonicalSet: the severity filter's enum is
// written on the inputs for the OpenAPI document, so it is held here to the
// one canonical set (#1145) and cannot drift from what the gateway accepts.
func TestLogSeverityFilterEnumIsTheCanonicalSet(t *testing.T) {
	want := strings.Join(logline.Levels(), ",")
	for _, in := range []any{componentLogsInput{}, systemLogsInput{}} {
		f, ok := reflect.TypeOf(in).FieldByName("Severity")
		if !ok {
			t.Fatalf("%T has no Severity filter", in)
		}
		if got := f.Tag.Get("enum"); got != want {
			t.Fatalf("%T severity enum = %q, want %q", in, got, want)
		}
	}
}
