package rbac_test

import "testing"

// TestWithinIntersects is the agent token's grant rule (#1179): an agent holds
// what its own roles grant AND what the person who minted it holds, so it can
// never do more than that person, even when its roles name more.
func TestWithinIntersects(t *testing.T) {
	minter := set("location:*", "*:read")
	agent := set(">").Within(minter)

	cases := []struct {
		tokens []string
		want   bool
	}{
		{[]string{"location", "update"}, true},
		{[]string{"system", "read"}, true},
		{[]string{"system", "update"}, false}, // the agent's `>` names it, the minter does not hold it
		{[]string{"audit", "read", "admin"}, false},
	}
	for _, c := range cases {
		if got := agent.Allows(c.tokens...); got != c.want {
			t.Errorf("Allows(%v) = %v, want %v", c.tokens, got, c.want)
		}
	}

	narrow := set("location:read").Within(minter)
	if narrow.Allows("location", "update") {
		t.Error("an agent whose own role is location:read must not update a location the minter may")
	}
	if !narrow.Allows("location", "read") {
		t.Error("an agent whose own role is location:read reads a location the minter may")
	}
}

// TestWithoutDropsActions is the agent ceiling (#1179, #2047): a reveal or
// enroll action is refused whatever pattern would grant it, the owner's `>`
// included, so an agent principal can never decrypt a credential, while it
// still binds one it never reads.
func TestWithoutDropsActions(t *testing.T) {
	owner := set(">").Without("reveal", "enroll")
	for _, tokens := range [][]string{
		{"secret", "reveal"},
		{"secret", "reveal", "admin"},
		{"notification_target", "reveal"},
		{"node", "enroll"},
	} {
		if owner.Allows(tokens...) {
			t.Errorf("ceilinged owner must not allow %v", tokens)
		}
	}
	if !owner.Allows("secret", "bind") {
		t.Error("the ceiling leaves binding a secret alone")
	}
	if !owner.Allows("secret", "read") {
		t.Error("the ceiling leaves reading a secret's masked metadata alone")
	}
	if !owner.Allows("location", "update") {
		t.Error("the ceiling leaves every other action alone")
	}
	// The ceiling survives an intersection, in either order.
	if set(">").Within(set(">")).Without("reveal").Allows("secret", "reveal") {
		t.Error("Within then Without must still refuse reveal")
	}
	if set(">").Without("reveal").Within(set(">")).Allows("secret", "reveal") {
		t.Error("Without then Within must still refuse reveal")
	}
}

// TestConstrainedCoversIsConservative: a constrained set is the caller in every
// escalation guard (impersonation, granting a role, minting an agent), so it
// covers only what it actually allows: never a role whose actions it is denied,
// never past its minter.
func TestConstrainedCoversIsConservative(t *testing.T) {
	ceilinged := set(">").Without("reveal")
	if ceilinged.Covers(set("secret:reveal")) {
		t.Error("a ceilinged set must not cover secret:reveal")
	}
	if ceilinged.Covers(set("secret:*")) {
		t.Error("a ceilinged set must not cover secret:*, which includes reveal")
	}
	if ceilinged.Covers(set(">")) {
		t.Error("a ceilinged set must not cover the owner")
	}
	if !ceilinged.Covers(set("location:update")) {
		t.Error("a ceilinged owner still covers location:update")
	}
	within := set(">").Within(set("location:*"))
	if within.Covers(set("system:update")) {
		t.Error("an intersected set must not cover past its minter")
	}
	if !within.Covers(set("location:update")) {
		t.Error("an intersected set covers what both sides hold")
	}
	// The constrained side as the covered set: an owner covers any agent.
	if !set(">").Covers(within) {
		t.Error("the owner covers an intersected set")
	}
}

// TestConstrainedStringsHint: the /auth/me hint list never advertises a denied
// action as held.
func TestConstrainedStringsHint(t *testing.T) {
	s := set("secret:reveal", "location:update").Without("reveal")
	got := s.Strings()
	if len(got) != 1 || got[0] != "location:update" {
		t.Errorf("Strings() = %v, want [location:update]", got)
	}
}
