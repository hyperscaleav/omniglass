package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/ingress"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The webhook route (#1024, ADR-1024): the server hosts the listener for the
// webhook transport. A sender posts to POST /webhooks/{token}; the token
// names one endpoint (and so one component), the scheme its driver declares
// authenticates the post, its listeners map the body onto any lane, and the
// batch is published on the push lane, whose writer admits only collected
// keys. The route carries no principal, so it is on the ungated allow-list
// by necessity, as POST /nodes:claim is: the sender is not a principal, and
// the endpoint's scheme is the authentication.
//
// Senders retry on anything but 2xx, so the answers are a contract: 202
// only once the batch is published; 401 for a refused scheme, saying
// nothing of which part failed; 404 for an unknown, rotated or malformed
// token, byte-identical whichever; 413 past the body cap, the read stopping
// there; 429 with Retry-After past a rate or concurrency limit; 5xx only for
// our own fault. A flood degrades only this route: its limits are its own.

const (
	// webhookBodyLimit caps a post's body; past it Huma answers 413 without
	// reading on.
	webhookBodyLimit = 1 << 20
	// webhookReadDeadline bounds reading a post, so a sender dripping a
	// body cannot hold a connection.
	webhookReadDeadline = 10 * time.Second
	// The token buckets: per source address, per endpoint (once a token
	// resolves), and the listener as a whole.
	webhookSourceRate, webhookSourceBurst     = 10, 20
	webhookEndpointRate, webhookEndpointBurst = 20, 40
	webhookListenerRate, webhookListenerBurst = 200, 400
	webhookBucketKeys                         = 10000
	// The posts in flight at once, in all and per source.
	webhookInFlight          = 64
	webhookInFlightPerSource = 8
	// webhookFlushEvery is the tick the tally of refusals and unclaimed
	// sources flushes on, one write per source per tick.
	webhookFlushEvery = 2 * time.Second
	// webhookChallengeMax bounds a verification challenge echoed back.
	webhookChallengeMax = 256
	// webhookHost is the listener host the server's route reports as in the
	// unclaimed-source inbox.
	webhookHost = "server"
)

// webhookReceiver is the server's listener host for the webhook transport:
// its limits, its replay memory and its tally, shared by every post.
type webhookReceiver struct {
	gw         storage.Gateway
	pub        TelemetryPublisher
	now        func() time.Time
	flushEvery time.Duration

	sources, endpoints, listener *ingress.Buckets
	dedup                        *ingress.Deduper
	tally                        *ingress.Tally

	mu         sync.Mutex
	inFlight   int
	perSource  map[string]int
	flushArmed bool
	// unclaimed is the sources the inbox holds a row for, so a post landing
	// on an endpoint lets go of its source's row (it is claimed) without a
	// write per post; loaded from the store on first use.
	unclaimed       map[string]bool
	unclaimedLoaded bool
}

func newWebhookReceiver(gw storage.Gateway, pub TelemetryPublisher) *webhookReceiver {
	return &webhookReceiver{
		gw: gw, pub: pub, now: time.Now, flushEvery: webhookFlushEvery,
		sources:   ingress.NewBuckets(webhookSourceRate, webhookSourceBurst, webhookBucketKeys),
		endpoints: ingress.NewBuckets(webhookEndpointRate, webhookEndpointBurst, webhookBucketKeys),
		listener:  ingress.NewBuckets(webhookListenerRate, webhookListenerBurst, 1),
		dedup:     ingress.NewDeduper(ingress.ReplayWindow, 4096),
		tally:     ingress.NewTally(storage.UnclaimedBound),
		perSource: map[string]int{},
		unclaimed: map[string]bool{},
	}
}

type webhookInput struct {
	Token   string `path:"token" doc:"The endpoint's webhook token, minted by the server when the endpoint was created or its token rotated"`
	RawBody []byte

	header http.Header
	source string
}

// Resolve keeps the post's headers (a scheme reads its declared header, a
// listener may match one) and the sender's address.
func (in *webhookInput) Resolve(ctx huma.Context) []error {
	in.header = http.Header{}
	ctx.EachHeader(func(name, value string) { in.header.Add(name, value) })
	_, in.source = clientMetaFrom(ctx.Context())
	return nil
}

