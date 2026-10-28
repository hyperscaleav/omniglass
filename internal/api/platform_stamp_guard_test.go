package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEveryPlatformStampedRouteEnforcesIt is the guard for a published
// permission no handler checks (#417, found by its third PR's review):
// platformGated only stamps x-omniglass-platform-permission on the operation,
// for the spec and the console; the handler has to enforce it, with
// requirePlatform, canPlatform or allows. A setRetention route shipped with
// the stamp and no check, and the seeded Operator role (a type's update
// permission, no platform:*) could set a shipped type's window to one day. This
// reads every registration in the package and fails one whose handler never
// checks.
func TestEveryPlatformStampedRouteEnforcesIt(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	opID := regexp.MustCompile(`OperationID:\s*"([^"]+)"`)
	enforced := regexp.MustCompile(`requirePlatform\(|canPlatform\(|\.allows\(`)
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		for _, at := range regexp.MustCompile(`a\.platformGated\(`).FindAllStringIndex(src, -1) {
			body := src[at[0]:]
			if next := strings.Index(body[1:], "huma.Register("); next >= 0 {
				body = body[:next+1]
			}
			seen++
			name := "an operation"
			if m := opID.FindStringSubmatch(body); m != nil {
				name = m[1]
			}
			if !enforced.MatchString(body) {
				t.Errorf("%s (%s) is stamped with a platform permission its handler never checks: call a.requirePlatform(ctx, action) first", name, f)
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no platformGated registration: the guard would pass vacuously")
	}
}
