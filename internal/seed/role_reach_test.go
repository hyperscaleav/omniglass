package seed_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
)

// What a LOCATION-scoped grant of the `deploy` role actually reaches (#714).
//
// This exists because the role's own description, which is the operator-facing
// explanation rendered in the Roles view and in the grant builder, claimed for
// most of a year that granting `deploy` at a location "builds out and edits
// everything inside a subtree (add rooms, systems, and components)". It never
// did. A grant contributes to a resolved scope only when its scope_kind can
// CONTAIN the resource (scope.applicableKinds), and every tree tier is
// contained by its own kind alone: a location-kind grant fills no system-tier
// and no component-tier scope, whatever actions the role carries. The
// cross-tier expansion is unbuilt (#10).
//
// The gap was silent, which is what made a stale sentence expensive: an admin
// granting `deploy` at a building reasonably expects the field tech to add a
// system in it, the console offers the surfaces, and the server refuses. So the
// description is now written FROM this matrix, and this test is what keeps the
// two together: if applicableKinds grows a cross-tier rule, or the role's
// permission list moves, this fails with the difference named and whoever made
// the change is pointed at the sentence to rewrite.
//
// It is pure: the role set is parsed from the embedded roles.yaml and the reach
// is computed by the same resolver the gateway uses, so no database is needed
// and the whole matrix is covered rather than the two or three routes an
// end-to-end test can afford to drive. The end-to-end proof that the resolved
// scope really is what the routes enforce lives beside it in
// internal/api/location_grant_reach_e2e_test.go, against the shipped `tech-east`
// grant shape.
func TestALocationGrantOfDeployReachesOneTier(t *testing.T) {
	roles := seed.SeededRoles()
	rr := make([]rbac.Role, 0, len(roles))
	for _, r := range roles {
		rr = append(rr, rbac.Role{ID: r.ID, Permissions: r.Permissions, Inherits: r.Inherits})
	}
	idx := rbac.NewRoleIndex(rr)
	// devseed's `tech-east` exactly: one deploy grant at a location root, with
	// the under-only operator, and nothing else.
	grants := []scope.Grant{{Role: "deploy", ScopeKind: "location", ScopeID: "L1", ScopeOp: scope.OpSubtreeExclRoot}}

	// Every action this role could plausibly resolve, against every resource a
	// grant on an fleet tier could plausibly contain. An action a role does not
	// carry resolves empty for the same reason an unreachable tier does (the
	// per-action union), and both are reach, so both are listed.
	actions := []string{"read", "create", "update", "rename", "move", "delete", "reveal", "issue", "push", "acknowledge"}
	want := map[string]string{
		// The tier the grant is at: the whole of what it builds out.
		"location": "read create update rename move",
		// The exclusive-arc families, whose owner may BE a location, so a
		// location-kind grant contains the ones this location owns.
		"secret":    "read create update reveal",
		"variable":  "read",
		"field":     "read",
		"telemetry": "read",
		// The tiers the description used to claim and the role's permissions
		// still name. Empty, every action, including read.
		"system":    "",
		"component": "",
		"interface": "",
		"task":      "",
		// An alarm resolves on the COMPONENT tier, so a location grant contains
		// none of them either. This is why #728 gave `alarm:acknowledge` to
		// `operator` and not to `deploy`: in the shape `deploy` is actually
		// granted (devseed's location-scoped `tech-east`), the capability would
		// resolve to nothing and acknowledge nothing, while the console's
		// capability-only hint would still offer the button. Revisit with the
		// cross-tier expansion (#10), not before.
		"alarm": "",
		// Unscoped: no scope kind contains a file, so a scoped grant fills
		// nothing and the permission alone governs.
		"file": "",
	}

	for resource, wantActions := range want {
		var got []string
		for _, action := range actions {
			if !scope.Resolve(grants, idx, resource, action).Empty() {
				got = append(got, action)
			}
		}
		if strings.Join(got, " ") != wantActions {
			t.Errorf("a location-scoped deploy grant reaches %s for %q, want %q.\n"+
				"The `deploy` description in internal/seed/roles.yaml is written from this matrix: change one and change the other.",
				resource, strings.Join(got, " "), wantActions)
		}
	}

	// The claim the description makes is not merely that the reach is small, it
	// is that a location grant reaches no system and no component AT ALL, so the
	// role's own system and component permissions resolve to nothing. Asserted
	// separately from the matrix because it is the sentence's actual content.
	for _, resource := range []string{"system", "component"} {
		for _, action := range []string{"read", "create", "update", "rename", "move"} {
			if !idx.Flatten([]string{"deploy"}).Allows(resource, action) {
				t.Fatalf("deploy no longer carries %s:%s, so this test is asserting the reach of a permission the role does not hold", resource, action)
			}
			if s := scope.Resolve(grants, idx, resource, action); !s.Empty() {
				t.Errorf("%s:%s resolved to %+v from a location grant: the cross-tier expansion (#10) has landed, and the role's description now understates it", resource, action, s)
			}
		}
	}
}

// TestTheDeployDescriptionDoesNotPromiseTheTiersItCannotReach ties the sentence
// to the matrix above from the other side (#714).
//
// The reach test fails when the BEHAVIOUR moves. This one fails when the COPY
// moves back: it refuses a description that promises system or component work
// while a location grant reaches neither. It matches on the promise rather than
// on the whole sentence, so the copy stays free to be rewritten, which is the
// only version of this guard that survives its first edit.
func TestTheDeployDescriptionDoesNotPromiseTheTiersItCannotReach(t *testing.T) {
	desc := describeSeededRole(t, "deploy")

	// The exact phrasing that was wrong: a subtree grant said to add systems and
	// components. If a future edit brings it back while the reach is still one
	// tier, this is where it stops.
	for _, promise := range []string{
		"add rooms, systems, and components",
		"builds out and edits everything inside a subtree",
	} {
		if strings.Contains(desc, promise) {
			t.Errorf("the deploy description claims %q, which a location-kind grant does not do: it fills no system-tier or component-tier scope at all", promise)
		}
	}
	// And it has to say so, rather than merely stopping short of the false
	// claim: an admin reading it needs to know the grant they are about to make
	// does not reach the systems in the building.
	if !strings.Contains(desc, "system") || !strings.Contains(desc, "component") {
		t.Errorf("the deploy description says nothing about systems or components:\n%s\nA grant of this role carries both permission sets and reaches neither tier from a location, which is exactly what an admin has to be told", desc)
	}
}

// describeSeededRole reads one official role's operator-facing description from
// the same embedded YAML the boot seed upserts, via the generated facts
// document, so the test reads what an operator is shown rather than a copy.
func describeSeededRole(t *testing.T, id string) string {
	t.Helper()
	facts, err := seed.FactsJSON()
	if err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	var doc struct {
		Roles []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(facts, &doc); err != nil {
		t.Fatalf("parse seed facts: %v", err)
	}
	for _, r := range doc.Roles {
		if r.ID == id {
			return r.Description
		}
	}
	t.Fatalf("no role %q in the seed facts", id)
	return ""
}
