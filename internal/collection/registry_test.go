package collection_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func TestRegistryAllows(t *testing.T) {
	reg := collection.NewRegistry([]storage.DatapointType{
		{Scope: "official", Name: "tcp.open", Kind: "metric"},
		{Scope: "official", Name: "icmp.reachable", Kind: "metric"},
	})

	if kind, ok := reg.Allows("tcp.open"); !ok || kind != "metric" {
		t.Errorf("tcp.open: want (metric,true), got (%q,%v)", kind, ok)
	}
	if _, ok := reg.Allows("bogus.key"); ok {
		t.Errorf("bogus.key: want reject, got allow")
	}
}
