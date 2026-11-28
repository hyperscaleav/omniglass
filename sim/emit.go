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
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
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

// emitSign is a signing block (#1024): the header names a post carries its
// signature, timestamp and delivery id in.
type emitSign struct {
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