type webhookOutput struct {
	Status int
}

type webhookVerifyInput struct {
	Token string `path:"token" doc:"The endpoint's webhook token"`

	query url.Values
}

func (in *webhookVerifyInput) Resolve(ctx huma.Context) []error {
	u := ctx.URL()
	in.query = u.Query()
	return nil
}

type webhookVerifyOutput struct {
	Status      int
	ContentType string `header:"Content-Type"`
	NoSniff     string `header:"X-Content-Type-Options"`
	Body        []byte
}

// registerWebhookRoutes wires the webhook receiver and its verification
// challenge, both ungated by necessity.
func registerWebhookRoutes(api huma.API, gw storage.Gateway, pub TelemetryPublisher) *webhookReceiver {
	rc := newWebhookReceiver(gw, pub)
	bound := huma.Middlewares{rc.bound(api)}
	huma.Register(api, huma.Operation{
		OperationID:   "receive-webhook",
		Method:        http.MethodPost,
		Path:          "/webhooks/{token}",
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  webhookBodyLimit,
		Middlewares:   bound,
		Summary:       "Receive a webhook post for an endpoint",
		Description: "The webhook transport's listener (#1024): a component or a cloud service posts here, to the URL its webhook endpoint was given. The token names one endpoint; the scheme its driver declares authenticates the post (an hmac signature over a signed timestamp, a token, basic, or none, which reads unsigned); a signed timestamp outside five minutes is refused, and a declared delivery id lands once. The driver's listeners map the body (json, xml, form or text) onto any lane, and the post also lands as a log line unless its listener says otherwise. " +
			"Public by necessity: the sender is not a principal, and the endpoint's scheme is the authentication. Answers 202 once the post is published, 401 for a refused scheme (saying nothing of which part failed), 404 for an unknown or rotated token (identical either way), 413 past 1 MiB, 429 with Retry-After past a rate limit.",
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests},
	}, rc.receive)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		op := "verify-webhook"
		if method == http.MethodHead {
			op = "probe-webhook"
		}
		huma.Register(api, huma.Operation{
			OperationID: op,
			Method:      method,
			Path:        "/webhooks/{token}",
			Middlewares: bound,
			Summary:     "Answer a webhook sender's verification of its URL",
			Description: "A sender checking its URL before it posts (#1024): an unknown or rotated token is the receiver's identical 404; a known one answers 200, echoing the query parameter its driver declares as the challenge (text, bounded), and 204 when it declares none. Public by necessity, as the receiver is.",
			Errors:      []int{http.StatusNotFound, http.StatusTooManyRequests},
		}, rc.verify)
	}
	return rc
}

// bound is the route's own limits, applied before the body is read: the
// listener's and the source's token buckets, the posts in flight in all and
// per source, and the read deadline.
func (rc *webhookReceiver) bound(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		_, src := clientMetaFrom(ctx.Context())
		now := rc.now()
		if ok, retry := rc.listener.Allow("", now); !ok {
			tooMany(api, ctx, retry)
			return
		}
		if ok, retry := rc.sources.Allow(src, now); !ok {
			tooMany(api, ctx, retry)
			return
		}
		if !rc.acquire(src) {
			tooMany(api, ctx, time.Second)
			return
		}
		defer rc.release(src)
		if _, w := humachi.Unwrap(ctx); w != nil {
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(webhookReadDeadline))
		}
		next(ctx)
	}
}

func (rc *webhookReceiver) acquire(src string) bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.inFlight >= webhookInFlight || rc.perSource[src] >= webhookInFlightPerSource {
		return false
	}
	rc.inFlight++
	rc.perSource[src]++
	return true
}

func (rc *webhookReceiver) release(src string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.inFlight--
	if rc.perSource[src]--; rc.perSource[src] <= 0 {
		delete(rc.perSource, src)
	}
}

// retryAfter is a wait as whole seconds, at least one.
func retryAfter(d time.Duration) string {
	s := int((d + time.Second - 1) / time.Second)
	if s < 1 {
		s = 1
	}
	return strconv.Itoa(s)
}

func tooMany(api huma.API, ctx huma.Context, wait time.Duration) {
	ctx.SetHeader("Retry-After", retryAfter(wait))
	_ = huma.WriteErr(api, ctx, http.StatusTooManyRequests, "too many requests")
}

