//go:build !web

package webui

import (
	"io/fs"
	"net/http"
)

// SPA returns the placeholder handler when the binary is built without the
// `web` tag (no console embedded). `make build-web` adds `-tags web` to embed
// the real SPA; bare `go build` and `go test ./...` use this path so the suite
// compiles offline with no frontend build.
func SPA() http.Handler {
	return SPAHandler(emptyFS{})
}

// emptyFS is an fs.FS with no files; SPAHandler falls back to its placeholder.
type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
