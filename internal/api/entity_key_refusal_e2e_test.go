package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// An illegal key is an operator typo, so it owes an operator a 422 that says
// what is wrong. It must never be a 5xx.
//
// This is the tier that matters and the tier that was missing. Asserting the
// storage sentinel proves the gateway refuses; it says nothing about what the
// route does with that refusal. When the key rule was first widened, five
// registry routes threw a brand new sentinel into mapTypeErr, which had no case
// for it, so every one of them fell to the default branch and returned
// 500 "type operation failed" for a name with a capital letter in it. The whole
// suite stayed green, because nothing drove those routes with a bad name.
//
// So this drives them. One request per route per illegal input, over real HTTP
// against real Postgres, asserting the status an operator actually receives.
func TestIllegalKeyIsAlways422(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Each route, with the minimum body its handler needs beyond the name.
	routes := map[string]func(seg string) map[string]any{
		"/vendors": func(s string) map[string]any {
			return map[string]any{"name": s, "display_name": "X", "kind": "manufacturer"}
		},
		"/drivers": func(s string) map[string]any { return map[string]any{"name": s, "display_name": "X"} },
		"/component-types": func(s string) map[string]any {
			return map[string]any{"name": s, "display_name": "X"}
		},
		"/standards":      func(s string) map[string]any { return map[string]any{"name": s, "display_name": "X"} },
		"/location-types": func(s string) map[string]any { return map[string]any{"name": s, "display_name": "X"} },
		"/products":       func(s string) map[string]any { return map[string]any{"name": s, "display_name": "X"} },
		"/nodes":          func(s string) map[string]any { return map[string]any{"name": s} },
		"/components":     func(s string) map[string]any { return map[string]any{"name": s} },
		"/systems":        func(s string) map[string]any { return map[string]any{"name": s} },
		"/locations":      func(s string) map[string]any { return map[string]any{"name": s, "location_type": "room"} },
	}

	// Each of these is refused for a different reason, and each is a plausible
	// thing an operator types or pastes.
	bad := map[string]string{
		"a capital letter":  "BadSeg",
		"a space":           "bad seg",
		"the sigil":         "bad$seg",
		"a dot":             "bad.seg",
		"an underscore":     "bad_seg",
		"a pasted uuid":     "019f8754-461f-7b82-b5f2-fc4bbe1c3765",
		"a leading hyphen":  "-badseg",
		"a NATS wildcard":   "bad*seg",
		"a NATS tail token": "bad>seg",
	}

	for path, body := range routes {
		for why, seg := range bad {
			t.Run(path+"/"+why, func(t *testing.T) {
				status, raw := c.send(ownerTok, http.MethodPost, path, body(seg))
				if status == http.StatusUnprocessableEntity {
					return
				}
				if status >= 500 {
					t.Fatalf("POST %s with %s (%q) returned %d, an operator typo must never be a 5xx.\nbody: %s",
						path, why, seg, status, raw)
				}
				t.Fatalf("POST %s with %s (%q) returned %d, want 422.\nbody: %s", path, why, seg, status, raw)
			})
		}
	}
}
