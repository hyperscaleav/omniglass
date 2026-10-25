package sim

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	neturl "net/url"
	"strconv"
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
	url     *template.Template
	body    *template.Template
	headers map[string]*template.Template
	sign    *emitSign
}

// emitSign is a compiled signing block (#1024): its templated scheme,
// secret and fault, and its fixed header names.
type emitSign struct {
	scheme, secret, fault                  *template.Template
	header, prefix, timestamp, delivery string
}

// renderedEmit is one post ready to send: its url, body and headers, and
// how it is signed.
type renderedEmit struct {
	url                   string
	body                  []byte
	headers               map[string]string
	scheme, secret, fault string
	sign                  *emitSign
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
	for k, v := range e.Headers {
		ht, err := template.New("emit-header").Funcs(funcs).Parse(v)
		if err != nil {
			return nil, fmt.Errorf("parsing emit header %q: %w", k, err)
		}
		if a.headers == nil {
			a.headers = map[string]*template.Template{}
		}
		a.headers[k] = ht
	}
	if sg := e.Sign; sg != nil {
		c := &emitSign{header: sg.Header, prefix: sg.Prefix, timestamp: sg.Timestamp, delivery: sg.Delivery}
		for _, f := range []struct {
			dst **template.Template
			src string
		}{{&c.scheme, sg.Scheme}, {&c.secret, sg.Secret}, {&c.fault, sg.Fault}} {
			t, err := template.New("emit-sign").Funcs(funcs).Parse(f.src)
			if err != nil {
				return nil, fmt.Errorf("parsing emit sign %q: %w", f.src, err)
			}
			*f.dst = t
		}
		a.sign = c
	}
	return a, nil
}

// render resolves the URL and body templates against the tick context,
// returning the post to send. Run this synchronously under the
// device's state lock: it reads data.State (the live state map) and must
// not race the next tick's mutations. An empty rendered URL means the
// emit is configured-off (no target set yet), signalled by ok=false with
// a nil error so the caller silently skips dispatch.
func (a *emitAction) render(data templateData) (r renderedEmit, ok bool, err error) {
	if a == nil {
		return r, false, nil
	}
	url, err := renderTmpl(a.url, data)
	if err != nil {
		return r, false, fmt.Errorf("rendering emit url: %w", err)
	}
	r.url = strings.TrimSpace(url)
	if r.url == "" {
		return r, false, nil // configured-off: no target URL set.
	}
	if a.body != nil {
		rendered, err := renderTmpl(a.body, data)
		if err != nil {
			return r, false, fmt.Errorf("rendering emit body: %w", err)
		}
		r.body = []byte(rendered)
	}
	for k, t := range a.headers {
		v, err := renderTmpl(t, data)
		if err != nil {
			return r, false, fmt.Errorf("rendering emit header %q: %w", k, err)
		}
		if r.headers == nil {
			r.headers = map[string]string{}
		}
		r.headers[k] = strings.TrimSpace(v)
	}
	if sg := a.sign; sg != nil {
		r.sign = sg
		for _, f := range []struct {
			dst *string
			t   *template.Template
		}{{&r.scheme, sg.scheme}, {&r.secret, sg.secret}, {&r.fault, sg.fault}} {
			v, err := renderTmpl(f.t, data)
			if err != nil {
				return r, false, fmt.Errorf("rendering emit sign: %w", err)
			}
			*f.dst = strings.TrimSpace(v)
		}
	}
	return r, true, nil
}

// staleBy is how old a stale fault's timestamp is: past the receiver's
// five-minute window.
const staleBy = 10 * time.Minute

// signed builds the post's request, signed by its scheme: an hmac over the
// body (or the timestamp, a dot and the body), a token or a basic login. A
// fault signs with a wrong secret, or a stale timestamp.
func (r renderedEmit) request(ctx context.Context, delivery string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(r.body))
	if err != nil {
		return nil, fmt.Errorf("building emit request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	sg := r.sign
	if sg == nil {
		return req, nil
	}
	if sg.delivery != "" {
		req.Header.Set(sg.delivery, delivery)
	}
	secret := r.secret
	if r.fault == "bad-signature" {
		secret += "-not-the-key"
	}
	switch r.scheme {
	case "hmac":
		content := r.body
		if sg.timestamp != "" {
			at := time.Now()
			if r.fault == "stale" {
				at = at.Add(-staleBy)
			}
			ts := strconv.FormatInt(at.Unix(), 10)
			req.Header.Set(sg.timestamp, ts)
			content = append([]byte(ts+"."), r.body...)
		}
		m := hmac.New(sha256.New, []byte(secret))
		m.Write(content)
		req.Header.Set(sg.header, sg.prefix+hex.EncodeToString(m.Sum(nil)))
	case "token":
		req.Header.Set(sg.header, sg.prefix+secret)
	case "basic":
		user, pass, _ := strings.Cut(secret, ":")
		req.SetBasicAuth(user, pass)
	}
	return req, nil
}

// dispatch POSTs the already-rendered webhook. Safe to call off the tick
// goroutine: it touches no device state, only the rendered post it
// was handed. The network call runs against a fresh context bounded by
// emitTimeout so a slow collector cannot outlive the tick. A redeliver
// fault sends the identical post twice, one delivery id.
func dispatchEmit(r renderedEmit) error {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	delivery := hex.EncodeToString(b)
	sends := 1
	if r.fault == "redeliver" {
		sends = 2
	}
	var first *http.Request
	for i := 0; i < sends; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), emitTimeout)
		req, err := r.request(ctx, delivery)
		if err != nil {
			cancel()
			return err
		}
		if first != nil {
			// The redelivery is the same post: the same signature and
			// timestamp, as a sender's retry is.
			req.Header = first.Header.Clone()
		}
		first = req
		resp, err := emitClient.Do(req)
		if err != nil {
			cancel()
			return fmt.Errorf("posting emit to %s: %w", emitTarget(r.url), redactURLError(err))
		}
		// Drain so the connection can be reused, then bound the response: we
		// don't care about the collector's reply, only that it accepted.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("emit to %s returned %s", emitTarget(r.url), resp.Status)
		}
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
	r, ok, err := a.render(data)
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
		if err := dispatchEmit(r); err != nil {
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
			"target", emitTarget(r.url),
		)
	}()
}

// emitTarget is where a post went, its scheme and host alone: a webhook
// URL's path carries the receiver's token (#1024), which no log keeps.
func emitTarget(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil {
		return "an unparseable url"
	}
	return u.Scheme + "://" + u.Host
}

// redactURLError drops the url a client error names, keeping its cause.
func redactURLError(err error) error {
	var ue *neturl.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
