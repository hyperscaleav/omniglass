package api

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The command palette's name search (#2444): one read that answers "what is
// called this" across the fleet and the catalog, so the palette never fetches a
// collection to filter it in the browser.
//
// The fleet kinds (location, system, component) are searched in the database
// through the Storage Gateway's scoped list, each on its own read scope, so a
// caller sees only what their grants reach and the limit is applied before the
// rows leave Postgres. The catalog kinds are small platform-wide lists behind
// one permission each; they are read through the same Gateway reads their list
// routes use and matched here, each only for a caller who holds its read
// permission.

type searchEntityBody struct {
	ID           string `json:"id" doc:"The entity's uuid, the address its page opens at"`
	Name         string `json:"name" doc:"The entity's name; many components share one, the location_path tells them apart"`
	Label        string `json:"label,omitempty" doc:"The entity's display label, when it has one"`
	LocationPath string `json:"location_path,omitempty" doc:"The locations it sits under, outermost first, joined with ' / '. Empty for a component that has no location of its own and is in a system; its system field says where it is"`
	System       string `json:"system,omitempty" doc:"A component's primary system, when it has one"`
}

type searchCatalogBody struct {
	Kind   string `json:"kind" enum:"product,driver,calc_rule,event_rule,metric" doc:"Which catalog the entry belongs to; the console opens its drawer on that catalog's page"`
	Name   string `json:"name" doc:"The entry's name, its address in the catalog"`
	Label  string `json:"label,omitempty" doc:"The entry's display label"`
	Detail string `json:"detail,omitempty" doc:"A short qualifier: a product's vendor, a driver's version, a metric's unit"`
}

type searchInput struct {
	Q     string `query:"q" minLength:"1" maxLength:"200" doc:"The text to find, matched case-insensitively anywhere in a name or label" example:"auditorium"`
	Limit int    `query:"limit" minimum:"1" maximum:"20" default:"8" doc:"The most hits returned for each kind"`
}

type searchOutput struct {
	Body struct {
		Locations  []searchEntityBody  `json:"locations"`
		Systems    []searchEntityBody  `json:"systems"`
		Components []searchEntityBody  `json:"components"`
		Catalog    []searchCatalogBody `json:"catalog" doc:"Catalog hits across products, drivers, calc rules, event rules and metrics, best match first, at most limit of each kind"`
	}
}

// locationPathOf is the location chain in a tree entity's address: the segments
// before its own accessor, outermost first. A location's address is the chain
// itself, so its path is everything above it.
func locationPathOf(segments []string, isLocation bool) string {
	var chain []string
	for _, s := range segments {
		if strings.HasPrefix(s, "$") {
			break
		}
		chain = append(chain, s)
	}
	if isLocation && len(chain) > 0 {
		chain = chain[:len(chain)-1]
	}
	return strings.Join(chain, " / ")
}

// rankCatalog keeps the hits whose name or label holds text and returns at
// most limit, best first: exact, then prefix, then the rest by name.
func rankCatalog(all []searchCatalogBody, text string, limit int) []searchCatalogBody {
	q := strings.ToLower(strings.TrimSpace(text))
	type scored struct {
		b    searchCatalogBody
		rank int
	}
	var hits []scored
	for _, b := range all {
		n, l := strings.ToLower(b.Name), strings.ToLower(b.Label)
		switch {
		case !strings.Contains(n, q) && !strings.Contains(l, q):
			continue
		case n == q || l == q:
			hits = append(hits, scored{b, 0})
		case strings.HasPrefix(n, q) || strings.HasPrefix(l, q):
			hits = append(hits, scored{b, 1})
		default:
			hits = append(hits, scored{b, 2})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return hits[i].b.Name < hits[j].b.Name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]searchCatalogBody, len(hits))
	for i, h := range hits {
		out[i] = h.b
	}
	return out
}

