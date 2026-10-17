package storage

import (
	"context"
	"fmt"
	"net"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// SimDevice is one component a simulator beside a node may serve: the
// component, its product's name (which selects the simulator profile), and
// the host its interfaces target.
type SimDevice struct {
	ComponentID string
	Name        string
	Product     string
	Address     string
}

// NodeSimDevices lists each component with an interface placed on the node,
// once, within the read scope: all-scope for the node's own simulator (the
// subject grant has already confined it to its node), the caller's component
// scope for an operator. The address is the host of the component's first
// interface, by name, that targets one. An unknown node lists nothing.
func (p *PG) NodeSimDevices(ctx context.Context, name string, read scope.Set) ([]SimDevice, error) {
	if read.Empty() {
		return nil, nil
	}
	var visible map[string]bool
	if !read.All {
		// The scoped-tree filter is the one component scope primitive; the
		// list keeps only what it returns.
		comps, err := scopedList(ctx, p, componentConfig, read)
		if err != nil {
			return nil, err
		}
		visible = make(map[string]bool, len(comps))
		for _, c := range comps {
			visible[c.ID] = true
		}
	}
	rows, err := p.pool.Query(ctx, `
		select c.id, c.name, pr.name, coalesce(i.params->>'target', '')
		from interface i
		join component c on c.id = i.component
		join product pr on pr.id = c.product_id
		where i.node_name = (select principal_id from node where name = $1)
		order by c.id, i.name`, name)
	if err != nil {
		return nil, fmt.Errorf("storage: node sim devices %q: %w", name, err)
	}
	defer rows.Close()
	var out []SimDevice
	for rows.Next() {
		var d SimDevice
		var target string
		if err := rows.Scan(&d.ComponentID, &d.Name, &d.Product, &target); err != nil {
			return nil, fmt.Errorf("storage: scan sim device: %w", err)
		}
		if visible != nil && !visible[d.ComponentID] {
			continue
		}
		d.Address = TargetHost(target)
		if n := len(out); n > 0 && out[n-1].ComponentID == d.ComponentID {
			if out[n-1].Address == "" {
				out[n-1].Address = d.Address
			}
			continue
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: node sim devices %q: %w", name, err)
	}
	return out, nil
}

// TargetHost is the host part of an interface target: "10.0.0.5:80" and
// "10.0.0.5" are both 10.0.0.5, and "[::1]:80" is ::1.
func TargetHost(target string) string {
	if host, _, err := net.SplitHostPort(target); err == nil {
		return host
	}
	return target
}
