// Package heartbeat is the server's outbound dead-man's switch (#1986): an
// optional signed request, sent on an interval to a notification target, that
// a monitor outside this install watches for the absence of. An alarm raised
// inside the platform cannot report the platform's own death, so the one
// signal that can is a message that stops arriving.
//
// It rides the machinery a notification does: the target is a webhook
// notification target, the request is built and signed by internal/notify,
// and it is sent by internal/egress under the install's allow list, so an
// operator's destination policy, bound secret and audit apply. It is off
// until the heartbeat settings name a target.
package heartbeat

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/notify"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Type is the notification type a heartbeat carries, and the transition its
// envelope names: it is about no alarm.
const Type = "heartbeat"

// idleEvery is how often a sender with no target re-reads its settings, so
// naming a target takes within a moment, with no restart.
const idleEvery = 10 * time.Second

// Store is the slice of the Storage Gateway the sender uses.
type Store interface {
	HeartbeatSettings(ctx context.Context) (settings.HeartbeatSettings, error)
	HeartbeatTarget(ctx context.Context, name string) (*storage.TargetSend, error)
	EgressPolicy(ctx context.Context) (egress.Policy, error)
	PlatformAlarmCount(ctx context.Context) (int, error)
	AuditHeartbeat(ctx context.Context, targetID string, ok bool, status int, reason string) error
}

// Envelope is the body of one heartbeat: the server's name, the build it
// runs and the open alarms on the platform's own components, so a receiver
// learns the install is up and whether its own alarms are quiet. Pure.
func Envelope(id, server, build string, openAlarms int) notify.Envelope {
	open := strconv.Itoa(openAlarms)
	return notify.Envelope{
		ID: id, Type: Type, Transition: Type,
		Subject: "Omniglass heartbeat from " + server,
		Text:    server + " (" + build + ") is running with " + open + " open platform alarms.",
		Facts:   map[string]string{"server": server, "build": build, "open_platform_alarms": open},
	}
}

// Sender sends the heartbeat.
type Sender struct {
	store         Store
	opts          egress.Options
	server, build string
	now           func() time.Time
	// unit is the length of one interval second; a test shortens it.
	unit time.Duration
	// last is the outcome the previous beat audited, so a steady heartbeat
	// writes one audit row per change of outcome, not one per beat.
	last *outcome
}

// outcome is one beat's result against the target it went to; a move to
// another target is a change of outcome even when both succeed.
type outcome struct {
	target string
	ok     bool
	status int
	reason string
}

// New returns a sender for the server named server running build; opts tunes
// its outbound client (a test's certificate roots).
func New(store Store, opts egress.Options, server, build string) *Sender {
	return &Sender{store: store, opts: opts, server: server, build: build, now: time.Now, unit: time.Second}
}

// Beat sends one heartbeat, if the settings name a target. A send the
// receiver refused, or that the egress contract did, is an error carrying the
// reason; no target is not an error, the heartbeat is off. A target the
// heartbeat will not use (disabled, its secret not unsealing, or no hmac key
// to sign with) is refused before any request and the refusal audited: a beat
// never goes out unsigned.
func (s *Sender) Beat(ctx context.Context) error {
	hb, err := s.store.HeartbeatSettings(ctx)
	if err != nil {
		return err
	}
	if hb.Target == "" {
		return nil
	}
	ts, err := s.store.HeartbeatTarget(ctx, hb.Target)
	if err != nil {
		return fmt.Errorf("heartbeat target %q: %w", hb.Target, err)
	}
	if reason := refusal(ts); reason != "" {
		s.audit(ctx, outcome{target: ts.TargetID, reason: reason})
		return fmt.Errorf("heartbeat target %q: %s", hb.Target, reason)
	}
	pol, err := s.store.EgressPolicy(ctx)
	if err != nil {
		return err
	}
	open, err := s.store.PlatformAlarmCount(ctx)
	if err != nil {
		return err
	}
	req, err := notify.Build(ts.URL, Envelope(uuid.NewString(), s.server, s.build, open),
		notify.Credential{HMACKey: ts.HMACKey, BearerToken: ts.BearerToken}, s.now())
	if err != nil {
		return err
	}
	res := egress.New(pol, s.opts).Post(ctx, req)
	s.audit(ctx, outcome{ts.TargetID, res.OK, res.Status, res.Reason})
	if !res.OK {
		return fmt.Errorf("heartbeat to %q failed: %s", hb.Target, res.Reason)
	}
	return nil
}

// refusal is why the heartbeat will not send to ts, or empty when it will.
// Pure.
func refusal(ts *storage.TargetSend) string {
	switch {
	case !ts.Enabled:
		return "the target is disabled"
	case ts.CredentialMissing:
		return "its secret no longer unseals; bind it again"
	case ts.HMACKey == "":
		return "it has no hmac-key secret bound, and a heartbeat is never sent unsigned"
	}
	return ""
}

// audit records o when it differs from the previous beat's outcome.
func (s *Sender) audit(ctx context.Context, o outcome) {
	if s.last != nil && *s.last == o {
		return
	}
	if err := s.store.AuditHeartbeat(ctx, o.target, o.ok, o.status, o.reason); err != nil {
		slog.Warn("heartbeat audit failed", "facility", "heartbeat", "error", err.Error())
		return
	}
	s.last = &o
}

// Run beats until ctx ends: once at start, then every interval the settings
// name, re-read each beat so a change takes without a restart. A beat that
// fails is logged and the next goes on schedule; a beat in flight when ctx
// ends is abandoned with it, so none is sent after the server stops.
func (s *Sender) Run(ctx context.Context) {
	for {
		wait := idleEvery
		if hb, err := s.store.HeartbeatSettings(ctx); err == nil && hb.Target != "" {
			if err := s.Beat(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("heartbeat not sent", "facility", "heartbeat", "error", err.Error())
			}
			wait = time.Duration(hb.IntervalSeconds) * s.unit
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
