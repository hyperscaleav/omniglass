package lab

import (
	"strings"
	"testing"
)

func TestSelectImage(t *testing.T) {
	for _, tc := range []struct {
		version, override string
		wantRef           string
		wantPull          bool
		wantVersion       string
		warns             bool
	}{
		{"0.4.1", "", "ghcr.io/hyperscaleav/omniglass:v0.4.1", true, "", false},
		{"v0.4.1", "", "ghcr.io/hyperscaleav/omniglass:v0.4.1", true, "", false},
		{"1.0.0-rc.1", "", "ghcr.io/hyperscaleav/omniglass:v1.0.0-rc.1", true, "", false},
		{"4a34346d", "", "omniglass:dev", false, "4a34346d", false},
		{"4a34346d-dirty-20261026T131500Z", "", "omniglass:dev", false, "4a34346d-dirty-20261026T131500Z", false},
		{"dev", "", "omniglass:dev", false, "dev", false},
		{"4a34346d", "my/omniglass:try", "my/omniglass:try", true, "", true},
	} {
		c := SelectImage(tc.version, tc.override)
		if c.Ref != tc.wantRef || c.Pull != tc.wantPull || c.WantVersion != tc.wantVersion || (c.Warning != "") != tc.warns {
			t.Errorf("SelectImage(%q, %q) = %+v", tc.version, tc.override, c)
		}
	}
}

func TestCheckDevImage(t *testing.T) {
	dev := SelectImage("4a34346d", "")
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
		"unstamped": {SelectImage("dev", ""), true, map[string]string{VersionLabel: "dev"}},
	} {
		err := CheckDevImage(tc.c, tc.found, tc.labels)
		if err == nil || !strings.Contains(err.Error(), "run `make`") {
			t.Errorf("%s: %v, want a refusal saying run make", name, err)
		}
	}
	if err := CheckDevImage(SelectImage("0.4.1", ""), false, nil); err != nil {
		t.Fatalf("a release image is pulled, not checked: %v", err)
	}
}
