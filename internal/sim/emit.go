package sim

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// emitTimeout bounds a single outbound webhook POST. Emit is best-effort
// telemetry, not a control path: a slow or dead collector must never
// stall the behavior goroutine, so the request is hard-capped well under
// any plausible tick interval.
const emitTimeout = 2 * time.Second

// emitClient is the shared HTTP client for behavior webhooks. One client
// (with its connection pool) is reused across every device and tick;
// per-request timeouts come from the context, not the client, so the
// pool stays warm.
var emitClient = &http.Client{Timeout: emitTimeout}

// emitAction is a compiled behavior emit block: the URL and body
// templates parsed once at load time against the device's behaviorFuncs
// FuncMap (so they share the same helper set as the behavior's
// mutations). A nil *emitAction on a behaviorEntry means "no emit".
type emitAction struct {
	url  *template.Template
	body *template.Template
}

// compileEmit parses an Emit block's URL and body templates. Returns nil
// (no error) when the block is nil, so callers can store the result
// directly on the behaviorEntry. A missing URL is a load-time error: an
// emit with no target can never fire and almost certainly indicates a
// typo, so fail loudly rather than ship a dead behavior.
func compileEmit(e *profile.Emit, funcs template.FuncMap) (*emitAction, error) {
	if e == nil {
		return nil, nil
	}
	if strings.TrimSpace(e.URL) == "" {
		return nil, fmt.Errorf("emit missing required field 'url'")
	}
	urlT, err := template.New("emit-url").Funcs(funcs).Parse(e.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing emit url %q: %w", e.URL, err)
	}
	a := &emitAction{url: urlT}
	if e.Body != "" {
		bodyT, err := template.New("emit-body").Funcs(funcs).Parse(e.Body)
		if err != nil {
			return nil, fmt.Errorf("parsing emit body %q: %w", e.Body, err)
		}
		a.body = bodyT
	}
	return a, nil
}

// render resolves the URL and body templates against the tick context,
// returning the strings to POST. Run this synchronously under the
// device's state lock: it reads data.State (the live state map) and must
// not race the next tick's mutations. An empty rendered URL means the
// emit is configured-off (no target set yet), signalled by ok=false with
// a nil error so the caller silently skips dispatch.
func (a *emitAction) render(data templateData) (url string, body []byte, ok bool, err error) {
	if a == nil {
		return "", nil, false, nil
	}
	url, err = renderTmpl(a.url, data)
	if err != nil {
		return "", nil, false, fmt.Errorf("rendering emit url: %w", err)
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return "", nil, false, nil // configured-off: no target URL set.
	}
	if a.body != nil {
		rendered, err := renderTmpl(a.body, data)
		if err != nil {
			return "", nil, false, fmt.Errorf("rendering emit body: %w", err)
		}
		body = []byte(rendered)
	}
	return url, body, true, nil
}

// dispatch POSTs the already-rendered webhook. Safe to call off the tick
// goroutine: it touches no device state, only the url/body strings it
// was handed. The network call runs against a fresh context bounded by
// emitTimeout so a slow collector cannot outlive the tick.
func dispatchEmit(url string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), emitTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building emit request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := emitClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting emit to %s: %w", url, err)
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused, then bound the response: we
	// don't care about the collector's reply, only that it accepted.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("emit to %s returned %s", url, resp.Status)
	}
	return nil
}

// runEmit renders the behavior's emit (if any) under the caller's lock,
// then fires the POST on its own goroutine and logs the outcome.
// Best-effort: render errors and dispatch errors both log at Warn
// without affecting state, the diff stream, or the goroutine's liveness.
// MUST be called while holding the device state lock (it reads
// data.State during render).
func (d *Device) runEmit(a *emitAction, behaviorName string, data templateData) {
	if a == nil {
		return
	}
	url, body, ok, err := a.render(data)
	if err != nil {
		slog.Warn("behavior emit render failed",
			"device_id", d.DeviceID,
			"behavior", behaviorName,
			"error", err,
		)
		return
	}
	if !ok {
		return // configured-off; nothing to send.
	}
	go func() {
		if err := dispatchEmit(url, body); err != nil {
			slog.Warn("behavior emit failed",
				"device_id", d.DeviceID,
				"behavior", behaviorName,
				"error", err,
			)
			return
		}
		slog.Debug("behavior emit sent",
			"device_id", d.DeviceID,
			"behavior", behaviorName,
			"url", url,
		)
	}()
}
