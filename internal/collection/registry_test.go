package collection_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func TestRegistryAllows(t *testing.T) {
	metric := "metric"
	reg := collection.NewRegistry([]storage.Property{
		{Name: "tcp.open", Kind: &metric},
		{Name: "icmp.reachable", Kind: &metric},
	})

	if kind, ok := reg.Allows("tcp.open"); !ok || kind != "metric" {
		t.Errorf("tcp.open: want (metric,true), got (%q,%v)", kind, ok)
	}
	if _, ok := reg.Allows("bogus.key"); ok {
		t.Errorf("bogus.key: want reject, got allow")
	}
}
