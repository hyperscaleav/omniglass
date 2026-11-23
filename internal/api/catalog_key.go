package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// catalogCollections maps a catalog route's collection segment to its table
// (#1434). A child route's segment (a product's metrics) names the catalog its
// key reads in.
var catalogCollections = map[string]string{
	"metric-types": "metric_type", "property-types": "property_type", "event-types": "event_type",
	"command-types": "command_type", "component-types": "component_type", "system-types": "system_type",
	"location-types": "location_type", "secret-types": "secret_type", "severity-levels": "severity_level",
	"notification-types": "notification_type", "event-rules": "event_rule", "action-rules": "action_rule",
	"calc-rules": "calc_rule", "vendors": "vendor", "products": "product", "drivers": "driver",
	"standards": "standard", "roles": "role",
	"metrics": "metric_type", "properties": "property_type",
}

// catalogKeyParam is one {namespace}/{key} pair a catalog route's path
// carries: the two path parameters and the catalog the key reads in.
type catalogKeyParam struct {
	namespace, key, table string
}

// catalogKeyParams reads the {namespace}/{key} pairs of a route's path
// (ADR-0318): a parameter named namespace, or ending _namespace for a child's,
// followed by the key's parameter, after the catalog's collection segment. A
// key segment's custom-method suffix (":rename") is not part of the name.
func catalogKeyParams(path string) []catalogKeyParam {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var out []catalogKeyParam
	for i := 1; i+1 < len(segs); i++ {
		ns, ok := pathParamName(segs[i])
		if !ok || (ns != "namespace" && !strings.HasSuffix(ns, "_namespace")) {
			continue
		}
		key, ok := pathParamName(strings.SplitN(segs[i+1], ":", 2)[0])
		table := catalogCollections[strings.SplitN(segs[i-1], ":", 2)[0]]
		if !ok || table == "" {
			continue
		}
		out = append(out, catalogKeyParam{namespace: ns, key: key, table: table})
	}
	return out
}

// pathParamName is the name of a "{name}" path segment.
func pathParamName(seg string) (string, bool) {
	if len(seg) < 3 || seg[0] != '{' || seg[len(seg)-1] != '}' {
		return "", false
	}
	return seg[1 : len(seg)-1], true
}

// catalogKeyed is the catalog address primitive (#1434, ADR-0318): every
// route addressing a catalog row as {namespace}/{key} resolves each pair once,
// here at the edge, into the ref that reaches exactly that row
// (storage.ResolveCatalogKey: its name where the search path reaches it, else
// its uuid), and the handler reads that ref as its key, so no handler and
// nothing in storage parses a joined string. A key no
// row of the namespace holds is a 404 before the handler runs. gated attaches
// it after authn and the permission check, so an unpermitted caller learns
// nothing of which keys exist, and documents each namespace parameter.
func (a *authenticator) catalogKeyed(op huma.Operation) huma.Operation {
	params := catalogKeyParams(op.Path)
	if len(params) == 0 {
		return op
	}
	for _, p := range params {
		op.Parameters = append(op.Parameters, &huma.Param{
			Name: p.namespace, In: "path", Required: true,
			Description: "The namespace the key is read in: og for the platform's shipped rows, local for the operator's (ADR-0318)",
			Schema:      &huma.Schema{Type: "string", Pattern: `^[a-z0-9][a-z0-9-]*$`},
		})
	}
	op.Middlewares = append(op.Middlewares, func(ctx huma.Context, next func(huma.Context)) {
		resolved := map[string]string{}
		for _, p := range params {
			ns, key := ctx.Param(p.namespace), ctx.Param(p.key)
			id, err := a.gw.ResolveCatalogKey(ctx.Context(), p.table, ns, key)
			if errors.Is(err, storage.ErrCatalogKeyNotFound) {
				// The row the route is about is a 404; a child key naming
				// what to bind it to is a 422, as an unknown one always was.
				status, word := http.StatusNotFound, " not found"
				if p.namespace != "namespace" {
					status, word = http.StatusUnprocessableEntity, " is not registered"
				}
				_ = huma.WriteErr(a.api, ctx, status, strings.ReplaceAll(p.table, "_", " ")+" "+ns+"/"+key+word)
				return
			}
			if err != nil {
				_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "resolve the catalog key")
				return
			}
			resolved[p.key] = id
		}
		next(keyedContext{humaContext: ctx, keys: resolved})
	})
	return op
}

// keyedContext is a request whose catalog key parameters read as the uuids
// catalogKeyed resolved them to.
type keyedContext struct {
	humaContext
	keys map[string]string
}

// humaContext names the embedded request so its field does not shadow the
// Context method.
type humaContext = huma.Context

func (c keyedContext) Param(name string) string {
	if id, ok := c.keys[name]; ok {
		return id
	}
	return c.humaContext.Param(name)
}

// catalogNamespaceOf is a catalog row's namespace, read from official: only
// the boot seed writes og and every og row is official (ADR-0318), and a
// create lands in local (CatalogCreateNamespace), so the two never differ.
func catalogNamespaceOf(official bool) string {
	if official {
		return storage.NamespaceOG
	}
	return storage.NamespaceLocal
}

// CatalogCreateNamespace is the namespace field every catalog create body
// carries (#1434): absent or local lands the row in local; og is refused,
// since only the boot seed writes og; any other is refused until a catalog
// create can land one (ADR-0318).
type CatalogCreateNamespace struct {
	Namespace string `json:"namespace,omitempty" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The namespace the new row lives in. Absent or local lands it in local, the operator's; og is refused (422), since only the boot seed writes the platform's rows"`
}

// Resolve refuses a create naming a namespace it cannot land in, before the
// handler runs.
func (n CatalogCreateNamespace) Resolve(huma.Context) []error {
	switch n.Namespace {
	case "", storage.NamespaceLocal:
		return nil
	case storage.NamespaceOG:
		return []error{&huma.ErrorDetail{Location: "body.namespace", Message: "og is written only by the boot seed; create the row in local", Value: n.Namespace}}
	default:
		return []error{&huma.ErrorDetail{Location: "body.namespace", Message: "a catalog create lands in local; another namespace is not yet creatable", Value: n.Namespace}}
	}
}
