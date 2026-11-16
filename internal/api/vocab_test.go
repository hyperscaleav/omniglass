package api

import (
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/vocab"
)

// The API speaks the product vocabulary (#2029): every top-level collection is
// a term or a listed not_term, each term's entity schema carries its
// definition, and each term's permission noun is one a route enforces.
func TestAPISpeaksTheVocabulary(t *testing.T) {
	doc := specAPI(storage.UnimplementedGateway{}).OpenAPI()
	v := vocab.MustLoad()

	collections := map[string]bool{}
	nouns := map[string]bool{}
	for path, item := range doc.Paths {
		seg := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)[0]
		seg = strings.SplitN(seg, ":", 2)[0]
		collections[seg] = true
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op == nil {
				continue
			}
			for _, key := range permissionStamps {
				if p, ok := op.Extensions[key].(string); ok {
					nouns[strings.SplitN(p, ":", 2)[0]] = true
				}
			}
		}
	}

	known := v.Collections()
	for c := range collections {
		if _, ok := known[c]; !ok {
			t.Errorf("collection /%s maps to no term: add it to internal/vocab/terms.yaml, or to not_terms with why", c)
		}
	}
	for c := range known {
		if !collections[c] {
			t.Errorf("terms.yaml names collection /%s, which the API no longer serves", c)
		}
	}

	schemas := doc.Components.Schemas.Map()
	for name, want := range v.SchemaDescriptions() {
		s, ok := schemas[name]
		if !ok {
			t.Errorf("terms.yaml names schema %s, which the spec does not have", name)
			continue
		}
		if s.Description != want {
			t.Errorf("schema %s description = %q, want its term's definition %q", name, s.Description, want)
		}
	}

	for _, term := range v.Terms {
		if term.Permission != "" && !nouns[term.Permission] {
			t.Errorf("term %q names permission noun %q, which no route enforces", term.Word, term.Permission)
		}
	}
}
