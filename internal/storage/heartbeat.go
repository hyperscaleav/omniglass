package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/settings"
)

// HeartbeatSettings is the effective heartbeat namespace (#1986): the target
// the server sends to, and the seconds between beats.
func (p *PG) HeartbeatSettings(ctx context.Context) (settings.HeartbeatSettings, error) {
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return settings.HeartbeatSettings{}, fmt.Errorf("storage: heartbeat settings: %w", err)
	}
	return s.Heartbeat, nil
}

// HeartbeatTarget reads the webhook target the heartbeat names, for the
// platform's own send (#1986): no caller's scope, the way a queued
// notification reads its target, since the admin who set the heartbeat's
// target setting chose it. A person-kind target is refused: a heartbeat goes
// to a receiver, not to people.
func (p *PG) HeartbeatTarget(ctx context.Context, name string) (*TargetSend, error) {
	t, err := getNotificationTarget(ctx, p.pool, name, false)
	if err != nil {
		return nil, err
	}
	if t.Kind != ChannelWebhook {
		return nil, fmt.Errorf("%w: a heartbeat goes to a webhook target; %s is a %s target", ErrNotificationTargetInvalid, name, t.Kind)
	}
	return p.targetSend(ctx, p.pool, t.ID)
}

// PlatformAlarmCount is the open alarms on the platform system's components,
// the servers' and nodes' own: the figure a heartbeat carries, so a receiver
// learns the install is up and whether its own alarms are quiet. An install
// with no platform system has none.
func (p *PG) PlatformAlarmCount(ctx context.Context) (int, error) {
	id, err := platformSystemID(ctx, p.pool)
	if err != nil {
		return 0, err
	}
	if id == nil {
		return 0, nil
	}
	var n int
	if err := p.pool.QueryRow(ctx, `
		select count(*) from alarm a
		join system_member m on m.component_id = a.component_id
		where m.system_id = $1 and a.cleared_at is null`, *id).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count open platform alarms: %w", err)
	}
	return n, nil
}

// AuditHeartbeat records a heartbeat's outcome against its target (never the
// URL, the envelope or a credential). The sender calls it on a change of
// outcome, not on every beat, so a steady heartbeat writes no rows.
func (p *PG) AuditHeartbeat(ctx context.Context, targetID string, ok bool, status int, reason string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin audit heartbeat: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := writeAuditRes(ctx, tx, "", "heartbeat", "notification_target", targetID, nil,
		map[string]any{"ok": ok, "status": status, "reason": reason}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
