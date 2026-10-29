package api

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A push naming the official row of a shadowed name (#1307) is refused, since the
// name resolves to the operator's row on another lane; the reason says so rather
// than calling a name the official catalog holds unregistered.
func TestAPushOfAShadowedNameSaysWhyItIsRefused(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{{Name: "x", Official: true}, {Name: "plain"}},
		[]storage.PropertyType{{Name: "x"}},
		nil,
	)
	if got := unregisteredReason(reg, "x", "metric"); !strings.Contains(got, "shadow") || strings.Contains(got, "unregistered") {
		t.Errorf("a shadowed name's reason = %q, want it to say an operator's row shadows it", got)
	}
	if got := unregisteredReason(reg, "nope", "metric"); got != "unregistered metric type name" {
		t.Errorf("an unknown name's reason = %q, want the unregistered one", got)
	}
	if got := unregisteredReason(reg, "nope", "event"); got != "unregistered event type name" {
		t.Errorf("an unknown event's reason = %q", got)
	}
}
