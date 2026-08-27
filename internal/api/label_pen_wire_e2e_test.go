package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The LABEL's pen on the wire (#683).
//
// Slice 2 (#682) stored label_generated and stamped it on every write,
// but never put it on a read body, so nothing outside the database could tell a
// label the platform rendered from one an operator typed. The console's
// hasLabel is the reader that needs it: "the label differs from the name"
// was the same question as "a human chose this" only while every label was
// operator-typed, and it stopped being the same question the moment a rule could
// render one.
//
// It is READ-ONLY on the wire, and deliberately: an operator claims the pen by
// writing label and returns it by clearing label, never by
// asserting the boolean. Two ways to say one thing is two ways for them to
// disagree, which is the same reason name_generated is read-only beside
// :rename.
//
// pen is the read shape shared by all three entity kinds: the label, and who
// owns it.
type pen struct {
	Name           string `json:"name"`
	Label          string `json:"label"`
	LabelGenerated bool   `json:"label_generated"`
}

func penHarness(t *testing.T) (*apiClient, string, storage.Product, func()) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		gw.Close()
		t.Fatalf("seed: %v", err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		gw.Close()
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		gw.Close()
		t.Fatalf("bootstrap: %v", err)
	}
	p := storagetest.MintProduct(t, ctx, gw, "")
	srv := httptest.NewServer(api.NewHandler(gw))
	return &apiClient{t: t, ctx: ctx, base: srv.URL}, ownerTok, p, func() {
		srv.Close()
		gw.Close()
	}
}

func decodePen(t *testing.T, what string, raw []byte) pen {
	t.Helper()
	var p pen
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decode %s: %v", what, err)
	}
	return p
}

// The field exists on the read body of every labelled entity, with the polarity
// name_generated already established: TRUE means the platform owns it.
func TestLabelPenOnTheWire(t *testing.T) {
	c, tok, p, stop := penHarness(t)
	defer stop()

	cases := []struct {
		kind   string
		path   string
		blank  map[string]any // a create that hands the platform the pen
		typed  map[string]any // a create where the operator types a label
		addr   string         // the typed row's address, for the GET and PATCH
		blankA string
	}{
		{
			kind:   "component",
			path:   "/components",
			blank:  map[string]any{"name": "pen-blank", "product": p.Name},
			typed:  map[string]any{"name": "pen-typed", "label": "Operator Typed", "product": p.Name},
			addr:   "/components/pen-typed",
			blankA: "/components/pen-blank",
		},
		{
			kind:   "system",
			path:   "/systems",
			blank:  map[string]any{"name": "pen-blank-sys"},
			typed:  map[string]any{"name": "pen-typed-sys", "label": "Operator Typed"},
			addr:   "/systems/pen-typed-sys",
			blankA: "/systems/pen-blank-sys",
		},
		{
			kind:   "location",
			path:   "/locations",
			blank:  map[string]any{"name": "pen-blank-loc", "location_type": "campus"},
			typed:  map[string]any{"name": "pen-typed-loc", "location_type": "campus", "label": "Operator Typed"},
			addr:   "/locations/pen-typed-loc",
			blankA: "/locations/pen-blank-loc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			// Created with no label: the platform holds the pen, and says so.
			blank := decodePen(t, tc.kind+" create blank", c.do(tok, http.MethodPost, tc.path, tc.blank, http.StatusCreated))
			if !blank.LabelGenerated {
				t.Fatalf("%s created with no label = %+v, want label_generated=true", tc.kind, blank)
			}

			// Created with a label an operator typed: the operator holds it.
			typed := decodePen(t, tc.kind+" create typed", c.do(tok, http.MethodPost, tc.path, tc.typed, http.StatusCreated))
			if typed.LabelGenerated {
				t.Fatalf("%s created with a typed label = %+v, want label_generated=false", tc.kind, typed)
			}

			// The read side reports the same, so it survives a round trip
			// through the API rather than only riding the create response.
			got := decodePen(t, tc.kind+" get", c.do(tok, http.MethodGet, tc.addr, nil, http.StatusOK))
			if got.LabelGenerated {
				t.Fatalf("GET %s = %+v, want label_generated=false on the read side too", tc.addr, got)
			}
			back := decodePen(t, tc.kind+" get blank", c.do(tok, http.MethodGet, tc.blankA, nil, http.StatusOK))
			if !back.LabelGenerated {
				t.Fatalf("GET %s = %+v, want label_generated=true on the read side too", tc.blankA, back)
			}

			// Clearing the field hands the pen back, which is the whole reason
			// the console must read the pen rather than compare two strings.
			cleared := decodePen(t, tc.kind+" clear", c.do(tok, http.MethodPatch, tc.addr, map[string]any{"label": ""}, http.StatusOK))
			if !cleared.LabelGenerated {
				t.Fatalf("%s after clearing the label = %+v, want label_generated=true", tc.kind, cleared)
			}
		})
	}
}

// The pen is read-only: it is not a field a write body accepts, so an operator
// cannot claim or surrender it except by writing the label itself. Huma refuses
// an unknown body field, so the assertion is that the write is refused at all.
func TestLabelPenIsNotWritable(t *testing.T) {
	c, tok, p, stop := penHarness(t)
	defer stop()

	c.do(tok, http.MethodPost, "/components", map[string]any{"name": "pen-ro", "product": p.Name}, http.StatusCreated)
	c.do(tok, http.MethodPatch, "/components/pen-ro",
		map[string]any{"label_generated": false}, http.StatusUnprocessableEntity)
}
