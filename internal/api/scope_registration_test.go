package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// permissionStamps are the operation extensions a gate publishes a permission
// under: gated(...)'s own, and the second gates platformGated and
// conditionalGated declare. Each names a <resource>:<action>[:tier] the route
// enforces, so each resource is one scope may be resolved for.
var permissionStamps = []string{
	"x-omniglass-permission",
	"x-omniglass-platform-permission",
	"x-omniglass-conditional-permission",
}

// scopeRegistrationGaps reads every operation's permission stamps from the
// OpenAPI document and reports each one whose resource scope cannot resolve:
// a resource scope.Registered says no for falls to applicableKinds' empty
// default, so every grant but an all-scoped one resolves to the empty set, and
// a route fencing on that set denies every scoped caller while an all-scoped
// owner keeps working (#754). The tokens come from the operation metadata, the
// same stamps the spec publishes, never from a hand-kept list, so a new route
// is checked the moment it registers.
func scopeRegistrationGaps(doc *huma.OpenAPI) []string {
	var gaps []string
	for path, item := range doc.Paths {
		for method, op := range map[string]*huma.Operation{
			http.MethodGet: item.Get, http.MethodPut: item.Put, http.MethodPost: item.Post,
			http.MethodDelete: item.Delete, http.MethodPatch: item.Patch, http.MethodHead: item.Head,
			http.MethodOptions: item.Options, http.MethodTrace: item.Trace,
		} {
			if op == nil {
				continue
			}
			for _, ext := range permissionStamps {
				perm, _ := op.Extensions[ext].(string)
				if perm == "" {
					continue
				}
				resource, _, _ := strings.Cut(perm, ":")
				if !scope.Registered(resource) {
					gaps = append(gaps, fmt.Sprintf("%s %s: %s %q names resource %q, which internal/scope's applicableKinds does not register", method, path, ext, perm, resource))
				}
			}
		}
	}
	sort.Strings(gaps)
	return gaps
}

// TestEveryGatedResourceRegistersItsScope is the gate on the invariant
// identity-access names: every permission a route enforces has a scope tier
// (or is declared fleet-wide) in internal/scope. Forgetting the registration
// does not fail anything at runtime; it silently denies every scoped caller,
// which is why the check lives here, at make test, over the live route set.
func TestEveryGatedResourceRegistersItsScope(t *testing.T) {
	t.Parallel()
	doc := specAPI(storage.UnimplementedGateway{}).OpenAPI()
	if len(doc.Paths) == 0 {
		t.Fatal("the spec has no operations; the scan would pass vacuously")
	}
	for _, gap := range scopeRegistrationGaps(doc) {
		t.Errorf("%s: register its tier in applicableKinds (or declare it fleet-wide there), with a test", gap)
	}
}

// TestScopeRegistrationGapsNamesTheRouteAndPermission proves the scan above can
// fail: a route gated on a resource no tier registers (widget, standing for a
// future system- or location-level command route that forgot its tier) is
// reported by its route and its permission, while the real routes beside it
// are not.
func TestScopeRegistrationGapsNamesTheRouteAndPermission(t *testing.T) {
	t.Parallel()
	api := specAPI(storage.UnimplementedGateway{})
	a := &authenticator{api: api, perms: map[string]struct{}{}}
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-widget",
		Method:      http.MethodGet,
		Path:        "/widgets/{name}",
	}, "widget", "read"), func(context.Context, *struct {
		Name string `path:"name"`
	}) (*struct{}, error) {
		return nil, nil
	})

	gaps := scopeRegistrationGaps(api.OpenAPI())
	if len(gaps) != 1 {
		t.Fatalf("gaps = %q, want exactly the throwaway widget route", gaps)
	}
	for _, want := range []string{"GET /widgets/{name}", `"widget:read"`} {
		if !strings.Contains(gaps[0], want) {
			t.Errorf("gap %q does not name %s", gaps[0], want)
		}
	}
}
