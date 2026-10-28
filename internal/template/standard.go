package template

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// StandardOptions are the choices beyond the template when creating a standard from it: the name the
// standard takes (the template's by default), its label (the template's by
// default) and, for a variant, the standard it hangs off (the operator's
// standard named for the template's parent by default).
type StandardOptions struct {
	Name   string
	Label  string
	Parent string
}

// ErrNotAStandard is a standard asked of a template that is not a standard template.
var ErrNotAStandard = errors.New("template: only a standard template creates a standard")

// StandardFromTemplate is what creating a standard from t writes: the storage spec naming everything
// by value. It reads nothing; the create resolves every name in its
// transaction.
func StandardSpec(t *Template, opt StandardOptions) (storage.StandardFromTemplate, error) {
	if t.Kind != KindStandard || t.Standard == nil {
		return storage.StandardFromTemplate{}, fmt.Errorf("%w: %s is a %s", ErrNotAStandard, t.Name, t.Kind)
	}
	s := t.Standard
	f := storage.StandardFromTemplate{
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
		f.Roles = append(f.Roles, storage.TemplateRole{
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
			return storage.StandardFromTemplate{}, fmt.Errorf("template: %s map: %w", t.Name, err)
		}
		f.Map = b
	}
	return f, nil
}

// CreateStandardFromTemplate creates a standard from the named standard template through gw: the one path
// the API route, the dev seed and the lab share. A name the catalog lacks,
// or names as another kind, is ErrNotFound.
func CreateStandardFromTemplate(ctx context.Context, gw storage.Gateway, c *Catalog, actorID, template string, opt StandardOptions) (*storage.Standard, error) {
	t, err := c.GetKind(template, KindStandard)
	if err != nil {
		return nil, err
	}
	f, err := StandardSpec(t, opt)
	if err != nil {
		return nil, err
	}
	return gw.CreateStandardFromTemplate(ctx, actorID, f)
}

// CreateAllStandards creates a standard from every standard template the operator does not
// already hold a standard of that name for, parents before variants, and
// returns the names it created. A standard already present is left exactly
// as it is: creating from a template never writes over an operator's row. The dev seed and
// the lab use it so their fleets have the standards they conform to.
func CreateAllStandards(ctx context.Context, gw storage.Gateway, c *Catalog, actorID string) ([]string, error) {
	var created []string
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
				return created, fmt.Errorf("template: read standard %s: %w", t.Name, err)
			}
			if p := t.Standard.Parent; p != "" {
				if _, err := gw.GetStandard(ctx, p); errors.Is(err, storage.ErrTypeNotFound) {
					next = append(next, *t)
					continue
				}
			}
			if _, err := CreateStandardFromTemplate(ctx, gw, c, actorID, t.Name, StandardOptions{}); err != nil {
				return created, fmt.Errorf("template: create %s from its template: %w", t.Name, err)
			}
			created = append(created, t.Name)
			progressed = true
		}
		if !progressed {
			return created, fmt.Errorf("template: %d standard templates wait on a parent nothing creates (first: %s)", len(next), next[0].Name)
		}
		pending = next
	}
	return created, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
