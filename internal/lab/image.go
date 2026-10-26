package lab

import (
	"fmt"
	"regexp"
	"strings"
)

// The image a binary runs follows the binary's version (#1194), so a lab
// always runs the code the binary was built from.
const (
	// ReleaseRepo is where a release binary's image is published, tagged
	// v<version>.
	ReleaseRepo = "ghcr.io/hyperscaleav/omniglass"
	// DevImage is the image `make` builds beside the host binary.
	DevImage = "omniglass:dev"
	// VersionLabel is the OCI label the image carries its build id in; a dev
	// binary runs only an image whose label matches its own build id.
	VersionLabel = "org.opencontainers.image.version"
)

// ImageChoice is the image a lab runs and how to get it.
type ImageChoice struct {
	Ref string
	// Pull fetches the image if it is not local: a release image is
	// published, a dev image never is.
	Pull bool
	// WantVersion, when set, is the build id the image's version label must
	// carry; a missing or mismatched image is refused, saying run make.
	WantVersion string
	// Warning is printed before the lab starts.
	Warning string
	// Why says where the choice came from, for the status line.
	Why string
}

var releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// SelectImage picks the image for a binary of the given version. Pure.
//   - --image overrides, with a warning.
//   - A release binary (a semantic version) runs ghcr.io/hyperscaleav/omniglass:v<version>,
//     pulled if absent.
//   - Any other build runs the local omniglass:dev, whose version label must
//     match the binary's build id; the lab never builds implicitly.
func SelectImage(version, override string) ImageChoice {
	if override != "" {
		return ImageChoice{Ref: override, Pull: true, Why: "--image",
			Warning: fmt.Sprintf("--image %s overrides the image this binary was built with (version %s); "+
				"a lab whose server and binary disagree is yours to debug", override, version)}
	}
	if releaseVersion.MatchString(version) {
		return ImageChoice{Ref: ReleaseRepo + ":v" + strings.TrimPrefix(version, "v"), Pull: true,
			Why: "the release image for this binary"}
	}
	return ImageChoice{Ref: DevImage, WantVersion: version, Why: "built by make beside this binary"}
}

// CheckDevImage holds a dev image to the binary that runs it: the image must
// exist and carry the binary's build id. Each refusal says run make.
func CheckDevImage(c ImageChoice, found bool, labels map[string]string) error {
	if c.WantVersion == "" {
		return nil
	}
	if !found {
		return fmt.Errorf("the image %s is not built; run `make` (it builds this binary and the image together), then run the lab again", c.Ref)
	}
	if c.WantVersion == "dev" {
		return fmt.Errorf("this binary carries no build id (it was not built by `make`), so it cannot tell whether %s is its own; run `make`, then ./bin/omniglass lab run", c.Ref)
	}
	if got := labels[VersionLabel]; got != c.WantVersion {
		if got == "" {
			got = "none"
		}
		return fmt.Errorf("the image %s was built from another tree (build %s) than this binary (build %s); run `make` to rebuild both, then run the lab again", c.Ref, got, c.WantVersion)
	}
	return nil
}
