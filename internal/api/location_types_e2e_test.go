package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationTypesAPI drives the location_type registry read endpoint: an owner
// lists the seeded official types in rank order, each with its display_name, so a
// form can populate a type picker (value = id, label = display_name). The 403 for
// a principal without location:read is covered generically by TestEveryRouteIsGated.
func TestLocationTypesAPI(t *testing.T) {
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

	out := c.do(ownerTok, http.MethodGet, "/location-types", nil, http.StatusOK)
	var body struct {
		LocationTypes []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Rank        int    `json:"rank"`
			Official    bool   `json:"official"`
		} `json:"location_types"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The four seeded official types, in rank order, each labelled and official.
	want := []string{"campus", "building", "floor", "room"}
	gotIDs := make([]string, len(body.LocationTypes))
	for i, lt := range body.LocationTypes {
		gotIDs[i] = lt.ID
	}
	if len(gotIDs) != len(want) {
		t.Fatalf("location_types = %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("location_types order = %v, want %v", gotIDs, want)
		}
	}
	for _, lt := range body.LocationTypes {
		if lt.DisplayName == "" || !lt.Official {
			t.Errorf("type %q: display_name=%q official=%v, want non-empty label + official", lt.ID, lt.DisplayName, lt.Official)
		}
	}
}
