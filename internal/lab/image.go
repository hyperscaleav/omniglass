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
	// v<version>; a release binary pulls it by the digest stamped at release.
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
	// BinaryVersion is the build id of the binary choosing.
	BinaryVersion string
}

// OwnBuild reports whether an image is the choosing binary's own build: its
// version label is the binary's build id (never the unstamped "dev").
func (c ImageChoice) OwnBuild(labels map[string]string) bool {
	return c.BinaryVersion != "" && c.BinaryVersion != "dev" && labels[VersionLabel] == c.BinaryVersion
}

var (
	releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	imageDigest    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// SelectImage picks the image for a binary of the given version and the
// image digest stamped into it at release. Pure.
//   - --image overrides, with a warning.
//   - A release binary (a semantic version) runs ghcr.io/hyperscaleav/omniglass@<digest>,
//     pulled if absent: the digest its release was built with, so a re-pushed
//     v<version> tag cannot change what it runs (#1257). One that carries no
//     digest is refused; it never falls back to the mutable tag.
//   - Any other build runs the local omniglass:dev, whose version label must
//     match the binary's build id; the lab never builds implicitly.
func SelectImage(version, digest, override string) (ImageChoice, error) {
	if override != "" {
		return ImageChoice{Ref: override, Pull: true, Why: "--image", BinaryVersion: version,
			Warning: fmt.Sprintf("--image %s overrides the image this binary was built with (version %s); "+
				"a lab whose server and binary disagree is yours to debug", override, version)}, nil
	}
	if releaseVersion.MatchString(version) {
		tag := "v" + strings.TrimPrefix(version, "v")
		if !imageDigest.MatchString(digest) {
			return ImageChoice{}, &PreflightError{
				Cause: fmt.Sprintf("This release binary (version %s) carries no image digest, so it cannot pin the image "+
					"it was released with; it will not run the %s:%s tag, which can be re-pushed.", version, ReleaseRepo, tag),
				Fix: "run an image you choose with --image (or OMNIGLASS_LAB_IMAGE), for example --image " +
					ReleaseRepo + ":" + tag + ", with a warning: a lab whose server and binary disagree is yours to debug.",
			}
		}
		return ImageChoice{Ref: ReleaseRepo + "@" + digest, Pull: true,
			Why: "the release image for this binary, " + tag + " pinned by digest", BinaryVersion: version}, nil
	}
	return ImageChoice{Ref: DevImage, WantVersion: version, Why: "built by make beside this binary", BinaryVersion: version}, nil
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
