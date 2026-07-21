package api_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// aheadOfRoutes is the explicit allow-list of seed-role grants that intentionally
// grant a capability no route enforces yet. Each key is a raw roles.yaml
// permission string; the value justifies it. Under the route-derived permission
// universe these grants light nothing on a role blade (they enforce nothing), so
// this list documents that gap consciously: when the route lands, its entry MUST
// be removed so the grant is exercised, and the drift test below fails until it is.
// A NEW entry here is a decision to ship a grant ahead of its enforcement.
var aheadOfRoutes = map[string]string{
	// operator day-two capabilities whose subsystems have no HTTP surface yet.
	"rule:create,update":       "no rule routes yet (event engine)",
	"config:create,update":     "no config routes yet (settings engine)",
	"alarm:ack,snooze,resolve": "no alarm routes yet (alarm engine)",
	// admin management capabilities whose registries/routes have not landed.
	"credential:*":          "no credential routes yet (credential management)",
	"role:*":                "no two-token role routes yet (custom-role editing is a later slice; role:read:admin is the only routed role capability, granted explicitly)",
	"unit:create":           "no unit routes yet (unit registry)",
	"event_type:create":     "no event_type routes yet (event registry)",
	"severity_level:create": "no severity_level routes yet (severity registry)",
	"source:create":         "no source routes yet (source registry)",
}

// TestSeedGrantsResolveToUniverse keeps the seed roles honest against the routed
// permission surface. Every permission a seed role grants must resolve to at least
// one capability in the universe (the set of x-omniglass-permission stamps), or sit
// in aheadOfRoutes with a reason. It is the mirror of the net view: a grant that
// resolves to nothing shows as held-nothing on a role blade, which is the honest
// signal that the capability is not enforced yet. The test also rejects stale
// aheadOfRoutes entries, so coverage tightens automatically as routes land.
func TestSeedGrantsResolveToUniverse(t *testing.T) {
	// Spec generation never queries the gateway, so a stub keeps this no-DB.
	var gw storage.Gateway = storage.UnimplementedGateway{}
	var universe []string
	seenPerm := map[string]bool{}
	add := func(perm string) {
		if perm != "" && !seenPerm[perm] {
			seenPerm[perm] = true
			universe = append(universe, perm)
		}
	}
	for _, op := range operations(t, gw) {
		add(op.perm)
		// A platform-tier write enforces a SECOND permission (platform:<action>) on
		// top of its resource gate, published as its own stamp, so it belongs in the
		// universe too: it is enforced, and a role blade must be able to show it.
		add(op.platformPerm)
	}
	if len(universe) == 0 {
		t.Fatal("permission universe is empty; the x-omniglass-permission stamps are missing from the spec")
	}

	// covered reports whether a single grant string resolves to any universe entry,
	// using the real rbac matcher (so a wildcard or the > tail counts).
	covered := func(grant string) bool {
		set := rbac.NewSet([]string{grant})
		for _, u := range universe {
			if set.Allows(strings.Split(u, ":")...) {
				return true
			}
		}
		return false
	}

	granted := map[string]bool{}
	for _, role := range seed.SeededRoles() {
		for _, p := range role.Permissions {
			granted[p] = true
			if covered(p) {
				continue
			}
			if _, ok := aheadOfRoutes[p]; !ok {
				t.Errorf("role %q grants %q, which resolves to no routed capability; add a route (and its gated() stamp) or list it in aheadOfRoutes with a reason", role.ID, p)
			}
		}
	}

	// No stale allow-list entries: each must still be granted by some role AND still
	// be dead. When a route lands, its entry must be deleted so the grant is exercised.
	for grant := range aheadOfRoutes {
		if !granted[grant] {
			t.Errorf("aheadOfRoutes lists %q but no seed role grants it; remove the stale entry", grant)
		}
		if covered(grant) {
			t.Errorf("aheadOfRoutes lists %q but it now resolves to a routed capability; remove it from the allow-list so the grant is exercised", grant)
		}
	}
}
