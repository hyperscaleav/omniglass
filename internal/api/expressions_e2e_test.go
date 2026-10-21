package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type validateOut struct {
	OK          bool   `json:"ok"`
	Site        string `json:"site"`
	ValueType   string `json:"value_type"`
	ResultType  string `json:"result_type"`
	Diagnostics []struct {
		Line    int    `json:"line"`
		Column  int    `json:"column"`
		Phase   string `json:"phase"`
		Message string `json:"message"`
	} `json:"diagnostics"`
	Bindings []expression.Binding `json:"bindings"`
	Builtins []string             `json:"builtins"`
	Budget   struct {
		Iterations int    `json:"iterations"`
		Memory     int    `json:"memory"`
		Deadline   string `json:"deadline"`
	} `json:"budget"`
}

// TestValidateExpression drives POST /expressions:validate (#958): a source
// is compiled against a registered site's environment (a sample key selects
// the criteria value's type) and answered with positioned diagnostics and
// the site's bindings, builtins and budget, all read from its declaration.
// Gated by rule:read; an unknown site or key is a 422.
func TestValidateExpression(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	viewer := setupAllViewer(t, ctx, dsn, "expr-viewer")
	limited := setupLimitedPrincipal(t, ctx, dsn)

	validate := func(tok string, body map[string]any, status int) validateOut {
		t.Helper()
		var out validateOut
		raw := c.do(tok, http.MethodPost, "/expressions:validate", body, status)
		if status == http.StatusOK {
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}

	ok := validate(viewer, map[string]any{"site": "criteria", "source": "value > 80 && instance != \"aux\"", "key": "room-temperature"}, http.StatusOK)
	if !ok.OK || ok.ValueType != "number" || ok.ResultType != "bool" || len(ok.Diagnostics) != 0 {
		t.Fatalf("a good metric criterion: %+v", ok)
	}
	site, _ := rules.LookupSite("criteria")
	num, _ := site.Variant(rules.ValueNumber)
	if !reflect.DeepEqual(ok.Bindings, num.Env.Bindings()) || !reflect.DeepEqual(ok.Builtins, num.Env.Builtins()) ||
		ok.Budget.Iterations != num.Env.Budget().Iterations || ok.Budget.Deadline != num.Env.Budget().Deadline.String() {
		t.Fatalf("bindings %+v builtins %v budget %+v, want the site's own", ok.Bindings, ok.Builtins, ok.Budget)
	}

	bad := validate(viewer, map[string]any{"site": "criteria", "source": "value > 80 &&\n  humidity > 2", "key": "room-temperature"}, http.StatusOK)
	if bad.OK || len(bad.Diagnostics) != 1 || bad.Diagnostics[0].Line != 2 || bad.Diagnostics[0].Column != 3 || bad.Diagnostics[0].Phase != "type" {
		t.Fatalf("a bad criterion: %+v, want one type diagnostic at 2:3", bad)
	}

	// A property key selects its value type: text compared to a bool is wrong.
	if err := gw.UpsertPropertyType(ctx, storage.PropertyType{Name: "door-open", DataType: "bool"}); err != nil {
		t.Fatal(err)
	}
	mismatch := validate(viewer, map[string]any{"site": "criteria", "source": `value == "open"`, "key": "door-open"}, http.StatusOK)
	if mismatch.OK || mismatch.ValueType != "bool" || len(mismatch.Diagnostics) != 1 || mismatch.Diagnostics[0].Phase != "type" {
		t.Fatalf("text against a bool key: %+v, want a type diagnostic", mismatch)
	}
	if b := validate(viewer, map[string]any{"site": "criteria", "source": "value", "key": "door-open"}, http.StatusOK); !b.OK {
		t.Fatalf("a bool key's bare value: %+v", b)
	}
	// A rule always reads a key, so criteria need one.
	validate(viewer, map[string]any{"site": "criteria", "source": `value == "down"`}, http.StatusUnprocessableEntity)

	// A diagnostic speaks in bindings, not Go types.
	shape := validate(viewer, map[string]any{"site": "criteria", "source": "component > 1", "key": "room-temperature"}, http.StatusOK)
	if len(shape.Diagnostics) != 1 || strings.Contains(shape.Diagnostics[0].Message, "rules.") ||
		shape.Diagnostics[0].Message != "invalid operation: > (mismatched types component and int)" {
		t.Fatalf("diagnostic %+v, want it to name the binding component", shape.Diagnostics)
	}

	validate(viewer, map[string]any{"site": "nope", "source": "true", "key": "room-temperature"}, http.StatusUnprocessableEntity)
	validate(viewer, map[string]any{"site": "criteria", "source": "true", "key": "no-such-key"}, http.StatusUnprocessableEntity)
	validate(limited, map[string]any{"site": "criteria", "source": "true", "key": "room-temperature"}, http.StatusForbidden)
	validate(ownerTok, map[string]any{"site": "criteria", "source": "true", "key": "room-temperature"}, http.StatusOK)
}
