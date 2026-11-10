package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// The push face (#1743): a device that is an outside actor, not a device on
// the network, reporting by pushing. It holds a bearer token for a principal
// granted telemetry:push on the systems it reports for, reads those systems
// off its own grant (GET /auth/me), and posts each one's batch through
// POST /telemetry:push on its profile's cadence. The batch is the profile's
// body template rendered per system over the device's state and clock, so a
// pusher is a profile, not code: the booking service is the first.

// pushTimeout bounds one call to the server, so a server that accepts and
// never answers does not stall the face past its next tick.
const pushTimeout = 10 * time.Second

var pushClient = &http.Client{Timeout: pushTimeout}

// pushFace is a device's compiled push face and its live credential.
type pushFace struct {
	every time.Duration
	body  *template.Template

	mu     sync.Mutex
	server string
	token  string
	// owners is the systems the last grant read named; nil reads it again.
	owners []string

	// kick wakes the loop when the credential arrives, so a pusher pushes
	// as soon as it can rather than a cadence later.
	kick chan struct{}

	loopMu sync.Mutex
	stop   chan struct{}
	done   chan struct{}
}

// booking is a booked place's reading at one instant: booked while a meeting
// runs and free between, and the meetings left in its day, the one in
// progress included.
type booking struct {
	State     string
	Remaining int
}

// bookingsAt reads a meeting day (a list of "HH:MM-HH:MM" windows in UTC,
// the same every day) at now. A day that is not such a list is an error, so
// a malformed profile fails its render rather than pushing a made-up day.
// Pure.
func bookingsAt(now time.Time, windows any) (booking, error) {
	list, ok := windows.([]any)
	if !ok {
		return booking{}, fmt.Errorf("bookings %v is not a list of HH:MM-HH:MM windows", windows)
	}
	now = now.UTC()
	since := now.Sub(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC))
	b := booking{State: "free"}
	for _, w := range list {
		start, end, err := parseWindow(w)
		if err != nil {
			return booking{}, err
		}
		if since < end {
			b.Remaining++
		}
		if since >= start && since < end {
			b.State = "booked"
		}
	}
	return b, nil
}

func parseWindow(w any) (start, end time.Duration, err error) {
	s, ok := w.(string)
	if !ok {
		return 0, 0, fmt.Errorf("booking window %v is not an HH:MM-HH:MM string", w)
	}
	from, to, ok := strings.Cut(s, "-")
	if !ok {
		return 0, 0, fmt.Errorf("booking window %q is not HH:MM-HH:MM", s)
	}
	if start, err = clockOffset(from); err != nil {
		return 0, 0, fmt.Errorf("booking window %q: %w", s, err)
	}
	if end, err = clockOffset(to); err != nil {
		return 0, 0, fmt.Errorf("booking window %q: %w", s, err)
	}
	if start >= end {
		return 0, 0, fmt.Errorf("booking window %q ends before it starts", s)
	}
	return start, end, nil
}

