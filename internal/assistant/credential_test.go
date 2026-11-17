package assistant

import (
	"strings"
	"testing"
)

// The note the model reads names the credential and where it lives, and says
// it is to be used by name, not read (#2112).
func TestCredentialNote(t *testing.T) {
	for _, tc := range []struct {
		c    Credential
		want []string
	}{
		{Credential{Name: "lobby-community", Type: "snmp-community", OwnerKind: "platform"}, []string{`"lobby-community"`, "snmp-community", "the platform", "by that name"}},
		{Credential{Name: "amp-login", Type: "basic-auth", OwnerKind: "component", Owner: "amp-9"}, []string{`"amp-login"`, "basic-auth", "the component amp-9"}},
	} {
		note := CredentialNote(tc.c)
		for _, w := range tc.want {
			if !strings.Contains(note, w) {
				t.Errorf("CredentialNote(%+v) = %q, want it to say %q", tc.c, note, w)
			}
		}
	}
}
