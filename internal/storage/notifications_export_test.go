package storage

import (
	"context"
	"errors"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// errRolledBack is the failure ExportRaiseThenFail injects after the raise.
var errRolledBack = errors.New("storage test: the transaction is rolled back")

// ExportRaiseThenFail raises an alarm through the shared raise seam inside a
// transaction that then fails, so a test can prove the notifications the
// raise queued roll back with it.
func (p *PG) ExportRaiseThenFail(ctx context.Context, componentName string, spec AlarmSpec) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	c, err := scopedByName(ctx, tx, componentConfig, componentName)
	if err != nil {
		return err
	}
	if _, err := p.raiseAlarmTx(ctx, tx, "", c, spec, nil); err != nil {
		return err
	}
	return errRolledBack
}

// ExportRuleRaiseAndClear raises, then clears, a named event rule's alarm on
// a component through the rule evaluator's own seams (raiseRuleAlarm, then
// clearRuleAlarmTx), each in its own transaction.
func (p *PG) ExportRuleRaiseAndClear(ctx context.Context, ruleName, componentName string, clear bool) (string, error) {
	r, err := p.GetEventRule(ctx, ruleName, scope.Set{All: true})
	if err != nil {
		return "", err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	c, err := scopedByName(ctx, tx, componentConfig, componentName)
	if err != nil {
		return "", err
	}
	var id string
	if clear {
		id, err = p.clearRuleAlarmTx(ctx, tx, "", *r, c, "")
	} else {
		id, err = p.raiseRuleAlarm(ctx, tx, "", *r, c, "")
	}
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

// ExportDueNow makes every queued notification due, so a test need not wait
// out a backoff.
func (p *PG) ExportDueNow(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `update notification set next_attempt_at = now() where status = 'queued'`)
	return err
}

// ExportServicePrincipal creates a service account principal named name and,
// when group is set, adds it to that group: a non-human member a group
// target's expansion must skip.
func (p *PG) ExportServicePrincipal(ctx context.Context, name, group string) (string, error) {
	var id string
	if err := p.pool.QueryRow(ctx, `insert into principal (kind) values ('service') returning id::text`).Scan(&id); err != nil {
		return "", err
	}
	if _, err := p.pool.Exec(ctx, `insert into service (principal_id, name) values ($1, $2)`, id, name); err != nil {
		return "", err
	}
	if group != "" {
		if _, err := p.pool.Exec(ctx, `insert into principal_group_member (group_id, principal_id)
			select id, $2 from principal_group where name = $1`, group, id); err != nil {
			return "", err
		}
	}
	return id, nil
}
