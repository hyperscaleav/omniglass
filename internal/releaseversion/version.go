// Package releaseversion works out the tag a release dispatch cuts on a release
// branch (#2139). The version comes from the branch name and the tags already
// cut, never from commit subjects: a dispatch on release/0.1 tags the next
// candidate, v0.1.0-rc.1, then rc.2 after a cherry-pick; a final dispatch tags
// the soaked candidate's commit v0.1.0; and the branch's next candidate after a
// final is the next patch, v0.1.1-rc.1.
package releaseversion

import (
	"fmt"
	"regexp"
	"strconv"
)

// Request is everything the next tag depends on.
type Request struct {
	// Branch is the ref the release runs on, release/<major>.<minor>.
	Branch string
	// Tags is every tag in the repository.
	Tags []string
	// HeadTags is the tags on the commit being released.
	HeadTags []string
	// Final asks for the final release of the newest candidate instead of a
	// new candidate.
	Final bool
}

// Version is the tag to cut.
type Version struct {
	// Tag is the tag to push, v<major>.<minor>.<patch>[-rc.<n>].
	Tag string
	// Prerelease marks a candidate, published as a GitHub prerelease.
	Prerelease bool
	// Previous is the newest final release below Tag, the start of its
	// notes, or empty when there is none.
	Previous string
}

var (
	branchPattern = regexp.MustCompile(`^release/(\d+)\.(\d+)$`)
	tagPattern    = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-rc\.(\d+))?$`)
)

// semver is a parsed release tag; rc is 0 for a final release.
type semver struct {
	major, minor, patch, rc int
}

func (v semver) final() bool { return v.rc == 0 }

func (v semver) String() string {
	if v.final() {
		return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
	}
	return fmt.Sprintf("v%d.%d.%d-rc.%d", v.major, v.minor, v.patch, v.rc)
}

// below orders the final releases: is v an earlier release than (major, minor, patch)?
func (v semver) below(major, minor, patch int) bool {
	if v.major != major {
		return v.major < major
	}
	if v.minor != minor {
		return v.minor < minor
	}
	return v.patch < patch
}

func parse(tag string) (semver, bool) {
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return semver{}, false
	}
	v := semver{major: atoi(m[1]), minor: atoi(m[2]), patch: atoi(m[3])}
	if m[4] != "" {
		v.rc = atoi(m[4])
		if v.rc == 0 {
			return semver{}, false
		}
	}
	return v, true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s) // the patterns admit only digits
	return n
}

// Next returns the tag a dispatch on req.Branch cuts.
func Next(req Request) (Version, error) {
	b := branchPattern.FindStringSubmatch(req.Branch)
	if b == nil {
		return Version{}, fmt.Errorf("%q is not a release branch: releases are cut from release/<major>.<minor>", req.Branch)
	}
	major, minor := atoi(b[1]), atoi(b[2])

	// The patch in play is the one after the branch's newest final release.
	patch := 0
	for _, t := range req.Tags {
		v, ok := parse(t)
		if ok && v.final() && v.major == major && v.minor == minor && v.patch >= patch {
			patch = v.patch + 1
		}
	}

	// The newest candidate of that patch.
	newest := semver{}
	for _, t := range req.Tags {
		v, ok := parse(t)
		if ok && !v.final() && v.major == major && v.minor == minor && v.patch == patch && v.rc > newest.rc {
			newest = v
		}
	}

	previous := ""
	prev := semver{major: -1}
	for _, t := range req.Tags {
		v, ok := parse(t)
		if ok && v.final() && v.below(major, minor, patch) && !v.below(prev.major, prev.minor, prev.patch) {
			prev = v
			previous = v.String()
		}
	}

	head := map[string]bool{}
	for _, t := range req.HeadTags {
		head[t] = true
	}

	if req.Final {
		if newest.rc == 0 {
			return Version{}, fmt.Errorf("no candidate of v%d.%d.%d to release: dispatch without final first, and soak it", major, minor, patch)
		}
		if !head[newest.String()] {
			return Version{}, fmt.Errorf("the final release goes on the newest candidate %s, which is not this commit", newest)
		}
		return Version{Tag: semver{major: major, minor: minor, patch: patch}.String(), Previous: previous}, nil
	}

	for t := range head {
		if v, ok := parse(t); ok && !v.final() && v.major == major && v.minor == minor && v.patch == patch {
			return Version{}, fmt.Errorf("this commit is already %s: land a change on the branch before cutting another candidate", t)
		}
	}
	next := semver{major: major, minor: minor, patch: patch, rc: newest.rc + 1}
	return Version{Tag: next.String(), Prerelease: true, Previous: previous}, nil
}
