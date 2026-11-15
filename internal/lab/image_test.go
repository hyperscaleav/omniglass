package lab

import (
	"errors"
	"strings"
	"testing"
)

const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSelectImage(t *testing.T) {
	for _, tc := range []struct {
		version, digest, override string
		wantRef                   string
		wantPull                  bool
		wantVersion               string
		warns                     bool
	}{
		{"0.4.1", testDigest, "", "ghcr.io/hyperscaleav/omniglass@" + testDigest, true, "", false},
		{"v0.4.1", testDigest, "", "ghcr.io/hyperscaleav/omniglass@" + testDigest, true, "", false},
		{"1.0.0-rc.1", testDigest, "", "ghcr.io/hyperscaleav/omniglass@" + testDigest, true, "", false},
		{"4a34346d", "", "", "omniglass:dev", false, "4a34346d", false},
		{"4a34346d-dirty-20261026T131500Z", "", "", "omniglass:dev", false, "4a34346d-dirty-20261026T131500Z", false},
		{"dev", "", "", "omniglass:dev", false, "dev", false},
		{"4a34346d", "", "my/omniglass:try", "my/omniglass:try", true, "", true},
		// --image overrides a release binary too, digest or none, with its warning.
		{"0.4.1", testDigest, "my/omniglass:try", "my/omniglass:try", true, "", true},
		{"0.4.1", "", "my/omniglass:try", "my/omniglass:try", true, "", true},
	} {
		c, err := SelectImage(tc.version, tc.digest, tc.override)
		if err != nil {
			t.Errorf("SelectImage(%q, %q, %q): %v", tc.version, tc.digest, tc.override, err)
			continue
		}
		if c.Ref != tc.wantRef || c.Pull != tc.wantPull || c.WantVersion != tc.wantVersion || (c.Warning != "") != tc.warns {
			t.Errorf("SelectImage(%q, %q, %q) = %+v", tc.version, tc.digest, tc.override, c)
		}
	}
}

// A release image is pulled by the digest stamped at release, so a re-pushed
// tag cannot change what runs; the status line still names the tag.
func TestSelectImageReleaseNamesItsTag(t *testing.T) {
	c, err := SelectImage("0.4.1", testDigest, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Why, "v0.4.1") {
		t.Errorf("Why = %q, want it to name the release tag v0.4.1", c.Why)
	}
}

// A release binary that carries no image digest (a tag-pushed build, a local
// build of a tagged tree), or a malformed one, is refused saying how to
// override; it never falls back to the mutable tag.
func TestSelectImageRefusesAReleaseWithoutADigest(t *testing.T) {
	for name, digest := range map[string]string{
		"none":      "",
		"a tag":     "v0.4.1",
		"truncated": "sha256:0123",
	} {
		_, err := SelectImage("0.4.1", digest, "")
		var pe *PreflightError
		if !errors.As(err, &pe) {
			t.Errorf("%s: %v, want a PreflightError", name, err)
			continue
		}
		if !strings.Contains(pe.Fix, "--image") {
			t.Errorf("%s: fix %q, want it to name --image", name, pe.Fix)
		}
	}
}

func TestCheckDevImage(t *testing.T) {
	dev, _ := SelectImage("4a34346d", "", "")
	if err := CheckDevImage(dev, true, map[string]string{VersionLabel: "4a34346d"}); err != nil {
		t.Fatalf("a matching image: %v", err)
	}
	for name, tc := range map[string]struct {
		c      ImageChoice
		found  bool
		labels map[string]string
	}{
		"missing":   {dev, false, nil},
		"mismatch":  {dev, true, map[string]string{VersionLabel: "0badc0de"}},
		"unlabeled": {dev, true, nil},
		"unstamped": {mustSelect(t, "dev"), true, map[string]string{VersionLabel: "dev"}},
	} {
		err := CheckDevImage(tc.c, tc.found, tc.labels)
		if err == nil || !strings.Contains(err.Error(), "run `make`") {
			t.Errorf("%s: %v, want a refusal saying run make", name, err)
		}
	}
	if err := CheckDevImage(mustSelect(t, "0.4.1"), false, nil); err != nil {
		t.Fatalf("a release image is pulled, not checked: %v", err)
	}
}

// mustSelect is the image a binary of version runs, stamped with testDigest.
func mustSelect(t *testing.T, version string) ImageChoice {
	t.Helper()
	c, err := SelectImage(version, testDigest, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}
