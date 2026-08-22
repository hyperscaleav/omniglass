// Package webui serves the embedded single-page operator console. The routing
// logic (serve a real file, 404 a miss that names one, else fall back to
// index.html for the SPA router) lives here against an injected fs.FS so it is
// unit-testable with a fake filesystem; the real embedded bytes are wired in
// spa_embed.go under the `web` build tag, and a placeholder is wired in
// spa_noembed.go without it. So a bare `go build` / `go test ./...` never needs
// the Vite build to exist.
package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler serves the console from fsys with client-side-routing support: an
// existing file (index.html, assets/*, favicon) is served as-is; a path that
// names no file falls back to index.html so the Solid router resolves it,
// unless the path names a FILE rather than a client route, which is a genuine
// 404 (see namesAFile, #778). If fsys has no index.html (a binary built without
// `-tags web`), routes get a build-the-console placeholder and file requests
// still 404.
func SPAHandler(fsys fs.FS) http.Handler {
	dirs := buildDirectories(fsys)
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if namesAFile(requestPath(r), dirs) {
				http.NotFound(w, r)
				return
			}
			placeholder(w, r)
		})
	}
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := requestPath(r)
		if p != "" {
			if _, err := fs.Stat(fsys, p); err == nil {
				// Vite emits content-hashed asset names, safe to cache forever;
				// index.html (served below) is not.
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				if ct := contentTypeFor(p); ct != "" {
					w.Header().Set("Content-Type", ct)
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			if namesAFile(p, dirs) {
				http.NotFound(w, r)
				return
			}
		}
		serveIndex(w, r, fsys)
	})
}

// requestPath is the FS-relative path a request addresses: cleaned (so a
// traversal attempt resolves inside the embedded FS) and root-relative.
func requestPath(r *http.Request) string {
	return strings.TrimPrefix(path.Clean(r.URL.Path), "/")
}

// namesAFile reports whether a request path names a FILE the console build was
// supposed to contain, rather than one of the SPA's own client routes. A miss on
// one is a real 404; a miss on a client route is the deep link the catch-all
// exists for.
//
// Before #778 there was no such split: every miss answered index.html with 200,
// so a missing chunk after a partial deploy arrived as HTML the browser parsed
// as JavaScript (a syntax error at line 1, a layer away from the cause), a
// caching layer stored HTML under the asset's URL, and monitoring keyed on 4xx
// read a healthy origin while the console was broken. Nothing failed, so a guard
// that watches for failed requests was blind by construction, which is how the
// self-hosted typefaces (#775) shipped past their first guard.
//
// Two derived halves, neither a hand-kept list:
//
//   - The last segment carries an EXTENSION. A console client route cannot: an
//     entity name is `^[a-z0-9][a-z0-9-]*$` (internal/storage/name.go) and the
//     uuid form of the same address (ADR-0062) carries no dot either. This half
//     covers an asset kind nobody enumerated, which is the point of writing it
//     this way rather than as a list of prefixes or extensions.
//   - The path sits under a DIRECTORY THE BUILD EMITTED, read from the built
//     filesystem itself (buildDirectories), so a future Vite output directory is
//     covered on the day it appears and an extensionless file under one is too.
//
// Rejected: keying on the Accept header. A script or fetch request sends
// `Accept: */*`, which is indistinguishable from a probe of a deep link, and a
// monitor or curl sends none at all, so the split would land on the client's
// self-description rather than on what the build actually contains
// (ADR-0127).
func namesAFile(p string, buildDirs map[string]struct{}) bool {
	if p == "" {
		return false
	}
	if strings.Contains(path.Base(p), ".") {
		return true
	}
	first, _, nested := strings.Cut(p, "/")
	if !nested {
		return false
	}
	_, ok := buildDirs[first]
	return ok
}

// buildDirectories is the set of top-level directories the console build emitted
// (assets/, fonts/, and whatever Vite adds later). Read once at construction:
// the embedded FS is fixed at compile time, so this is a build fact, not a
// per-request one. An FS with no readable root (the placeholder build) yields an
// empty set, leaving namesAFile on its extension half alone.
func buildDirectories(fsys fs.FS) map[string]struct{} {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil
	}
	dirs := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = struct{}{}
		}
	}
	return dirs
}

// contentTypeFor returns the media type this binary states for a console file,
// or "" to leave net/http's own typing alone.
//
// It exists for the typefaces the console serves itself (#775). net/http types by
// extension from the HOST's mime database, and the images this binary ships in
// (distroless, debian-slim) carry no /etc/mime.types, so a woff2 there is sniffed
// to application/octet-stream: correct enough for a browser, which types a font by
// its own magic, but refusable by any nosniff policy in front of the console. A
// single-binary product should not answer differently depending on which packages
// the base image happened to install. Pure and table-driven so the mapping is
// testable without a filesystem or an ambient mime database.
func contentTypeFor(name string) string {
	switch path.Ext(name) {
	case ".woff2":
		return "font/woff2"
	default:
		return ""
	}
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
