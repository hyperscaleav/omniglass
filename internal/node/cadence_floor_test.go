package node_test

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/settings"
)

// The platform's cadences (#1373) are floored at, and default to, the
// heartbeat a node runs at by default. The settings tags are literals, so this
// holds them to node.DefaultHeartbeat: a heartbeat that moves takes its floor
// with it, or this fails.
func TestPlatformCadencesAreFlooredAtTheHeartbeat(t *testing.T) {
	want := strconv.Itoa(int(node.DefaultHeartbeat.Seconds()))
	typ := reflect.TypeOf(settings.CollectionSettings{})
	for _, name := range []string{"ProbeSeconds", "PollSeconds"} {
		f, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("collection settings have no %s", name)
		}
		if got := f.Tag.Get("minimum"); got != want {
			t.Errorf("%s minimum %q, want the heartbeat's %s seconds", name, got, want)
		}
		if got := f.Tag.Get("default"); got != want {
			t.Errorf("%s default %q, want the heartbeat's %s seconds", name, got, want)
		}
	}
}
