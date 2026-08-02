package docslint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The route lint (#446): a route documented in current-tense prose must exist
// in the generated OpenAPI document. The docs hand-write route mentions over a
// 142-path generated spec, and the 2026-07-30 audit found routes documented
// that no build ever served. Fence-aware per the #434 contract: a route inside
// a :::design fence is target design and exempt; the vocabulary lint is not.

var (
	placeholderSeg = regexp.MustCompile(`\{[^}]*\}`)
	methodPathSpan = regexp.MustCompile(`^(GET|POST|PATCH|PUT|DELETE)\s+(/\S+)$`)
	methodPathLine = regexp.MustCompile(`^\s*(GET|POST|PATCH|PUT|DELETE)\s+(/\S+)`)
	inlineSpan     = regexp.MustCompile("`([^`]+)`")
	bareRoute      = regexp.MustCompile(`^/[a-z][a-zA-Z0-9_{}:./-]*$`)
)

// docsSitePrefixes are site sections, not API routes; a code span holding one
// is a docs link, and firing on it would make every cross-reference illegal.
var docsSitePrefixes = []string{"/architecture", "/guides", "/contributing", "/reference", "/llms"}

// normalizeRoutePath maps a documented path onto the spec's shape: the server
// prefix goes, every placeholder collapses to {} (the name is prose, the arity
// is the contract), and a trailing slash is cosmetic.
func normalizeRoutePath(p string) string {
	p = strings.TrimPrefix(p, "/api/v1")
	p = placeholderSeg.ReplaceAllString(p, "{}")
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// LoadRoutes reads the committed OpenAPI document into a normalized
// path -> methods index.
func LoadRoutes() (map[string]map[string]bool, error) {
	b, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.json"))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	routes := make(map[string]map[string]bool, len(doc.Paths))
	for p, ops := range doc.Paths {
		methods := map[string]bool{}
		for m := range ops {
			switch m {
			case "get", "post", "patch", "put", "delete":
				methods[m] = true
			}
		}
		routes[normalizeRoutePath(p)] = methods
	}
	return routes, nil
}

func checkRoute(method, path string, routes map[string]map[string]bool) []string {
	norm := normalizeRoutePath(path)
	methods, ok := routes[norm]
	if !ok {
		return []string{"route " + path + " is not in the OpenAPI document: the route is design (fence it) or the mention drifted"}
	}
	if method != "" && !methods[strings.ToLower(method)] {
		return []string{"route " + path + " exists but has no " + method + " operation"}
	}
	return nil
}

// illustrativeProse marks a route named as a teaching stand-in rather than a
// claim it exists, the route lint's sibling of the vocabulary lint's
// retirement escape. Scoped to the line so the marker must sit beside the
// mention it licenses.
var illustrativeProse = regexp.MustCompile(`(?i)\b(illustrative|hypothetical)\b`)

// scanRoutesIn checks one line of prose: every inline code span that reads as
// a route mention must resolve. A bare span (no method) needs a route signal
// (a placeholder or a :verb) or a known first segment, so file paths, docs
// links, and loose ideas stay out of scope. A line marking its mention as
// illustrative is exempt.
func scanRoutesIn(line string, routes map[string]map[string]bool) []string {
	if illustrativeProse.MatchString(line) {
		return nil
	}
	var out []string
	firstSegs := map[string]bool{}
	for p := range routes {
		if i := strings.Index(p[1:], "/"); i >= 0 {
			firstSegs["/"+p[1:i+1]] = true
		} else {
			// A one-segment path may still carry a :verb suffix.
			seg := p
			if j := strings.Index(seg, ":"); j > 0 {
				seg = seg[:j]
			}
			firstSegs[seg] = true
		}
	}
	for _, m := range inlineSpan.FindAllStringSubmatch(line, -1) {
		span := strings.TrimSpace(m[1])
		// An ellipsis marks deliberate shorthand, and a query string is
		// params, not path; a dot names a file, not a route (incl. the
		// spec's own /openapi.json endpoint, which is served outside the
		// paths it describes).
		if strings.Contains(span, "...") {
			continue
		}
		if i := strings.IndexByte(span, '?'); i >= 0 {
			span = span[:i]
		}
		if mp := methodPathSpan.FindStringSubmatch(span); mp != nil {
			if !strings.Contains(mp[2], ".") {
				for _, path := range expandBraceGroups(mp[2]) {
					out = append(out, checkRoute(mp[1], path, routes)...)
				}
			}
			continue
		}
		if !bareRoute.MatchString(span) || strings.Contains(span, ".") {
			continue
		}
		if hasDocsSitePrefix(span) {
			continue
		}
		p := strings.TrimPrefix(span, "/api/v1")
		if len(p) < 2 {
			continue // the bare prefix names the API, not a route
		}
		if !strings.Contains(p[1:], "/") && !strings.ContainsAny(p, "{:") {
			continue // a one-segment bare span is a resource-family mention
		}
		seg := p
		if i := strings.Index(p[1:], "/"); i >= 0 {
			seg = "/" + p[1:i+1]
		} else if j := strings.Index(p, ":"); j > 0 {
			seg = p[:j]
		}
		if !strings.ContainsAny(p, "{:") && !firstSegs[seg] {
			continue
		}
		for _, path := range expandBraceGroups(span) {
			out = append(out, checkRoute("", path, routes)...)
		}
	}
	return out
}

var braceGroup = regexp.MustCompile(`\{([a-z_]+(?:,[a-z_]+)+)\}`)

// expandBraceGroups expands the docs' {components,systems,locations} shorthand
// into one path per alternative, so the shorthand is checked against every
// route it claims rather than none of them.
func expandBraceGroups(path string) []string {
	m := braceGroup.FindStringSubmatchIndex(path)
	if m == nil {
		return []string{path}
	}
	var out []string
	for _, alt := range strings.Split(path[m[2]:m[3]], ",") {
		out = append(out, expandBraceGroups(path[:m[0]]+alt+path[m[1]:])...)
	}
	return out
}

func hasDocsSitePrefix(p string) bool {
	for _, pre := range docsSitePrefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// scanRoutesInDoc runs the route lint over one document: unfenced regions
// only, prose lines by their code spans, and request lines inside code blocks
// (a wrong route in a curl example is exactly what a reader copies).
func scanRoutesInDoc(content string, routes map[string]map[string]bool) []Finding {
	var findings []Finding
	for _, r := range Regions(content) {
		if r.Fenced {
			continue
		}
		inCode := false
		codeDelim := ""
		for i, line := range strings.Split(r.Text, "\n") {
			lineNo := r.StartLine + i
			switch {
			case inCode:
				trimmed := strings.TrimLeft(line, " \t")
				if strings.HasPrefix(trimmed, codeDelim) &&
					strings.Trim(trimmed, string(codeDelim[0])+" \t") == "" {
					inCode = false
					continue
				}
				if mp := methodPathLine.FindStringSubmatch(line); mp != nil {
					for _, text := range checkRoute(mp[1], mp[2], routes) {
						findings = append(findings, Finding{Line: lineNo, Text: text})
					}
				}
			case codeFenceDelim.MatchString(line):
				inCode = true
				codeDelim = codeFenceDelim.FindStringSubmatch(line)[1]
			default:
				for _, text := range scanRoutesIn(line, routes) {
					findings = append(findings, Finding{Line: lineNo, Text: text})
				}
			}
		}
	}
	return findings
}

// ScanRouteReferences runs the route lint over the docs tree, excluding the
// history and generated files the vocabulary lint also exempts.
func ScanRouteReferences() ([]Finding, error) {
	routes, err := LoadRoutes()
	if err != nil {
		return nil, err
	}
	var findings []Finding
	err = walkDocs(func(rel, content string) error {
		if vocabularyAllowed[rel] {
			return nil
		}
		for _, f := range scanRoutesInDoc(content, routes) {
			findings = append(findings, Finding{File: rel, Line: f.Line, Text: f.Text})
		}
		return nil
	})
	return findings, err
}
