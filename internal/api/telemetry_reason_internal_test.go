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
	if got := unregisteredReason(reg, "", "x", "metric"); !strings.Contains(got, "shadow") || strings.Contains(got, "unregistered") {
		t.Errorf("a shadowed name's reason = %q, want it to say an operator's row shadows it", got)
	}
	if got := unregisteredReason(reg, "", "nope", "metric"); got != "unregistered metric type name" {
		t.Errorf("an unknown name's reason = %q, want the unregistered one", got)
	}
	if got := unregisteredReason(reg, "", "nope", "event"); got != "unregistered event type name" {
		t.Errorf("an unknown event's reason = %q", got)
	}
}

// A shadowed name's reason teaches the way to the official row (#1433), and
// a qualified name no namespace row holds says which namespace was searched.
func TestAPushRejectionNamesTheNamespace(t *testing.T) {
	reg := collection.NewRegistry([]storage.MetricType{{Name: "x", Official: true}}, []storage.PropertyType{{Name: "x"}}, nil)
	if got := unregisteredReason(reg, "", "x", "metric"); !strings.Contains(got, `namespace "og"`) {
		t.Errorf("a shadowed name's reason = %q, want it to say naming namespace og reaches the official row", got)
	}
	if got := unregisteredReason(reg, "acme", "x", "metric"); got != `no metric type "x" in namespace "acme"` {
		t.Errorf("a qualified miss's reason = %q", got)
	}
}
