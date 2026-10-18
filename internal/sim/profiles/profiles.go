// Package profiles embeds the simulator catalog: device and system
// profiles, fleet manifests, and the response templates they render.
package profiles

import (
	"embed"
	"io/fs"
)

//go:embed devices/*.yaml devices/schema.json systems/*.yaml fleets/*.yaml coverage.yaml all:templates
var FS embed.FS

// Templates is the catalog's templates tree, rooted so profile template
// paths (api/kestrel-vroom/status.json.gotempl) resolve as written.
func Templates() fs.FS { return sub("templates") }

// Fleets is the catalog's fleet manifests, keyed by file name.
func Fleets() fs.FS { return sub("fleets") }

func sub(dir string) fs.FS {
	f, err := fs.Sub(FS, dir)
	if err != nil {
		panic(err) // dir is a compile-time constant inside the embed
	}
	return f
}
