// Package apiclient is the hand-written runtime the generated CLI commands call:
// a thin JSON-over-HTTP client carrying the bearer token and server base. The
// generated command tree depends on this stable surface, so regenerating the
// commands never regenerates the transport. It is a client of the public API
// like any other caller; the server enforces capability and scope.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client issues authenticated requests against a running Omniglass server.
type Client struct {
	server string // host root, e.g. http://localhost:8080 (no trailing slash)
	token  string
	http   *http.Client
}

// New builds a client for a server root and bearer token. A trailing slash on
// the server is trimmed so path joining stays predictable.
func New(server, token string) *Client {
	return &Client{
		server: strings.TrimRight(server, "/"),
		token:  token,
		http:   http.DefaultClient,
	}
}

// Result is one API response: the HTTP status and the raw body. The caller (a
// generated command) renders the body and maps the status to an exit code.
type Result struct {
	Status int
	Body   []byte
}

// OK reports a 2xx status.
func (r Result) OK() bool { return r.Status >= 200 && r.Status < 300 }

// Do sends method to path (an absolute API path such as /api/v1/locations) with
// an optional JSON body, attaching the bearer token. A nil body sends no
// payload. Transport errors return a non-nil error; an HTTP error status is a
// successful round trip reported in Result.Status, not a Go error, so the
// command can render the server's message and choose the exit code.
func (c *Client) Do(ctx context.Context, method, path string, body any) (Result, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return Result{}, fmt.Errorf("apiclient: marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.server+path, rdr)
	if err != nil {
		return Result{}, fmt.Errorf("apiclient: build request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("apiclient: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("apiclient: read response: %w", err)
	}
	return Result{Status: resp.StatusCode, Body: buf}, nil
}
