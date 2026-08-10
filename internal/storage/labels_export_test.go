package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/label"
)

// The label generator's seam for the external test package, the same shim
// pattern ExportGenerateName established: both live in _test.go files, so they
// exist only in the test binary and ship in nothing.
//
// Two things #682 has to prove are not reachable from outside the package.
//
// The recompute-and-compare invariant needs "what WOULD this row's label be",
// which is a read nothing in production wants: a label is stored, and the
// gateway's answer to "what is this row's label" is the column. Slice 5's bulk
// recompute is what promotes this to a real gateway method.
//
// The sandbox test needs the data map ITSELF, not a rendering of it. The
// security argument is that a secret is absent from the map rather than
// filtered from a syntax, and the only way to assert an absence is to hold the
// whole set of keys and check what is in it.

// ExportRenderComponentLabel re-renders a component's label from its current
// rules and facts, writing nothing.
func (p *PG) ExportRenderComponentLabel(ctx context.Context, id string) (string, error) {
	c, err := scanComponent(p.pool.QueryRow(ctx, `select `+componentCols+` from component where id = $1`, id))
	if err != nil {
		return "", fmt.Errorf("storage: load component %q for label recompute: %w", id, err)
	}
	eng, err := p.labelEngine(ctx)
	if err != nil {
		return "", err
	}
	return renderComponentLabel(ctx, p.pool, eng, c)
}

// ExportRenderSystemLabel is ExportRenderComponentLabel on the system tier.
func (p *PG) ExportRenderSystemLabel(ctx context.Context, id string) (string, error) {
	s, err := scanSystem(p.pool.QueryRow(ctx, `select `+systemCols+` from system where id = $1`, id))
	if err != nil {
		return "", fmt.Errorf("storage: load system %q for label recompute: %w", id, err)
	}
	eng, err := p.labelEngine(ctx)
	if err != nil {
		return "", err
	}
	return renderSystemLabel(ctx, p.pool, eng, s)
}

// ExportRenderLocationLabel is ExportRenderComponentLabel on the location tier.
func (p *PG) ExportRenderLocationLabel(ctx context.Context, id string) (string, error) {
	l, err := scanLocation(p.pool.QueryRow(ctx, `select `+locationCols+` from location where id = $1`, id))
	if err != nil {
		return "", fmt.Errorf("storage: load location %q for label recompute: %w", id, err)
	}
	eng, err := p.labelEngine(ctx)
	if err != nil {
		return "", err
	}
	return renderLocationLabel(ctx, p.pool, eng, l)
}

// ExportComponentLabelData returns the whole closed data map a component's
// rules execute over: the sandbox itself, for a test that asserts what is NOT
// in it.
func (p *PG) ExportComponentLabelData(ctx context.Context, id string) (label.Data, error) {
	c, err := scanComponent(p.pool.QueryRow(ctx, `select `+componentCols+` from component where id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("storage: load component %q for label data: %w", id, err)
	}
	if c.ProductID == nil {
		return nil, fmt.Errorf("storage: component %q has no product to resolve label facts from", id)
	}
	in, err := componentLabelChain(ctx, p.pool, *c.ProductID)
	if err != nil {
		return nil, err
	}
	return componentLabelData(c, in), nil
}

// ExportSystemLabelData is ExportComponentLabelData on the system tier.
func (p *PG) ExportSystemLabelData(ctx context.Context, id string) (label.Data, error) {
	s, err := scanSystem(p.pool.QueryRow(ctx, `select `+systemCols+` from system where id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("storage: load system %q for label data: %w", id, err)
	}
	in, err := systemLabelChain(ctx, p.pool, s)
	if err != nil {
		return nil, err
	}
	return systemLabelData(s, in), nil
}

// ExportLocationLabelData is ExportComponentLabelData on the location tier.
func (p *PG) ExportLocationLabelData(ctx context.Context, id string) (label.Data, error) {
	l, err := scanLocation(p.pool.QueryRow(ctx, `select `+locationCols+` from location where id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("storage: load location %q for label data: %w", id, err)
	}
	in, err := locationLabelChain(ctx, p.pool, l)
	if err != nil {
		return nil, err
	}
	return locationLabelData(l, in), nil
}
