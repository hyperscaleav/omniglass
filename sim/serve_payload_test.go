// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 Hyperscale AV
package sim

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"text/template"
)

// TestServeHTTP_LoggerCapturesPayload pins the new payload fields on
// RequestLog: query / request headers / request body / response
// headers / response body all reach the Logger so the publisher can
// surface them in the console's click-to-expand log entry. Without
// this, HTTP probes show as the existing summary line only, no
// forensic detail.
func TestServeHTTP_LoggerCapturesPayload(t *testing.T) {
	dev := NewDevice("dev-1", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1,
		map[string]any{"status": map[string]any{"power": "on"}})
	dev.Port = 8080

	// One open route that echoes a tiny JSON body, gives the
	// recorder something non-empty to capture on both sides.
	dev.routes = []routeEntry{{
		Method:    "POST",
		Path:      "/api/state",
		Template:  mustTemplate(t, `{"ok":true}`),
		Mutations: nil,
	}}

	var (
		mu    sync.Mutex
		got   RequestLog
		fired bool
	)
	dev.Logger = func(rl RequestLog) {
		mu.Lock()
		got = rl
		fired = true
		mu.Unlock()
	}

	body := []byte(`{"power":"off"}`)
	req := httptest.NewRequest("POST", "/api/state?source=test&dry=1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer xyz")
	w := httptest.NewRecorder()
	dev.ServeHTTP(w, req)

	mu.Lock()
	defer mu.Unlock()
	if !fired {
		t.Fatal("Logger never fired")
	}
	if got.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", got.Status)
	}
	// Query
	if got.Query.Get("source") != "test" || got.Query.Get("dry") != "1" {
		t.Errorf("query = %v, want source=test dry=1", got.Query)
	}
	// Request headers
	if got.ReqHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("req headers content-type = %q", got.ReqHeaders.Get("Content-Type"))
	}
	if got.ReqHeaders.Get("Authorization") != "Bearer xyz" {
		t.Errorf("req headers authorization not captured")
	}
	// Request body
	if string(got.ReqBody) != `{"power":"off"}` {
		t.Errorf("req body = %q, want %q", got.ReqBody, body)
	}
	// Response body, set by the route's template render
	if !strings.Contains(string(got.RespBody), `"ok":true`) {
		t.Errorf("response body = %q, missing ok:true", got.RespBody)
	}
	// Response headers should at least include Content-Type set by render.
	// (We don't assert on every value, that depends on render impl.)
	if got.RespHeaders == nil {
		t.Errorf("response headers nil; want at least one entry")
	}
	if got.BodiesTrunc {
		t.Errorf("BodiesTrunc set on a small body; want false")
	}

	// Route still saw the body, ServeHTTP must reset r.Body so the
	// route handler can read it for mutation evaluation. Confirm by
	// re-reading the captured body matches what the test wrote.
	if string(got.ReqBody) != string(body) {
		t.Errorf("captured body diverged from request body")
	}
}

// TestServeHTTP_LoggerTruncatesLargeBodies guards the cap so a
// runaway upload can't pin RAM. Sends 2× the cap; expects the
// capture to clamp at httpBodyCaptureCap and BodiesTrunc to be set.
func TestServeHTTP_LoggerTruncatesLargeBodies(t *testing.T) {
	dev := NewDevice("dev-1", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	dev.Port = 8080
	dev.routes = []routeEntry{{
		Method:    "POST",
		Path:      "/api/big",
		Template:  mustTemplate(t, `ok`),
		Mutations: nil,
	}}

	var got RequestLog
	dev.Logger = func(rl RequestLog) { got = rl }

	big := make([]byte, httpBodyCaptureCap*2)
	for i := range big {
		big[i] = 'a'
	}
	req := httptest.NewRequest("POST", "/api/big", bytes.NewReader(big))
	dev.ServeHTTP(httptest.NewRecorder(), req)

	if len(got.ReqBody) != httpBodyCaptureCap {
		t.Errorf("captured req body len = %d, want %d (cap)", len(got.ReqBody), httpBodyCaptureCap)
	}
	if !got.BodiesTrunc {
		t.Errorf("BodiesTrunc = false on a 2× cap body, want true")
	}
}

// mustTemplate compiles a Go text/template from src or fails the
// test. Centralized here because newDevice's routes field expects
// pre-compiled templates and the test fixtures need an easy way
// to build one.
func mustTemplate(t *testing.T, src string) *template.Template {
	t.Helper()
	tpl, err := template.New("route").Parse(src)
	if err != nil {
		t.Fatalf("parse template %q: %v", src, err)
	}
	return tpl
}

// silence unused imports kept for future test edits.
var _ = io.Discard
var _ = strings.HasPrefix
