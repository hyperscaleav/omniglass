// Package webui serves the embedded single-page operator console. The routing
// logic (serve a real file, else fall back to index.html for the SPA router)
// lives here against an injected fs.FS so it is unit-testable with a fake
// filesystem; the real embedded bytes are wired in spa_embed.go under the `web`
// build tag, and a placeholder is wired in spa_noembed.go without it. So a bare
// `go build` / `go test ./...` never needs the Vite build to exist.
package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler serves the console from fsys with client-side-routing support: an
// existing file (index.html, assets/*, favicon) is served as-is; any other path
// falls back to index.html so the Solid router resolves it. If fsys has no
// index.html (a binary built without `-tags web`), it serves a build-the-console
// placeholder.
func SPAHandler(fsys fs.FS) http.Handler {
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return http.HandlerFunc(placeholder)
	}
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" {
			if _, err := fs.Stat(fsys, p); err == nil {
				// Vite emits content-hashed asset names, safe to cache forever;
				// index.html (served below) is not.
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		serveIndex(w, r, fsys)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	b, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		placeholder(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

func placeholder(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(placeholderHTML))
}

const placeholderHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<title>Omniglass</title></head><body><h1>Omniglass</h1>` +
	`<p>The operator console was not built into this binary. Build it with ` +
	`<code>make build-web</code>, which runs the Vite build and compiles with ` +
	`<code>-tags web</code>.</p></body></html>`
