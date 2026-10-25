package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
)

// apiBase is the prefix every API operation is served under, the spec's server
// URL; TestAPIBaseIsTheSpecsServer holds the two together.
const apiBase = "/api/v1"

// buildRequest turns a tool call's arguments into the API request the tool
// stands for (#1162): each path argument substituted into its segment, each
// query argument into the query string, the rest into a JSON body (or, for a
// free-form body, the one body argument sent whole). An argument the tool does
// not take, a missing or non-string path argument, an empty or dot segment, or a
// structured query value is refused before anything is sent: a tool must never
// reach a route other than its own, nor drop what the caller asked for.
func buildRequest(ctx context.Context, d tooldef.Def, raw json.RawMessage) (*http.Request, error) {
	args := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("the arguments must be a JSON object")
		}
	}
	part := map[string]string{}
	for _, p := range d.PathParams {
		part[p] = "path"
	}
	for _, q := range d.QueryParams {
		part[q] = "query"
	}
	for _, b := range d.BodyParams {
		part[b] = "body"
	}
	var unknown []string
	for k := range args {
		if _, ok := part[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("%s takes no argument %s", d.Name, strings.Join(unknown, ", "))
	}

	path := d.Path
	for _, p := range d.PathParams {
		v, ok := args[p]
		if !ok {
			return nil, fmt.Errorf("%s needs the argument %s", d.Name, p)
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, fmt.Errorf("the argument %s must be a string (a name or an id)", p)
		}
		if s == "" || s == "." || s == ".." {
			return nil, fmt.Errorf("the argument %s must name a resource, not %q", p, s)
		}
		path = strings.Replace(path, "{"+p+"}", pathSegment(s), 1)
	}
	q := url.Values{}
	for _, name := range d.QueryParams {
		v, ok := args[name]
		if !ok {
			continue
		}
		s, err := scalar(v)
		if err != nil {
			return nil, fmt.Errorf("the argument %s: %w", name, err)
		}
		q.Set(name, s)
	}
	target := apiBase + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}

	var body []byte
	switch {
	case d.BodyWhole:
		if v, ok := args[d.BodyParams[0]]; ok {
			body = v
		}
	case len(d.BodyParams) > 0:
		obj := map[string]json.RawMessage{}
		for _, b := range d.BodyParams {
			if v, ok := args[b]; ok {
				obj[b] = v
			}
		}
		var err error
		if body, err = json.Marshal(obj); err != nil {
			return nil, err
		}
	}
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	var req *http.Request
	var err error
	if rdr != nil {
		req, err = http.NewRequestWithContext(ctx, d.Method, target, rdr)
	} else {
		req, err = http.NewRequestWithContext(ctx, d.Method, target, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// pathSegment renders an argument as exactly one path segment, the CLI's rule
// (internal/cli pathSegment): url.PathEscape keeps a qualified name's slash
// inside the segment, and a colon is encoded too so it never reads as the
// start of a :verb custom method.
func pathSegment(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ":", "%3A")
}

// scalar renders a query argument: a string as itself, a number or boolean in
// its JSON form. An object or array has no query-string form.
func scalar(v json.RawMessage) (string, error) {
	var x any
	if err := json.Unmarshal(v, &x); err != nil {
		return "", err
	}
	switch t := x.(type) {
	case string:
		return t, nil
	case bool:
		return strconv.FormatBool(t), nil
	case float64:
		return strings.TrimSpace(string(v)), nil
	case nil:
		return "", fmt.Errorf("null has no query-string form")
	default:
		return "", fmt.Errorf("an object or array has no query-string form")
	}
}