// clockOffset is an "HH:MM" as an offset from midnight, 24:00 the latest.
func clockOffset(s string) (time.Duration, error) {
	var h, m int
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	if _, err := fmt.Sscanf(s, "%02d:%02d", &h, &m); err != nil {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	if h < 0 || m < 0 || m > 59 || h > 24 || (h == 24 && m > 0) {
		return 0, fmt.Errorf("%q is not a time of day", s)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

// pushFuncs are the template functions a push body renders with.
func pushFuncs() template.FuncMap {
	funcs := template.FuncMap{}
	for name, fn := range templateFuncs {
		funcs[name] = fn
	}
	funcs["bookingsAt"] = bookingsAt
	return funcs
}

// pushData is what a push body renders over: the device's own data, the
// system it is rendered for and the device's clock.
type pushData struct {
	templateData
	Owner string
	Now   time.Time
}

// compilePush compiles a profile's push face. Nil when it declares none.
func compilePush(name string, p *profile.Push) (*pushFace, error) {
	if p == nil {
		return nil, nil
	}
	every, err := p.PushEvery()
	if err != nil {
		return nil, err
	}
	body, err := template.New(name + ".push").Funcs(pushFuncs()).Option("missingkey=zero").Parse(p.Body)
	if err != nil {
		return nil, fmt.Errorf("push.body: %w", err)
	}
	return &pushFace{every: every, body: body, kick: make(chan struct{}, 1)}, nil
}

var errNoPushFace = errors.New("the device's profile declares no push face")

// RenderPush renders the batch the device pushes for one system now.
func (d *Device) RenderPush(owner string) ([]byte, error) {
	if d.push == nil {
		return nil, errNoPushFace
	}
	now := d.clock()
	d.mu.RLock()
	data := pushData{
		templateData: templateData{State: d.state, Defaults: d.defaults, Hostname: d.Hostname, DeviceID: d.DeviceID, IP: d.LoopbackIP.String()},
		Owner:        owner,
		Now:          now,
	}
	var buf bytes.Buffer
	err := d.push.body.Execute(&buf, data)
	d.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("rendering the push for %s: %w", owner, err)
	}
	if !json.Valid(buf.Bytes()) {
		return nil, fmt.Errorf("the push rendered for %s is not JSON: %s", owner, buf.Bytes())
	}
	return buf.Bytes(), nil
}

// PushEvery is the device's push cadence, zero when it has no push face.
func (d *Device) PushEvery() time.Duration {
	if d.push == nil {
		return 0
	}
	return d.push.every
}

// SetPushCredential hands the push face the server it pushes to and its
// bearer token, and wakes a running loop to push now. The token is held
// for the calls and never logged. A device with no push face ignores it.
func (d *Device) SetPushCredential(server, token string) {
	if d.push == nil {
		return
	}
	d.push.mu.Lock()
	d.push.server, d.push.token, d.push.owners = strings.TrimRight(server, "/"), token, nil
	d.push.mu.Unlock()
	select {
	case d.push.kick <- struct{}{}:
	default:
	}
}

// PushOnce pushes every system the device's grant names its batch now. It
// reads the grant first when it holds none, and drops what it read on any
// failure, so the next push reads it again (a server still coming up, a
// grant that changed).
func (d *Device) PushOnce(ctx context.Context) error {
	if d.push == nil {
		return errNoPushFace
	}
	d.push.mu.Lock()
	server, token, owners := d.push.server, d.push.token, d.push.owners
	d.push.mu.Unlock()
	if server == "" || token == "" {
		return errors.New("the push face holds no credential yet")
	}
	err := d.pushOwners(ctx, server, token, owners)
	if err != nil {
		d.push.mu.Lock()
		d.push.owners = nil
		d.push.mu.Unlock()
	}
	return err
}

func (d *Device) pushOwners(ctx context.Context, server, token string, owners []string) error {
	if owners == nil {
		read, err := grantedSystems(ctx, server, token)
		if err != nil {
			return err
		}
		owners = read
		d.push.mu.Lock()
		d.push.owners = owners
		d.push.mu.Unlock()
	}
	for _, owner := range owners {
		body, err := d.RenderPush(owner)
		if err != nil {
			return err
		}
		var out struct {
			Rejected []struct {
				Name   string `json:"name"`
				Reason string `json:"reason"`
			} `json:"rejected"`
		}
		if err := pushCall(ctx, server, token, http.MethodPost, "/api/v1/telemetry:push", body, &out); err != nil {
			return err
		}
		if len(out.Rejected) > 0 {
			return fmt.Errorf("the server rejected %s for %s: %s", out.Rejected[0].Name, owner, out.Rejected[0].Reason)
		}
	}
	return nil
}

// grantedSystems is every system the principal's grants name, off GET
// /auth/me: the places it may push for, and nothing else.
func grantedSystems(ctx context.Context, server, token string) ([]string, error) {
	var me struct {
		Grants []struct {
			ScopeKind string `json:"scope_kind"`
			ScopeID   string `json:"scope_id"`
		} `json:"grants"`
	}
	if err := pushCall(ctx, server, token, http.MethodGet, "/api/v1/auth/me", nil, &me); err != nil {
		return nil, err
	}
	owners := []string{}
	for _, g := range me.Grants {
		if g.ScopeKind == "system" && g.ScopeID != "" {
			owners = append(owners, g.ScopeID)
		}
	}
	if len(owners) == 0 {
		return nil, errors.New("the pusher holds no grant on a system")
	}
	return owners, nil
}

func pushCall(ctx context.Context, server, token, method, path string, in []byte, out any) error {
	var rd io.Reader
	if in != nil {
		rd = bytes.NewReader(in)
	}
	req, err := http.NewRequestWithContext(ctx, method, server+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := pushClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s %s", method, path, resp.Status, bytes.TrimSpace(b))
	}
	return json.Unmarshal(b, out)
}

// StartPush runs the push face's loop: a push on every tick of its cadence,
// and one as soon as a credential arrives. A tick with no credential yet
// pushes nothing. A failed push is logged with the device and the error,
// never the token. A no-op for a device with no push face, or one already
// running.
func (d *Device) StartPush() {
	if d.push == nil {
		return
	}
	d.push.loopMu.Lock()
	defer d.push.loopMu.Unlock()
	if d.push.stop != nil {
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	d.push.stop, d.push.done = stop, done
	go d.pushLoop(stop, done)
}

func (d *Device) pushLoop(stop, done chan struct{}) {
	defer close(done)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	t := time.NewTicker(d.push.every)
	defer t.Stop()
	tick := func() {
		d.push.mu.Lock()
		held := d.push.token != ""
		d.push.mu.Unlock()
		if !held {
			return
		}
		if err := d.PushOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("sim: push failed", "device", d.DeviceID, "error", err)
		}
	}
	tick()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			tick()
		case <-d.push.kick:
			tick()
		}
	}
}

// StopPush stops the push loop and waits for it. Idempotent.
func (d *Device) StopPush() {
	if d.push == nil {
		return
	}
	d.push.loopMu.Lock()
	stop, done := d.push.stop, d.push.done
	d.push.stop, d.push.done = nil, nil
	d.push.loopMu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}
