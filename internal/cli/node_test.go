package cli

import (
	"strconv"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/node"
)

// --session-dials (#1636) bounds how many held sessions the node dials at
// once: the flag wins, the env var stands in when the flag is omitted, and
// neither leaves the node's default.
func TestNodeRunSessionDials(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flag    int
		changed bool
		env     string
		want    int
		wantErr string
	}{
		{name: "default", flag: node.DefaultSessionDials, want: node.DefaultSessionDials},
		{name: "flag", flag: 2, changed: true, want: 2},
		{name: "env stands in for the flag", flag: node.DefaultSessionDials, env: "4", want: 4},
		{name: "flag wins over env", flag: 2, changed: true, env: "4", want: 2},
		{name: "env not a count", flag: node.DefaultSessionDials, env: "lots", wantErr: "OMNIGLASS_NODE_SESSION_DIALS"},
		{name: "env zero", flag: node.DefaultSessionDials, env: "0", wantErr: "at least 1"},
		{name: "flag zero", flag: 0, changed: true, wantErr: "at least 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sessionDialsFrom(tc.flag, tc.changed, tc.env)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %d, %v; want an error naming %q", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

// The flag is registered with the node's default, and the env reference
// states that same default.
func TestNodeRunSessionDialsFlagDefault(t *testing.T) {
	f := newNodeRunCmd().Flags().Lookup("session-dials")
	if f == nil || f.DefValue != strconv.Itoa(node.DefaultSessionDials) {
		t.Fatalf("session-dials flag = %+v, want one defaulting to %d", f, node.DefaultSessionDials)
	}
	for _, v := range config.Registry {
		if v.Key == "OMNIGLASS_NODE_SESSION_DIALS" {
			if v.Default != strconv.Itoa(node.DefaultSessionDials) {
				t.Fatalf("env reference states default %q, want %d", v.Default, node.DefaultSessionDials)
			}
			return
		}
	}
	t.Fatal("OMNIGLASS_NODE_SESSION_DIALS is not in the env reference")
}
