package profile

import "fmt"

// resolveExtends folds each profile's base into it, bases first, so a chain
// (a profile extending one that extends another) resolves whole. A base is
// never changed by what extends it.
func (s *Store) resolveExtends() error {
	done := map[string]bool{}
	var resolve func(name string, path []string) error
	resolve = func(name string, path []string) error {
		if done[name] {
			return nil
		}
		for _, seen := range path {
			if seen == name {
				return fmt.Errorf("profile %q extends itself (through %v)", name, append(path, name))
			}
		}
		p := s.Devices[name]
		if p.Extends == "" {
			done[name] = true
			return nil
		}
		if _, ok := s.Devices[p.Extends]; !ok {
			return fmt.Errorf("profile %q extends %q, which is no profile", name, p.Extends)
		}
		// Resolve the base first, then read it: resolving replaces it.
		if err := resolve(p.Extends, append(path, name)); err != nil {
			return err
		}
		merged, err := extend(s.Devices[p.Extends], p)
		if err != nil {
			return err
		}
		for _, i := range merged.Interfaces {
			if i.Protocol == "" {
				return fmt.Errorf("profile %q adds to interface %q, which %q does not have", name, i.Name, p.Extends)
			}
		}
		if err := validateInterfaces(merged); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
		if err := validateComputed(merged); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
		s.Devices[name] = merged
		done[name] = true
		return nil
	}
	for name := range s.Devices {
		if err := resolve(name, nil); err != nil {
			return err
		}
	}
	return nil
}

// extend is p carrying base's surface. The base's interfaces come first, in
// order; one p names again keeps the base's setup (protocol, port, auth,
// framing, welcome: a driver's login may read the welcome as ready) and takes
// p's doc when p gives one; p's routes, commands and subjects replace the
// base's of the same match in place and follow the base's when new, and p's
// OID map entries go over the base's; p's other interfaces follow. Behaviors,
// computed fields and overrides are the base's then p's; defaults are deep
// merged, p's winning; a webhook subscription is p's, else the base's.
// Identity, docs and products are p's alone.
func extend(base, p *DeviceProfile) (*DeviceProfile, error) {
	out := *p
	own := map[string]Interface{}
	for _, i := range p.Interfaces {
		own[i.Name] = i
	}
	inherited := map[string]bool{}
	out.Interfaces = nil
	for _, b := range base.Interfaces {
		inherited[b.Name] = true
		merged := cloneInterface(b)
		if add, ok := own[b.Name]; ok {
			if (add.Protocol != "" && add.Protocol != b.Protocol) || (add.Port != 0 && add.Port != b.Port) {
				return nil, fmt.Errorf("profile %q changes inherited interface %q (%s:%d); add a face of its own instead", p.Name, b.Name, b.Protocol, b.Port)
			}
			if add.Doc.Summary != "" {
				merged.Doc = add.Doc
			}
			merged.Routes = mergeByMatch(merged.Routes, add.Routes, func(r Route) string { return r.Match })
			merged.Commands = mergeByMatch(merged.Commands, add.Commands, func(c Command) string { return c.Match })
			merged.Subjects = mergeByMatch(merged.Subjects, add.Subjects, func(sub Subject) string { return sub.Match })
			if len(add.OIDMap) > 0 {
				if merged.OIDMap == nil {
					merged.OIDMap = map[string]string{}
				}
				for k, v := range add.OIDMap {
					merged.OIDMap[k] = v
				}
			}
		}
		out.Interfaces = append(out.Interfaces, merged)
	}
	for _, i := range p.Interfaces {
		if !inherited[i.Name] {
			out.Interfaces = append(out.Interfaces, i)
		}
	}
	out.Behaviors = append(append([]Behavior(nil), base.Behaviors...), p.Behaviors...)
	out.Computed = append(append([]Computed(nil), base.Computed...), p.Computed...)
	out.Overrides = append(append([]Override(nil), base.Overrides...), p.Overrides...)
	out.Defaults = mergeDefaults(base.Defaults, p.Defaults)
	if out.Webhook == nil {
		out.Webhook = base.Webhook
	}
	return &out, nil
}

// mergeByMatch is base with each of add's entries replacing the base entry of
// the same match in place, or appended after the base's when it is new.
func mergeByMatch[T any](base, add []T, match func(T) string) []T {
	at := map[string]int{}
	for i, b := range base {
		at[match(b)] = i
	}
	for _, a := range add {
		if i, ok := at[match(a)]; ok {
			base[i] = a
			continue
		}
		at[match(a)] = len(base)
		base = append(base, a)
	}
	return base
}

// cloneInterface copies an interface's slices and map, so appending to the
// copy never writes into the base's backing arrays.
func cloneInterface(i Interface) Interface {
	i.Routes = append([]Route(nil), i.Routes...)
	i.Commands = append([]Command(nil), i.Commands...)
	i.Subjects = append([]Subject(nil), i.Subjects...)
	if i.OIDMap != nil {
		m := make(map[string]string, len(i.OIDMap))
		for k, v := range i.OIDMap {
			m[k] = v
		}
		i.OIDMap = m
	}
	return i
}

// mergeDefaults deep-merges over onto a copy of base: maps merge key by key,
// anything else in over replaces. Neither input is changed.
func mergeDefaults(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = copyValue(v)
	}
	for k, v := range over {
		bm, bok := out[k].(map[string]any)
		om, ook := v.(map[string]any)
		if bok && ook {
			out[k] = mergeDefaults(bm, om)
			continue
		}
		out[k] = copyValue(v)
	}
	return out
}

func copyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return mergeDefaults(t, nil)
	case []any:
		c := make([]any, len(t))
		for i, e := range t {
			c[i] = copyValue(e)
		}
		return c
	default:
		return v
	}
}
