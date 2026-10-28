package template

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Check dry-runs every template in the catalog against the catalogs a
// database holds (the seeded registries, in the build's test), reading only:
// each product, type, driver input, property and metric a template names
// must exist there, so a template naming a row a release withdrew fails the
// build, naming the template and the row, rather than an operator. It
// returns one error per fault found, in catalog order.
func Check(ctx context.Context, c *Catalog, gw storage.Gateway) []error {
	var errs []error
	for _, t := range c.List("") {
		for _, err := range checkOne(ctx, c, gw, &t) {
			errs = append(errs, fmt.Errorf("%s %s: %w", t.Kind, t.Name, err))
		}
	}
	return errs
}

func checkOne(ctx context.Context, c *Catalog, gw storage.Gateway, t *Template) []error {
	switch t.Kind {
	case KindStandard:
		return checkStandard(ctx, gw, t.Standard)
	case KindComponent:
		return checkComponent(ctx, gw, t.Component)
	case KindSystem:
		return checkSystem(ctx, c, gw, t.System)
	case KindPlace:
		return checkPlace(ctx, gw, t.Place, "")
	}
	return nil
}

func checkStandard(ctx context.Context, gw storage.Gateway, s *Standard) []error {
	var errs []error
	for _, r := range s.Roles {
		for _, ct := range r.AcceptedTypes {
			if _, err := gw.GetComponentType(ctx, ct); err != nil {
				errs = append(errs, fmt.Errorf("role %s accepts component type %q: %w", r.Name, ct, err))
			}
		}
		for _, p := range r.PinnedProducts {
			if _, err := gw.GetProduct(ctx, p); err != nil {
				errs = append(errs, fmt.Errorf("role %s pins product %q: %w", r.Name, p, err))
			}
		}
	}
	for _, l := range s.Props {
		if _, err := gw.GetPropertyType(ctx, l.Name); err != nil {
			errs = append(errs, fmt.Errorf("declares property %q: %w", l.Name, err))
		}
	}
	for _, l := range s.Metrics {
		if _, err := gw.GetMetricType(ctx, l.Name); err != nil {
			errs = append(errs, fmt.Errorf("declares metric %q: %w", l.Name, err))
		}
	}
	return errs
}

func checkComponent(ctx context.Context, gw storage.Gateway, comp *Component) []error {
	var errs []error
	prod, err := gw.GetProduct(ctx, comp.Product)
	if err != nil {
		return []error{fmt.Errorf("names product %q: %w", comp.Product, err)}
	}
	drv := comp.Attach.Driver
	if drv == "" && prod.DriverName != nil {
		drv = *prod.DriverName
	}
	switch {
	case drv == "" && len(comp.Attach.Inputs) > 0:
		errs = append(errs, fmt.Errorf("presets driver inputs, but product %q names no driver", comp.Product))
	case drv != "":
		errs = append(errs, checkPreset(ctx, gw, drv, comp.Attach.Inputs)...)
	}
	errs = append(errs, checkValues(ctx, gw, comp.Properties)...)
	return errs
}

// checkPreset holds an attach preset to the driver's declared inputs: each
// preset input must be one the driver declares, and the driver must carry a
// spec (a stub cannot be attached).
func checkPreset(ctx context.Context, gw storage.Gateway, drv string, inputs map[string]string) []error {
	d, err := gw.GetDriver(ctx, drv)
	if err != nil {
		return []error{fmt.Errorf("attaches driver %q: %w", drv, err)}
	}
	if len(d.Spec) == 0 {
		return []error{fmt.Errorf("attaches driver %q, a stub with no spec", drv)}
	}
	var sp driver.Spec
	if err := json.Unmarshal(d.Spec, &sp); err != nil {
		return []error{fmt.Errorf("attaches driver %q: read its spec: %w", drv, err)}
	}
	declared := map[string]bool{}
	for _, in := range sp.Inputs {
		declared[in.Name] = true
	}
	var errs []error
	for _, name := range sortedKeys(inputs) {
		if !declared[name] {
			errs = append(errs, fmt.Errorf("presets input %q, which driver %q does not declare", name, drv))
		}
	}
	return errs
}

func checkValues(ctx context.Context, gw storage.Gateway, v Values) []error {
	var errs []error
	for _, name := range v.Names() {
		if _, err := gw.GetPropertyType(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("declares a value for property %q: %w", name, err))
		}
	}
	return errs
}

func checkSystem(ctx context.Context, c *Catalog, gw storage.Gateway, s *System) []error {
	var errs []error
	if s.SystemType != "" {
		if _, err := gw.GetSystemType(ctx, s.SystemType); err != nil {
			errs = append(errs, fmt.Errorf("names system type %q: %w", s.SystemType, err))
		}
	}
	var pins map[string][]string
	if st, err := c.GetKind(s.Standard, KindStandard); err == nil {
		pins = map[string][]string{}
		for _, r := range st.Standard.Roles {
			pins[r.Name] = r.PinnedProducts
		}
	}
	for _, role := range sortedKeys(s.Roles) {
		product := s.Roles[role].Product
		if comp := s.Roles[role].Component; comp != "" {
			ct, err := c.GetKind(comp, KindComponent)
			if err != nil {
				errs = append(errs, fmt.Errorf("role %s: %w", role, err))
				continue
			}
			product = ct.Component.Product
		}
		if _, err := gw.GetProduct(ctx, product); err != nil {
			errs = append(errs, fmt.Errorf("role %s names product %q: %w", role, product, err))
			continue
		}
		if p := pins[role]; len(p) > 0 && !slices.Contains(p, product) {
			errs = append(errs, fmt.Errorf("role %s fills with product %q, but the standard pins %v", role, product, p))
		}
	}
	errs = append(errs, checkValues(ctx, gw, s.Properties)...)
	return errs
}

// checkPlace holds each place's type to the shipped location types: the
// type must exist, and a nested place's type must allow its parent's. The
// root's placement is the caller's to choose, so it is checked when the
// template is instantiated.
func checkPlace(ctx context.Context, gw storage.Gateway, p *Place, parentType string) []error {
	var errs []error
	lt, err := gw.GetLocationType(ctx, p.Type)
	switch {
	case errors.Is(err, storage.ErrTypeNotFound):
		errs = append(errs, fmt.Errorf("names location type %q, which the catalog does not hold", p.Type))
	case err != nil:
		errs = append(errs, fmt.Errorf("names location type %q: %w", p.Type, err))
	case parentType != "" && !slices.Contains(lt.AllowedParentTypes, parentType):
		errs = append(errs, fmt.Errorf("places a %s under a %s, which %s does not allow", p.Type, parentType, p.Type))
	}
	for i := range p.Places {
		errs = append(errs, checkPlace(ctx, gw, &p.Places[i], p.Type)...)
	}
	return errs
}