func tooManyErr(wait time.Duration) error {
	return huma.ErrorWithHeaders(huma.Error429TooManyRequests("too many requests"), http.Header{"Retry-After": {retryAfter(wait)}})
}

// webhookNotFound is the one answer for a token that resolves to nothing:
// unknown, rotated or malformed read the same.
func webhookNotFound() error { return huma.Error404NotFound("not found") }

// validToken reports a token of the shape the server mints (unpadded
// base64url of 128 bits); anything else resolves to nothing without a read.
func validToken(t string) bool {
	if len(t) != storage.WebhookTokenLen {
		return false
	}
	for _, r := range t {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (rc *webhookReceiver) resolve(ctx context.Context, token string) (*storage.WebhookTarget, error) {
	if !validToken(token) {
		return nil, storage.ErrWebhookNotFound
	}
	return rc.gw.ResolveWebhook(ctx, storage.WebhookTokenHash(token))
}

func (rc *webhookReceiver) receive(ctx context.Context, in *webhookInput) (*webhookOutput, error) {
	if rc.pub == nil {
		return nil, huma.Error503ServiceUnavailable("webhook ingest is unavailable (no bus)")
	}
	now := rc.now()
	t, err := rc.resolve(ctx, in.Token)
	if errors.Is(err, storage.ErrWebhookNotFound) {
		// Counted and held in the inbox by its source; the token tried is
		// never kept.
		rc.tally.Unclaimed(in.source, in.RawBody, now)
		rc.armFlush()
		return nil, webhookNotFound()
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("resolve webhook")
	}
	if ok, wait := rc.endpoints.Allow(t.EndpointID, now); !ok {
		return nil, tooManyErr(wait)
	}
	post := ingress.Post{Header: in.header, Body: in.RawBody}
	cred := credentialOf(t)
	if err := ingress.Authenticate(t.Scheme, t.Webhook, cred, post, now); err != nil {
		reason := ingress.ReasonUnknownScheme
		var r *ingress.Refusal
		if errors.As(err, &r) {
			reason = r.Reason
		}
		rc.tally.Refused(t.EndpointID, in.source, reason, now)
		rc.armFlush()
		return nil, huma.Error401Unauthorized("unauthorized")
	}
	id := ingress.DeliveryID(t.Webhook, in.header)
	if id != "" && !rc.dedup.Begin(t.EndpointID, id, now) {
		// A redelivery inside the window: accepted, so the sender stops
		// retrying, and landed once already.
		return &webhookOutput{Status: http.StatusAccepted}, nil
	}
	msg := driver.WebhookMessage(in.RawBody, t.Webhook.FormatOf(), routable(in.header, t.Webhook))
	batch := ingress.Batch(t.ComponentID, t.EndpointName, ingress.Route(t.Listeners, msg, now))
	if err := rc.pub.PublishTelemetry(ctx, batch); err != nil {
		if id != "" {
			rc.dedup.Abandon(t.EndpointID, id)
		}
		return nil, huma.Error503ServiceUnavailable("publish failed; retry")
	}
	// Last heard: every attributed post stamps its endpoint, the push
	// endpoint's read in place of a verdict.
	if err := rc.gw.StampEndpointsSeen(ctx, []string{t.EndpointID}); err != nil {
		slog.Warn("webhook: stamp last heard", "endpoint", t.EndpointID, "error", err)
	}
	rc.claim(ctx, in.source)
	return &webhookOutput{Status: http.StatusAccepted}, nil
}

// credentialOf is the scheme's credential off the endpoint's unsealed
// secret fields; empty when it is missing, which every scheme but none
// refuses.
func credentialOf(t *storage.WebhookTarget) ingress.Credential {
	f := t.Credential
	return ingress.Credential{Key: f["key"], Token: f["token"], Username: f["username"], Password: f["password"]}
}

// routable is the headers a listener may match on: every one but the
// credential's (an authorization or cookie, the declared signature or token
// header), so no driver can read a credential back through a match.
func routable(h http.Header, w *driver.WebhookSpec) http.Header {
	out := h.Clone()
	for _, k := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		out.Del(k)
	}
	if w != nil {
		if w.Signature != nil {
			out.Del(w.Signature.Header)
		}
		if w.Token != nil {
			out.Del(textproto.CanonicalMIMEHeaderKey(w.Token.Header))
		}
	}
	return out
}

func (rc *webhookReceiver) verify(ctx context.Context, in *webhookVerifyInput) (*webhookVerifyOutput, error) {
	t, err := rc.resolve(ctx, in.Token)
	if errors.Is(err, storage.ErrWebhookNotFound) {
		return nil, webhookNotFound()
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("resolve webhook")
	}
	if t.Webhook == nil || t.Webhook.Challenge == "" {
		return &webhookVerifyOutput{Status: http.StatusNoContent}, nil
	}
	v := in.query.Get(t.Webhook.Challenge)
	if len(v) > webhookChallengeMax {
		v = v[:webhookChallengeMax]
	}
	return &webhookVerifyOutput{Status: http.StatusOK, ContentType: "text/plain; charset=utf-8", NoSniff: "nosniff", Body: []byte(v)}, nil
}

// armFlush schedules the tally's flush one tick out, once: a quiet listener
// holds no timer.
func (rc *webhookReceiver) armFlush() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.flushArmed {
		return
	}
	rc.flushArmed = true
	time.AfterFunc(rc.flushEvery, rc.flush)
}

