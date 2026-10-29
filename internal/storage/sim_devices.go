package storage

import (
	"context"
	"fmt"
	"net"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/impair"
)

// SimDevice is one component a simulator beside a node may serve: the
// component, its product's name (which selects the simulator profile), and
// the host its endpoints target.
type SimDevice struct {
	ComponentID string
	Name        string
	Product     string
	Address     string
	// Impairment is what is broken on the device; nil when it is healthy.
	Impairment *impair.Impairment
}

// NodeSimDevices lists each component with an endpoint placed on the node,
// once, within the read scope: all-scope for the node's own simulator (the
// subject grant has already confined it to its node), the caller's component
// scope for an operator. The address is the host of the component's first
// endpoint, by name, that targets one. An unknown node lists nothing.
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
		select c.id, c.name, pr.name, coalesce(i.params->>'target', ''),
			si.unplugged, si.latency_ms, si.jitter_ms, si.loss_pct::float8, si.rate_kbit
		from endpoint i
		join component c on c.id = i.component
		join product pr on pr.id = c.product_id
		left join sim_impairment si on si.component_id = c.id
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
		var unplugged *bool
		var latency, jitter, rate *int
		var loss *float64
		if err := rows.Scan(&d.ComponentID, &d.Name, &d.Product, &target, &unplugged, &latency, &jitter, &loss, &rate); err != nil {
			return nil, fmt.Errorf("storage: scan sim device: %w", err)
		}
		if unplugged != nil {
			d.Impairment = &impair.Impairment{Unplugged: *unplugged, LatencyMs: *latency, JitterMs: *jitter, LossPct: *loss, RateKbit: *rate}
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

// TargetHost is the host part of an endpoint target: "10.0.0.5:80" and
// "10.0.0.5" are both 10.0.0.5, and "[::1]:80" is ::1.
func TargetHost(target string) string {
	if host, _, err := net.SplitHostPort(target); err == nil {
		return host
	}
	return target
}

// SimWebhook is a simulated component's webhook subscription (ADR-1370): the
// server path its webhook endpoint answers at (the token in it), the scheme
// its driver declares, and the secret the sender signs or authenticates with
// (the hmac key, the token, or user:password; empty for none).
type SimWebhook struct {
	Path   string
	Scheme string
	Secret string
}

// simulatedBlock is the address block the dev seed and the simulated fleet's
// plan allocate simulated components from (internal/simfleet), and nothing
// else does.
var simulatedBlock = &net.IPNet{IP: net.IPv4(127, 60, 0, 0), Mask: net.CIDRMask(16, 32)}

// NodeSimWebhooks reads, for each simulated component on the node, the
// subscription of its webhook endpoint, keyed by component id (ADR-1370). The
// simulator plays the external service that posts to it, so it is handed what
// that service would hold. Any node credential may pull its device list, so
// the fence is two facts the server holds and a node cannot assert: the
// database carries the lab mark (set only by the lab's trusted seed), and the
// component's address is in the simulated block, which only the seed
// allocates. A database with no lab mark hands out nothing, and a component on
// the network or on any other address never appears.
func (p *PG) NodeSimWebhooks(ctx context.Context, name string) (map[string]SimWebhook, error) {
	st, err := p.LabState(ctx)
	if err != nil {
		return nil, err
	}
	if !st.Marked {
		return nil, nil
	}
	devices, err := p.NodeSimDevices(ctx, name, scope.Set{All: true})
	if err != nil {
		return nil, err
	}
	simulated := map[string]bool{}
	for _, d := range devices {
		if ip := net.ParseIP(d.Address); ip != nil && simulatedBlock.Contains(ip) {
			simulated[d.ComponentID] = true
		}
	}
	if len(simulated) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(simulated))
	for id := range simulated {
		ids = append(ids, id)
	}
	rows, err := p.pool.Query(ctx, `
		select e.component::text from webhook_token w join endpoint e on e.id = w.endpoint_id
		where e.component = any($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("storage: node sim webhooks %q: %w", name, err)
	}
	var comps []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan sim webhook: %w", err)
		}
		comps = append(comps, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: node sim webhooks %q: %w", name, err)
	}
	out := map[string]SimWebhook{}
	for _, c := range comps {
		tok, _, err := p.nodeSecretFields(ctx, WebhookTokenSecret, c)
		if err != nil {
			continue // the token was deleted: nothing to post to until it is minted again
		}
		t, err := p.ResolveWebhook(ctx, WebhookTokenHash(tok["token"]))
		if err != nil {
			continue
		}
		out[c] = SimWebhook{Path: "/api/v1/webhooks/" + tok["token"], Scheme: t.Scheme, Secret: webhookSenderSecret(t.Scheme, t.Credential)}
	}
	return out, nil
}

// webhookSenderSecret is the secret a sender holds for a scheme, from the
// endpoint's credential fields: the hmac key, the shared token, or
// user:password.
func webhookSenderSecret(scheme string, f map[string]string) string {
	switch scheme {
	case driver.WebhookHMAC:
		return f["key"]
	case driver.WebhookToken:
		return f["token"]
	case driver.WebhookBasic:
		if f["username"] == "" && f["password"] == "" {
			return ""
		}
		return f["username"] + ":" + f["password"]
	}
	return ""
}
