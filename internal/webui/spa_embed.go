//go:build web

package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

// distFS holds the built operator console. The embed directive compiles only
// under `-tags web`, so a bare `go build` / `go test ./...` never requires
// dist/ to exist. `make build-web` runs the Vite build to populate dist/ before
// compiling with the tag.
//
//go:embed all:dist
var distFS embed.FS

// SPA returns the handler serving the embedded console.
func SPA() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist is embedded at compile time; Sub cannot fail
	}
	return SPAHandler(sub)
}
