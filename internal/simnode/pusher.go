package simnode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// The simulated booking service (#1370): a first-party integration that
// pushes a place's bookings through POST /telemetry:push, as a scheduling
// platform's would. It holds a token the lab mints per run, for a principal
// holding telemetry:push alone on one system, and reads that system off its
// own grant: it pushes where it is granted and learns nothing else. A push
// has no endpoint, so it is the collection method that counts on its
// collected tier alone.

// pusherSource is the provenance source every pushed row carries.
const pusherSource = "sim-booking-service"

// defaultPushEvery is how often the booking service pushes.
const defaultPushEvery = 30 * time.Second

// booking is one meeting of the service's day, as offsets from midnight UTC.
type booking struct{ start, end time.Duration }

// bookingDay is the booked place's meeting day, the same every day so a
// walkthrough reads the same on any run.
var bookingDay = []booking{
	{9 * time.Hour, 10 * time.Hour},
	{10*time.Hour + 30*time.Minute, 11*time.Hour + 30*time.Minute},
	{13 * time.Hour, 14 * time.Hour},
	{15 * time.Hour, 16*time.Hour + 30*time.Minute},
}

// bookingsAt is the place's booking state at t (booked while a meeting runs,
// free between) and the meetings left in its day, the one in progress
// included. Pure.
func bookingsAt(t time.Time) (state string, remaining int) {
	t = t.UTC()
	since := t.Sub(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC))
	state = "free"
	for _, b := range bookingDay {
		if since < b.end {
			remaining++
		}
		if since >= b.start && since < b.end {
			state = "booked"
		}
	}
	return state, remaining
}

// pusher pushes the booking service's readings to the server.
type pusher struct {
	server string
	token  string
	http   *http.Client
	now    func() time.Time
}

func newPusher(server, token string) *pusher {
	return &pusher{server: strings.TrimRight(server, "/"), token: token,
		http: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

// owner is the system the service may push to, off its own grant (GET
// /auth/me): the one grant on a system.
func (p *pusher) owner(ctx context.Context) (string, error) {
	var me struct {
		Grants []struct {
			ScopeKind string `json:"scope_kind"`
			ScopeID   string `json:"scope_id"`
		} `json:"grants"`
	}
	if err := p.call(ctx, http.MethodGet, "/api/v1/auth/me", nil, &me); err != nil {
		return "", err
	}
	for _, g := range me.Grants {
		if g.ScopeKind == "system" && g.ScopeID != "" {
			return g.ScopeID, nil
		}
	}
	return "", fmt.Errorf("the booking service holds no grant on a system")
}

// push sends the system's bookings now.
func (p *pusher) push(ctx context.Context, system string) error {
	state, remaining := bookingsAt(p.now())
	body := map[string]any{
		"owner":      map[string]string{"kind": "system", "ref": system},
		"source":     pusherSource,
		"metrics":    []map[string]any{{"name": "booking-count", "value": remaining}},
		"properties": []map[string]any{{"name": "booking-state", "value": state}},
	}
	var out struct {
		Rejected []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"rejected"`
	}
	if err := p.call(ctx, http.MethodPost, "/api/v1/telemetry:push", body, &out); err != nil {
		return err
	}
	if len(out.Rejected) > 0 {
		return fmt.Errorf("the server rejected %s: %s", out.Rejected[0].Name, out.Rejected[0].Reason)
	}
	return nil
}

func (p *pusher) call(ctx context.Context, method, path string, in, out any) error {
	var rd io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.server+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.http.Do(req)
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

// run pushes on every tick until ctx ends, resolving its system first and
// again after a failure (a lab whose server is still coming up).
func (p *pusher) run(ctx context.Context, every time.Duration, log *slog.Logger) {
	if every <= 0 {
		every = defaultPushEvery
	}
	system := ""
	tick := func() {
		if system == "" {
			s, err := p.owner(ctx)
			if err != nil {
				log.Warn("the booking service cannot read its system yet", "error", err)
				return
			}
			system = s
		}
		if err := p.push(ctx, system); err != nil {
			log.Warn("the booking service's push failed", "error", err)
			system = ""
		}
	}
	tick()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
