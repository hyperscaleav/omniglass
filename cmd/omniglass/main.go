package main

import "github.com/hyperscaleav/omniglass/internal/cli"

// version is injected at release-build time via:
//
//	go build -ldflags "-X main.version=vX.Y.Z" ...
//
// The "dev" default keeps local and `make build` artifacts honest about not
// being a tagged release.
var version = "dev"

// imageDigest is the digest of the container image built for this release,
// injected beside version (-X main.imageDigest=sha256:...). A release binary's
// `omniglass lab run` pulls its image by it, so a re-pushed tag cannot change
// what it runs; empty for any other build.
var imageDigest = ""

func main() {
	cli.Execute(version, imageDigest)
}
