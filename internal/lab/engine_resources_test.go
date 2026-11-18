package lab

import "testing"

// #1892: a service's caps reach Docker as its HostConfig resources, with swap
// held to the memory cap so the limit is the container's whole memory; a
// service with no cap gets none.
func TestResourcesCapTheContainer(t *testing.T) {
	r := resources(Service{CPUs: 1.5, MemoryBytes: 512 << 20})
	if r.NanoCPUs != 1_500_000_000 || r.Memory != 512<<20 || r.MemorySwap != 512<<20 {
		t.Errorf("resources = nano %d memory %d swap %d, want 1.5e9, 512 MiB and 512 MiB",
			r.NanoCPUs, r.Memory, r.MemorySwap)
	}
	if r := resources(Service{}); r.NanoCPUs != 0 || r.Memory != 0 || r.MemorySwap != 0 {
		t.Errorf("an uncapped service's resources = %+v, want none", r)
	}
}
