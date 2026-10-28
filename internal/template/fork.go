package template

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// ForkOptions are a fork's choices beyond the template: the name the
// standard takes (the template's by default), its label (the template's by
// default) and, for a variant, the standard it hangs off (the operator's
// standard named for the template's parent by default).
type ForkOptions struct {
	Name   string
	Label  string
	Parent string
}

// ErrNotAStandard is a fork of a template that is not a standard template.
var ErrNotAStandard = errors.New("template: only a standard template forks into a standard")

// StandardFork is what forking t writes: the storage spec naming everything
// by value. It reads nothing; the fork resolves every name in its
// transaction.
func StandardFork(t *Template, opt ForkOptions) (storage.StandardFork, error) {
	if t.Kind != KindStandard || t.Standard == nil {
		return storage.StandardFork{}, fmt.Errorf("%w: %s is a %s", ErrNotAStandard, t.Name, t.Kind)
	}
	s := t.Standard
	f := storage.StandardFork{
		Template: t.Name,
		Name:     orDefault(opt.Name, t.Name),
		Label:    orDefault(opt.Label, t.Label),
		Parent:   orDefault(opt.Parent, s.Parent),
	}
	for _, c := range s.Choices {
		spec := storage.RoleChoiceSpec{Name: c.Name, Label: c.Label}
		for _, a := range c.Alternates {
			spec.Alternates = append(spec.Alternates, storage.AlternateSpec{Name: a.Name, Label: a.Label})
		}
		f.Choices = append(f.Choices, spec)
	}
	for _, r := range s.Roles {
		f.Roles = append(f.Roles, storage.ForkRole{
			SystemRoleSpec: storage.SystemRoleSpec{
				Name: r.Name, Label: r.Label, Quorum: r.Quorum, Capacity: r.Capacity, Impact: r.Impact,
				AcceptedTypes: r.AcceptedTypes, PinnedProducts: r.PinnedProducts,
			},
			Choice: r.Choice, Alternate: r.Alternate,
		})
	}
	for _, l := range s.Props {
		f.Properties = append(f.Properties, storage.StandardPropertySpec{PropertyTypeName: l.Name, DefaultValue: l.Default, Required: l.Required})
	}
	for _, l := range s.Metrics {
		f.Metrics = append(f.Metrics, storage.StandardMetricSpec{MetricTypeName: l.Name, DefaultValue: l.Default, Required: l.Required})
	}
	if s.Map != nil {
		b, err := json.Marshal(s.Map)
		if err != nil {
			return storage.StandardFork{}, fmt.Errorf("template: %s map: %w", t.Name, err)
		}
		f.Map = b
	}
	return f, nil
}

// ForkStandard forks the named standard template through gw: the one path
// the API route, the dev seed and the lab share. A name the catalog lacks,
// or names as another kind, is ErrNotFound.
func ForkStandard(ctx context.Context, gw storage.Gateway, c *Catalog, actorID, template string, opt ForkOptions) (*storage.Standard, error) {
	t, err := c.GetKind(template, KindStandard)
	if err != nil {
		return nil, err
	}
	f, err := StandardFork(t, opt)
	if err != nil {
		return nil, err
	}
	return gw.ForkStandard(ctx, actorID, f)
}

// ForkAllStandards forks every standard template the operator does not
// already hold a standard of that name for, parents before variants, and
// returns the names it forked. A standard already present is left exactly
// as it is: the fork never writes over an operator's row. The dev seed and
// the lab use it so their fleets have the standards they conform to.
func ForkAllStandards(ctx context.Context, gw storage.Gateway, c *Catalog, actorID string) ([]string, error) {
	var forked []string
	pending := c.List(KindStandard)
	for len(pending) > 0 {
		var next []Template
		progressed := false
		for i := range pending {
			t := &pending[i]
			if _, err := gw.GetStandard(ctx, t.Name); err == nil {
				progressed = true
				continue
			} else if !errors.Is(err, storage.ErrTypeNotFound) {
				return forked, fmt.Errorf("template: read standard %s: %w", t.Name, err)
			}
			if p := t.Standard.Parent; p != "" {
				if _, err := gw.GetStandard(ctx, p); errors.Is(err, storage.ErrTypeNotFound) {
					next = append(next, *t)
					continue
				}
			}
			if _, err := ForkStandard(ctx, gw, c, actorID, t.Name, ForkOptions{}); err != nil {
				return forked, fmt.Errorf("template: fork %s: %w", t.Name, err)
			}
			forked = append(forked, t.Name)
			progressed = true
		}
		if !progressed {
			return forked, fmt.Errorf("template: %d standard templates wait on a parent nothing forks (first: %s)", len(next), next[0].Name)
		}
		pending = next
	}
	return forked, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