// flush writes the tally (one write per source per tick) and logs it once:
// each refusal names its endpoint, source and reason, never a credential.
func (rc *webhookReceiver) flush() {
	refused, unclaimed, dropped := rc.tally.Drain()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rs := make([]storage.WebhookRefusal, 0, len(refused))
	for _, r := range refused {
		slog.Warn("webhook refused", "endpoint", r.EndpointID, "source", r.Source, "reason", r.Reason, "count", r.Count)
		rs = append(rs, storage.WebhookRefusal{EndpointID: r.EndpointID, Source: r.Source, Reason: r.Reason, Count: r.Count, LastAt: r.LastAt})
	}
	if err := rc.gw.RecordWebhookRefusals(ctx, rs); err != nil {
		slog.Warn("webhook: record refusals", "error", err)
	}
	us := make([]storage.UnclaimedSource, 0, len(unclaimed))
	for _, u := range unclaimed {
		slog.Info("webhook from an unclaimed source", "source", u.Source, "count", u.Count)
		us = append(us, storage.UnclaimedSource{Host: webhookHost, Transport: "webhook", Source: u.Source,
			FirstHeardAt: u.FirstAt, LastHeardAt: u.LastAt, Count: u.Count, LastMessage: u.LastMessage})
	}
	if dropped > 0 {
		slog.Warn("webhook: unclaimed sources past the tally's bound, counted and dropped", "dropped", dropped)
	}
	if err := rc.gw.RecordUnclaimedSources(ctx, us); err != nil {
		slog.Warn("webhook: record unclaimed sources", "error", err)
	}
	rc.mu.Lock()
	for _, u := range us {
		rc.unclaimed[u.Source] = true
	}
	rc.flushArmed = false
	rc.mu.Unlock()
	if rc.tally.Pending() {
		rc.armFlush()
	}
}

// claim lets go of a source's inbox row once a post from it lands: its
// sender now posts to an endpoint. Only a source the inbox holds costs a
// write.
func (rc *webhookReceiver) claim(ctx context.Context, src string) {
	rc.mu.Lock()
	if !rc.unclaimedLoaded {
		rc.unclaimedLoaded = true
		rc.mu.Unlock()
		if us, err := rc.gw.ListUnclaimedSources(ctx); err == nil {
			rc.mu.Lock()
			for _, u := range us {
				if u.Host == webhookHost && u.Transport == "webhook" {
					rc.unclaimed[u.Source] = true
				}
			}
			rc.mu.Unlock()
		}
		rc.mu.Lock()
	}
	held := rc.unclaimed[src]
	delete(rc.unclaimed, src)
	rc.mu.Unlock()
	if held {
		if _, err := rc.gw.ClaimUnclaimedSource(ctx, webhookHost, "webhook", src); err != nil {
			slog.Warn("webhook: claim unclaimed source", "source", src, "error", err)
		}
	}
}
