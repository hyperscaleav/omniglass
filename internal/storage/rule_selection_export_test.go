package storage

import (
	"context"
	"fmt"
)

// ExportReselectComponentRules runs one write in a transaction between
// reading a component's rule selection (by name) before and after it, then
// the reselect step, and commits: what a caller wiring the step does (#941).
func ExportReselectComponentRules(ctx context.Context, p *PG, component, write string, args ...any) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if err := tx.QueryRow(ctx, `select id::text from component where name = $1`, component).Scan(&id); err != nil {
		return fmt.Errorf("component %s: %w", component, err)
	}
	before, err := readComponentSelection(ctx, tx, id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, write, args...); err != nil {
		return err
	}
	after, err := readComponentSelection(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := p.reselectComponentRules(ctx, tx, "", before, after); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ExportReselectCandidates names the enabled event rules a reselect between
// two type lineages considers, in id order.
func ExportReselectCandidates(ctx context.Context, p *PG, before, after []string) ([]string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rs, err := reselectCandidates(ctx, tx, reselectNames(componentSelection{lineage: before}, componentSelection{lineage: after}))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rs))
	for _, r := range rs {
		names = append(names, r.Name)
	}
	return names, nil
}