func registerSearchRoutes(api huma.API, a *authenticator, gw storage.Gateway, log *slog.Logger) {
	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID: "search",
		Method:      http.MethodGet,
		Path:        "/search",
		Summary:     "Find locations, systems, components and catalog entries by name",
		Description: "The read behind the command palette. Returns up to limit hits of each kind whose name or label holds q, case-insensitively, best match first (exact, then prefix, then the rest by name). Locations, systems and components are searched inside the caller's own read scope for each kind, so nothing outside it appears; a caller with no scope for a kind gets an empty group, not an error. Each fleet hit carries its location_path, so many components with one name are told apart. Catalog hits cover products (product:read), drivers (driver:read), calc rules and event rules (rule:read) and metrics (metric_type:read), each only for a caller who holds that permission. Gated by location:read, the floor of every fleet reader; the component group also needs component:read.",
	}, "location", "read"), "component", "read"), func(ctx context.Context, in *searchInput) (*searchOutput, error) {
		if strings.TrimSpace(in.Q) == "" {
			return nil, huma.Error422UnprocessableEntity("q must name something to find")
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 8
		}
		out := &searchOutput{}
		out.Body.Locations, out.Body.Systems, out.Body.Components = []searchEntityBody{}, []searchEntityBody{}, []searchEntityBody{}
		out.Body.Catalog = []searchCatalogBody{}

		found, err := gw.SearchFleet(ctx, in.Q, limit,
			a.scopeFor(ctx, "location", "read"),
			a.scopeFor(ctx, "system", "read"),
			a.scopeFor(ctx, "component", "read"),
		)
		if err != nil {
			log.ErrorContext(ctx, "search the fleet", "error", err)
			return nil, huma.Error500InternalServerError("search")
		}
		for _, l := range found.Locations {
			out.Body.Locations = append(out.Body.Locations, searchEntityBody{ID: l.ID, Name: l.Name, Label: l.Label, LocationPath: locationPathOf(l.PathSegments, true)})
		}
		for _, s := range found.Systems {
			out.Body.Systems = append(out.Body.Systems, searchEntityBody{ID: s.ID, Name: s.Name, Label: s.Label, LocationPath: locationPathOf(s.PathSegments, false)})
		}
		for _, c := range found.Components {
			b := searchEntityBody{ID: c.ID, Name: c.Name, Label: c.Label, LocationPath: locationPathOf(c.PathSegments, false)}
			if c.PrimarySystem != nil {
				b.System = *c.PrimarySystem
			}
			out.Body.Components = append(out.Body.Components, b)
		}

		cat, err := searchCatalog(ctx, a, gw, in.Q, limit)
		if err != nil {
			log.ErrorContext(ctx, "search the catalog", "error", err)
			return nil, huma.Error500InternalServerError("search")
		}
		out.Body.Catalog = cat
		return out, nil
	})
}

// searchCatalog reads each catalog the caller may read and ranks the matches.
func searchCatalog(ctx context.Context, a *authenticator, gw storage.Gateway, text string, limit int) ([]searchCatalogBody, error) {
	out := []searchCatalogBody{}
	add := func(all []searchCatalogBody) { out = append(out, rankCatalog(all, text, limit)...) }

	if a.allows(ctx, "product", "read") {
		items, err := gw.ListProducts(ctx)
		if err != nil {
			return nil, err
		}
		all := make([]searchCatalogBody, 0, len(items))
		for _, p := range items {
			d := ""
			if p.VendorName != nil {
				d = *p.VendorName
			}
			all = append(all, searchCatalogBody{Kind: "product", Name: p.Name, Label: p.Label, Detail: d})
		}
		add(all)
	}
	if a.allows(ctx, "driver", "read") {
		items, err := gw.ListDrivers(ctx)
		if err != nil {
			return nil, err
		}
		all := make([]searchCatalogBody, 0, len(items))
		for _, d := range items {
			all = append(all, searchCatalogBody{Kind: "driver", Name: d.Name, Label: d.Label, Detail: d.Version})
		}
		add(all)
	}
	if a.allows(ctx, "rule", "read") {
		calc, err := gw.ListCalcRules(ctx)
		if err != nil {
			return nil, err
		}
		all := make([]searchCatalogBody, 0, len(calc))
		for _, r := range calc {
			all = append(all, searchCatalogBody{Kind: "calc_rule", Name: r.Name, Label: r.Label, Detail: r.OutputKey})
		}
		add(all)
		ev, err := gw.ListEventRules(ctx, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, err
		}
		all = make([]searchCatalogBody, 0, len(ev))
		for _, r := range ev {
			all = append(all, searchCatalogBody{Kind: "event_rule", Name: r.Name, Label: r.Label, Detail: r.SampleKey})
		}
		add(all)
	}
	if a.allows(ctx, "metric_type", "read") {
		items, err := gw.ListMetricTypes(ctx)
		if err != nil {
			return nil, err
		}
		all := make([]searchCatalogBody, 0, len(items))
		for _, m := range items {
			d := ""
			if m.Unit != nil {
				d = *m.Unit
			}
			all = append(all, searchCatalogBody{Kind: "metric", Name: m.Name, Label: m.Label, Detail: d})
		}
		add(all)
	}
	return out, nil
}
