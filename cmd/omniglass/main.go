package main

import "github.com/hyperscaleav/omniglass/internal/cli"

// version is injected at release-build time via:
//
//	go build -ldflags "-X main.version=vX.Y.Z" ...
//
// The "dev" default keeps local and `make build` artifacts honest about not
// being a tagged release.
var version = "dev"

func main() {
	cli.Execute(version)
}
