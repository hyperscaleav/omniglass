package storage

import "context"

// ExportComponentRuleSelection reads a component's rule selection (its type
// and that type's ancestors, by id) in a transaction of its own, so a test
// can capture it before and after a write.
func (p *PG) ExportComponentRuleSelection(ctx context.Context, componentID string) ([]string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sel, err := componentRuleSelection(ctx, tx, componentID)
	return []string(sel), err
}

// ExportReselectComponentRules runs the rule re-selection step on one
// component, before and after, in a transaction of its own, committed: the
// step a component write (#941) will call in its own.
func (p *PG) ExportReselectComponentRules(ctx context.Context, componentID string, before, after []string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := p.reselectComponentRules(ctx, tx, "", componentID, before, after); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
