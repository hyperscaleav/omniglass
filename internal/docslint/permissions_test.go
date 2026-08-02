package docslint

import (
	"os"
	"strings"
	"testing"
)

func permStamps() [][]string {
	return [][]string{
		{"component", "create"}, {"component", "read"}, {"component", "update"},
		{"secret", "reveal"}, {"audit", "read", "admin"}, {"command", "issue"},
		{"settings", "update"}, {"principal", "read", "admin"},
	}
}

// TestScanPermissionsResolves pins the forward direction case by case: real
// stamps pass, drifted tokens fire, wildcards run the real rbac matching, and
// the non-permission token shapes stay out of scope.
func TestScanPermissionsResolves(t *testing.T) {
	stamps := permStamps()
	resources, actions := stampVocab(stamps)
	cases := []struct {
		name string
		line string
		want int
	}{
		{"a stamped permission resolves", "gated by `component:create`", 0},
		{"a known resource with a phantom action fires", "requires `component:frobnicate`", 1},
		{"a tier mismatch fires", "auditors hold `audit:read`", 1},
		{"the exact admin tier resolves", "gated by `audit:read:admin`", 0},
		{"a stamped read resolves", "viewer reach is `component:read`", 0},
		{"a phantom verb does not gate through the read floor", "requires `secret:frobnicate`", 1},
		{"a resource wildcard resolves when it gates something", "viewer seeds `*:read`", 0},
		{"an action wildcard resolves", "admin holds `component:*`", 0},
		{"an unknown resource with a real verb fires", "needs `field:create`", 1},
		{"an unknown resource with an unknown verb is not a permission", "see `go:embed` and `host:port`", 0},
		{"the label taxonomy is not a permission", "label it `area:storage` or `run:preview`", 0},
		{"an audit verb pair is not a permission", "rows with `action:login`", 0},
		{"a comma action list checks each", "operator holds `component:create,frobnicate`", 1},
		{"an illustrative mention is exempt", "the illustrative `alarm:ack` example", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scanPermissionsIn(c.line, stamps, resources, actions)
			if len(got) != c.want {
				t.Fatalf("scanPermissionsIn(%q) = %v, want %d finding(s)", c.line, got, c.want)
			}
		})
	}
}

// TestGatesAnyRunsRealMatching pins that the lint's notion of "gates a route"
// is the rbac matcher, not a string compare: a two-token grant never reaches a
// three-token :admin stamp, and the sensitive-resource wildcard rule holds.
func TestGatesAnyRunsRealMatching(t *testing.T) {
	stamps := [][]string{{"audit", "read", "admin"}, {"secret", "reveal"}}
	if gatesAny("audit:read", stamps) {
		t.Error("audit:read must not reach the audit:read:admin stamp")
	}
	if !gatesAny("audit:read:admin", stamps) {
		t.Error("the exact admin tier must resolve")
	}
	if !gatesAny("audit:*:admin", stamps) {
		t.Error("a mid-token wildcard must resolve")
	}
	if gatesAny("*:reveal", stamps) {
		t.Error("a bare resource wildcard must not reach the sensitive secret resource")
	}
}

// TestSeededGrantsGateRoutes runs the inverse direction over the real seed and
// the real spec, enforced: a role must never seed a dead grant.
func TestSeededGrantsGateRoutes(t *testing.T) {
	findings, err := ScanSeededGrants()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "seeded-grant", findings, true)
}

// TestPermissionReferences runs the forward direction over the real corpus,
// enforced.
func TestPermissionReferences(t *testing.T) {
	findings, err := ScanPermissionReferences()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "permission-reference", findings, true)
}

// TestPermissionStampsLoad guards the failure mode that would make both
// directions meaningless: an empty universe reports zero findings whether the
// docs are clean or the spec never loaded.
func TestPermissionStampsLoad(t *testing.T) {
	stamps, err := permissionStamps()
	if err != nil {
		t.Fatal(err)
	}
	if len(stamps) < 50 {
		t.Fatalf("only %d permission stamps loaded; the universe should be ~98", len(stamps))
	}
	var hasAdminTier bool
	for _, s := range stamps {
		if strings.Join(s, ":") == "audit:read:admin" {
			hasAdminTier = true
		}
	}
	if !hasAdminTier {
		t.Error("the admin tier stamps did not load")
	}
}

// TestHandlerTierRegisterIsLive pins each handler-tier register entry to the
// code that enforces it: the cited file must still run the per-row admin-tier
// check, or the register entry is stale and must go.
func TestHandlerTierRegisterIsLive(t *testing.T) {
	for _, h := range handlerTierPermissions {
		b, err := os.ReadFile(h.File)
		if err != nil {
			t.Fatalf("register cites %s: %v", h.File, err)
		}
		want := `Allows("` + h.Resource + `", action, "admin")`
		if !strings.Contains(string(b), want) {
			t.Errorf("register entry %s: %s no longer contains %q; remove or re-point the entry", h.Resource, h.File, want)
		}
	}
}
