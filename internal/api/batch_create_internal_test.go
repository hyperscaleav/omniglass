package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A dry run answers 200 and a write 201, and the spec declares both on every
// batch create.
func TestBatchCreatesDeclareBothStatuses(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Responses map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/locations:batchCreate", "/systems:batchCreate", "/components:batchCreate"} {
		r := spec.Paths[path]["post"].Responses
		for _, code := range []string{"200", "201"} {
			if _, ok := r[code]; !ok {
				t.Errorf("%s declares no %s response", path, code)
			}
		}
	}
}

// A refused row is mapped as its single create maps it, with its place in the
// request leading the detail and as the error's location.
func TestBatchErrNamesTheRow(t *testing.T) {
	t.Parallel()
	err := batchErr(&storage.BatchError{Index: 3, Err: storage.ErrUnknownType}, mapLocationErr)
	var em *huma.ErrorModel
	if !errors.As(err, &em) || em.Status != http.StatusUnprocessableEntity {
		t.Fatalf("mapped = %v, want the single create's 422", err)
	}
	if em.Detail != "requests[3]: unknown location_type" {
		t.Fatalf("detail = %q", em.Detail)
	}
	if len(em.Errors) == 0 || em.Errors[len(em.Errors)-1].Location != "body.requests[3]" {
		t.Fatalf("errors = %+v, want one at body.requests[3]", em.Errors)
	}
}

// The row limit is a 413 naming it; a write is stamped with a batch id and a
// dry run is not.
func TestBatchStart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	if _, _, _, err := batchStart(ctx, batchRowLimit+1, false); err == nil {
		t.Fatal("a batch over the limit was accepted")
	} else {
		var se huma.StatusError
		if !errors.As(err, &se) || se.GetStatus() != http.StatusRequestEntityTooLarge {
			t.Fatalf("over the limit = %v, want a 413", err)
		}
	}
	if _, id, status, err := batchStart(ctx, batchRowLimit, true); err != nil || id != "" || status != http.StatusOK {
		t.Fatalf("dry run = %q %d %v, want no id and a 200", id, status, err)
	}
	if _, id, status, err := batchStart(ctx, 1, false); err != nil || id == "" || status != http.StatusCreated {
		t.Fatalf("write = %q %d %v, want an id and a 201", id, status, err)
	}
}

// A dry run blanks the ids the batch minted and keeps a reference to a row
// that was already there.
func TestDryIDs(t *testing.T) {
	t.Parallel()
	minted := map[string]bool{"a": true}
	id, inBatch, existing := "b", "a", "z"
	p := &inBatch
	dryIDs(minted, &id, &p)
	if id != "" || p != nil {
		t.Fatalf("in-batch parent: id %q parent %v, want both blank", id, p)
	}
	id, p = "c", &existing
	dryIDs(minted, &id, &p)
	if id != "" || p == nil || *p != "z" {
		t.Fatalf("existing parent: id %q parent %v, want the parent kept", id, p)
	}
}
