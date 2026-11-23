package sso_test

import (
	"bytes"
	"os"
	"regexp"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sso"
)

// The refusal codes are a closed set of URL-safe words, each once, so the
// console can key a fixed message by each and nothing else reaches the page.
func TestErrorCodesAreDistinctURLSafeWords(t *testing.T) {
	seen := map[sso.ErrorCode]bool{}
	for _, c := range sso.ErrorCodes() {
		if !regexp.MustCompile(`^[a-z][a-z_]*$`).MatchString(string(c)) {
			t.Errorf("code %q is not a lowercase word", c)
		}
		if seen[c] {
			t.Errorf("code %q is listed twice", c)
		}
		seen[c] = true
	}
	if len(seen) == 0 {
		t.Fatal("no refusal codes")
	}
}

// The console's copy of the codes is generated: the committed file is what
// make gen writes.
func TestErrorCodesTSIsCurrent(t *testing.T) {
	committed, err := os.ReadFile("../../web/src/lib/sso_errors.gen.ts")
	if err != nil {
		t.Fatalf("read committed sso_errors.gen.ts (run make gen and commit it): %v", err)
	}
	if !bytes.Equal(committed, sso.ErrorCodesTS()) {
		t.Error("web/src/lib/sso_errors.gen.ts drifted from sso.ErrorCodes: run make gen and commit the result")
	}
}
