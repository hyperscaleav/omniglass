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
	"time"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// The push face (#1743): a device that is an outside actor, not a device on
// the network, reporting by pushing. It holds a bearer token for a user
// granted telemetry:push on the systems it reports for, reads those systems
// off its own grant (GET /auth/me), and posts each one's batch through
// POST /telemetry:push on its family's cadence. The batch is the family's Go
// func over the device's state and clock: the booking service is the first,
// and reads the lab calendar for what it reports.

// pushTimeout bounds one call to the server, so a server that accepts and
// never answers does not stall the face past its next tick.
const pushTimeout = 10 * time.Second

var pushClient = &http.Client{Timeout: pushTimeout}

// pushFace is a device's compiled push face and its live credential.
type pushFace struct {
	every time.Duration
	// goBody is the batch the family renders for one system, read from the
	// device and its clock.
	goBody func(d spec.Device, owner string) ([]byte, error)

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

// compilePush is the device's push face from its family's declaration. Nil
// when the family declares none.
func compilePush(p *spec.PushFace) *pushFace {
	if p == nil {
		return nil
	}
	return &pushFace{every: p.Every, goBody: p.Body, kick: make(chan struct{}, 1)}
}

var errNoPushFace = errors.New("the device's profile declares no push face")

// RenderPush renders the batch the device pushes for one system now.
func (d *Device) RenderPush(owner string) ([]byte, error) {
	if d.push == nil {
		return nil, errNoPushFace
	}
	b, err := d.push.goBody(goDevice{d}, owner)
	if err != nil {
		return nil, fmt.Errorf("rendering the push for %s: %w", owner, err)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("the push rendered for %s is not JSON: %s", owner, b)
	}
	return b, nil
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
		read := d.worldSystems()
		if len(read) == 0 {
			var err error
			if read, err = grantedSystems(ctx, server, token); err != nil {
				return err
			}
		}
		owners = read
		d.push.mu.Lock()
		d.push.owners = owners
		d.push.mu.Unlock()
	}
	// Each system is pushed for on its own: a grant outlives the system it
	// names, so one refused system must not cost the others their push. The
	// failures collapse to one error, the first named and the rest counted.
	var first error
	failed := 0
	for _, owner := range owners {
		if err := d.pushOwner(ctx, server, token, owner); err != nil {
			if first == nil {
				first = err
			}
			failed++
		}
	}
	switch {
	case failed == 0:
		return nil
	case failed == 1:
		return first
	default:
		return fmt.Errorf("%w (and %d more of %d systems)", first, failed-1, len(owners))
	}
}

// worldSystems is the systems the world says this device speaks for, by name.
// A grant names a system by its uuid, which the simulator does not hold (the
// sim is; Omniglass learns), so a pusher the world covers pushes for the
// names the world gives and lets the server resolve each inside its grant, and
// a body can resolve its owner to a room (ownerBookings). With no world, or
// one that does not name the device, it is empty and the grant says.
func (d *Device) worldSystems() []string {
	w := d.world()
	if w == nil {
		return nil
	}
	var out []string
	for _, s := range w.Services {
		if s.Name != d.Hostname {
			continue
		}
		for _, c := range s.Covers {
			out = append(out, c.System)
		}
	}
	return out
}

// pushOwner renders and pushes one system's batch.
func (d *Device) pushOwner(ctx context.Context, server, token, owner string) error {
	body, err := d.RenderPush(owner)
	if err != nil {
		return fmt.Errorf("render for %s: %w", owner, err)
	}
	var out struct {
		Rejected []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"rejected"`
	}
	if err := pushCall(ctx, server, token, http.MethodPost, "/api/v1/telemetry:push", body, &out); err != nil {
		return fmt.Errorf("push for %s: %w", owner, err)
	}
	if len(out.Rejected) > 0 {
		return fmt.Errorf("the server rejected %s for %s: %s", out.Rejected[0].Name, owner, out.Rejected[0].Reason)
	}
	return nil
}

// grantedSystems is every system the user's grants name, off GET
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
	// A credential set before the loop started left a wake the first tick
	// already answers; spend it, so the pusher does not push twice at once.
	select {
	case <-d.push.kick:
	default:
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
