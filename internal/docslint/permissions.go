package docslint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/rbac"
	"gopkg.in/yaml.v3"
)

// The permission lint (#447), both directions. Forward: a permission token in
// current-tense docs prose must gate at least one route (the
// x-omniglass-permission, x-omniglass-platform-permission and
// x-omniglass-conditional-permission stamps in the generated OpenAPI document), under the real rbac matching rules, wildcards
// and the :read floor included. Inverse: every grant seeded in roles.yaml must
// gate at least one route, so the pedagogical Roles view can never teach a
// permission that does nothing. Fence-aware: a fenced permission is target
// design; the vocabulary lint still applies inside.

// permissionStamps returns the stamped permission universe as token paths.
func permissionStamps() ([][]string, error) {
	b, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.json"))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Permission            string `json:"x-omniglass-permission"`
			PlatformPermission    string `json:"x-omniglass-platform-permission"`
			ConditionalPermission string `json:"x-omniglass-conditional-permission"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var stamps [][]string
	for _, ops := range doc.Paths {
		for _, op := range ops {
			for _, p := range []string{op.Permission, op.PlatformPermission, op.ConditionalPermission} {
				if p == "" || seen[p] {
					continue
				}
				seen[p] = true
				stamps = append(stamps, strings.Split(p, ":"))
			}
		}
	}
	return stamps, nil
}

// gatesAny reports whether one permission pattern (a doc mention or a seeded
// grant) explicitly matches at least one stamped route permission. Explicit on
// purpose: rbac.Set.Allows carries the :read floor, under which any phantom
// verb on a resource would "gate" that resource's read stamp; a documented or
// seeded permission must name a capability a route actually enforces, so the
// lint matches with the floor off. matchExplicit mirrors rbac's documented
// rules otherwise (a literal matches itself, `*` one token but never a
// sensitive resource at the resource position, `>` the rest, exact length).
func gatesAny(pattern string, stamps [][]string) bool {
	set := rbac.NewSet([]string{pattern})
	pat := strings.Split(pattern, ":")
	_ = set // parse validation only: an unparseable pattern gates nothing
	if len(set.Strings()) == 0 {
		return false
	}
	if handlerTier(pat) {
		return true
	}
	for _, s := range stamps {
		if matchExplicit(pat, s) {
			return true
		}
	}
	return false
}

var sensitivePermissionResources = map[string]bool{"secret": true, "settings": true, "platform": true}

// handlerTierPermissions are real capabilities enforced inside a handler on a
// per-row basis (the admin tier on sensitive rows), so they never appear as a
// route stamp. Each entry cites the file whose handler runs the check;
// TestHandlerTierRegisterIsLive fails when a citation stops matching, so this
// register cannot outlive the code it describes.
var handlerTierPermissions = []struct{ Resource, File string }{
	{"secret", filepath.Join("..", "..", "internal", "api", "secrets.go")},
	{"file", filepath.Join("..", "..", "internal", "api", "files.go")},
}

// handlerTier reports whether a three-token :admin pattern is one of the
// handler-enforced tiers.
func handlerTier(pat []string) bool {
	if len(pat) != 3 || pat[2] != "admin" {
		return false
	}
	for _, h := range handlerTierPermissions {
		if h.Resource == pat[0] {
			return true
		}
	}
	return false
}

func matchExplicit(pat, path []string) bool {
	i := 0
	for ; i < len(pat); i++ {
		if pat[i] == ">" {
			return len(path)-i >= 1
		}
		if i >= len(path) {
			return false
		}
		if pat[i] == "*" {
			if i == 0 && sensitivePermissionResources[path[0]] {
				return false
			}
			continue
		}
		if pat[i] != path[i] {
			return false
		}
	}
	return i == len(path)
}

// permToken matches a permission-shaped code span: resource:action(s), with an
// optional tier. The broader vetting (known resource or known action) is what
// keeps host:port, go:embed, the area:*/run:* label taxonomy, and audit verb
// pairs like action:login out of scope.
var permToken = regexp.MustCompile(`^([a-z_*]+):([a-z,*-]+)(?::([a-z]+))?$`)

func stampVocab(stamps [][]string) (resources, actions map[string]bool) {
	resources = map[string]bool{}
	actions = map[string]bool{}
	for _, s := range stamps {
		resources[s[0]] = true
		if len(s) > 1 {
			actions[s[1]] = true
		}
	}
	return resources, actions
}

// scanPermissionsIn checks one line of prose for permission mentions that gate
// nothing.
func scanPermissionsIn(line string, stamps [][]string, resources, actions map[string]bool) []string {
	if illustrativeProse.MatchString(line) {
		return nil
	}
	var out []string
	for _, m := range inlineSpan.FindAllStringSubmatch(line, -1) {
		span := strings.TrimSpace(m[1])
		tok := permToken.FindStringSubmatch(span)
		if tok == nil {
			continue
		}
		res := tok[1]
		if res != "*" && !resources[res] {
			// An unknown resource is in scope only when it claims at least one
			// real permission verb; otherwise the span is a label glob, a
			// port, or a build directive, not a permission claim.
			verbs := 0
			for _, a := range strings.Split(tok[2], ",") {
				if a == "*" {
					continue
				}
				if !actions[a] {
					verbs = -1
					break
				}
				verbs++
			}
			if verbs <= 0 {
				continue
			}
		}
		// A comma list is one grant naming several permissions; each must
		// gate a route on its own, or the dead half hides behind the live one.
		for _, action := range strings.Split(tok[2], ",") {
			single := res + ":" + action
			if tok[3] != "" {
				single += ":" + tok[3]
			}
			if !gatesAny(single, stamps) {
				out = append(out, "permission "+single+" gates no route: the stamp universe has no match (design goes in a fence; a drifted mention names the real permission)")
			}
		}
	}
	return out
}

// expandGrant splits a comma action list into one permission per action, so a
// dead action cannot hide behind a live sibling in the same grant string.
func expandGrant(perm string) []string {
	segs := strings.SplitN(perm, ":", 2)
	if len(segs) != 2 {
		return []string{perm}
	}
	rest := strings.SplitN(segs[1], ":", 2)
	var out []string
	for _, a := range strings.Split(rest[0], ",") {
		single := segs[0] + ":" + strings.TrimSpace(a)
		if len(rest) == 2 {
			single += ":" + rest[1]
		}
		out = append(out, single)
	}
	return out
}

// ScanPermissionReferences runs the forward direction over the docs tree,
// unfenced regions only, history files exempt.
func ScanPermissionReferences() ([]Finding, error) {
	stamps, err := permissionStamps()
	if err != nil {
		return nil, err
	}
	resources, actions := stampVocab(stamps)
	var findings []Finding
	err = walkDocs(func(rel, content string) error {
		if vocabularyAllowed[rel] {
			return nil
		}
		for _, r := range Regions(content) {
			if r.Fenced {
				continue
			}
			inCode := false
			codeDelim := ""
			for i, line := range strings.Split(r.Text, "\n") {
				switch {
				case inCode:
					trimmed := strings.TrimLeft(line, " \t")
					if strings.HasPrefix(trimmed, codeDelim) &&
						strings.Trim(trimmed, string(codeDelim[0])+" \t") == "" {
						inCode = false
					}
				case codeFenceDelim.MatchString(line):
					inCode = true
					codeDelim = codeFenceDelim.FindStringSubmatch(line)[1]
				default:
					for _, text := range scanPermissionsIn(line, stamps, resources, actions) {
						findings = append(findings, Finding{File: rel, Line: r.StartLine + i, Text: text})
					}
				}
			}
		}
		return nil
	})
	return findings, err
}

// ScanSeededGrants runs the inverse direction: every permission seeded on a
// role must gate at least one stamped route.
func ScanSeededGrants() ([]Finding, error) {
	stamps, err := permissionStamps()
	if err != nil {
		return nil, err
	}
	path := filepath.Join("..", "..", "internal", "seed", "roles.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Roles []struct {
			ID          string   `yaml:"id"`
			Permissions []string `yaml:"permissions"`
		} `yaml:"roles"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	var findings []Finding
	for _, role := range doc.Roles {
		for _, perm := range role.Permissions {
			for _, single := range expandGrant(perm) {
				if !gatesAny(single, stamps) {
					findings = append(findings, Finding{
						File: "internal/seed/roles.yaml",
						Line: 1,
						Text: "role " + role.ID + " seeds " + single + ", which gates no route: a dead grant teaches a phantom permission on the Roles view",
					})
				}
			}
		}
	}
	return findings, nil
}
