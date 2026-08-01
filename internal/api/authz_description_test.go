package api

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestDescriptionsNameTheirGate makes the generated API reference's authority
// mechanical: every gated operation's description must contain its
// x-omniglass-permission stamp verbatim (a reader searching the reference for
// a permission token finds every operation it gates), and every ungated
// operation's description must say why it needs no gate ("public" or
// "authn-only"). Before this test, one operation had already deviated: the
// principal purge described a two-token gate while carrying the admin-tier
// three-token stamp.
func TestDescriptionsNameTheirGate(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read openapi.json (run make gen): %v", err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Description string `json:"description"`
			Permission  string `json:"x-omniglass-permission"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}

	for path, ops := range spec.Paths {
		for method, op := range ops {
			if method == "parameters" {
				continue
			}
			key := strings.ToUpper(method) + " " + strings.TrimPrefix(path, "/api/v1")
			if op.Permission != "" {
				if !strings.Contains(op.Description, op.Permission) {
					t.Errorf("%s: description does not name its gate %q verbatim; a reader searching the reference for the token cannot find this operation", key, op.Permission)
				}
				continue
			}
			desc := strings.ToLower(op.Description)
			if !strings.Contains(desc, "public") && !strings.Contains(desc, "requires authentication") {
				t.Errorf("%s: ungated but the description states neither Public nor Requires authentication; the reference must say why no permission gates it", key)
			}
		}
	}
}
