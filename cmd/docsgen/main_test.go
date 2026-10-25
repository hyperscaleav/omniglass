package main

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/cli"
	"github.com/hyperscaleav/omniglass/internal/config"
)

// The CLI reference renders every flag's default, so a default read from the
// environment put whatever the person running `make gen` had exported into a
// committed, published page: their bearer token among it (#979). The page is
// a function of the code alone.

// TestTheReferenceIgnoresTheCallersEnvironment renders the reference with
// every variable the binary reads unset, then again with each set to a value
// only the environment could supply, and requires the two to be identical.
func TestTheReferenceIgnoresTheCallersEnvironment(t *testing.T) {
	for _, v := range config.Registry {
		t.Setenv(v.Key, "")
	}
	clean := reference(cli.Root("(version)"))

	sentinels := map[string]string{}
	for _, v := range config.Registry {
		val := "sentinel-" + strings.ToLower(v.Key) + "-q7x"
		sentinels[v.Key] = val
		t.Setenv(v.Key, val)
	}
	dirty := reference(cli.Root("(version)"))

	for k, v := range sentinels {
		if bytes.Contains(dirty, []byte(v)) {
			t.Errorf("the reference prints the value of $%s", k)
		}
	}
	if !bytes.Equal(clean, dirty) {
		cl, dl := strings.Split(string(clean), "\n"), strings.Split(string(dirty), "\n")
		for i := 0; i < len(cl) && i < len(dl); i++ {
			if cl[i] != dl[i] {
				t.Fatalf("the reference changes with the environment, first at line %d:\n  unset: %s\n  set:   %s", i+1, cl[i], dl[i])
			}
		}
		t.Fatalf("the reference changes with the environment (%d vs %d lines)", len(cl), len(dl))
	}
}

// bearerShape is the shape auth.NewBearerToken mints: the scheme, an 8-hex
// locator, and 24 random bytes in unpadded base64url.
var bearerShape = regexp.MustCompile(`ogp_[0-9a-f]{8}_[A-Za-z0-9_-]{32}`)

// TestTheCommittedReferenceCarriesNoToken checks the page as committed, the
// one the docs site publishes, for anything shaped like a bearer token.
func TestTheCommittedReferenceCarriesNoToken(t *testing.T) {
	page, err := os.ReadFile("../../" + dst)
	if err != nil {
		t.Fatalf("read the committed reference: %v", err)
	}
	if m := bearerShape.Find(page); m != nil {
		t.Errorf("%s carries a bearer token (%s...); regenerate it with no token exported", dst, m[:12])
	}
}
