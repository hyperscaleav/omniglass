package collection_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A changed host key is refused with both keys as data (#1078): the node
// publishes them beside the gate, so the console names the pinned and the
// offered key and trusts the exact one it showed, never one parsed out of the
// reason. The reason keeps naming both, for the property's own series and the
// CLI reader.
func TestAHostKeyRefusalCarriesBothKeys(t *testing.T) {
	pinned, offered := "SHA256:"+strings.Repeat("A", 43), "SHA256:"+strings.Repeat("B", 43)
	terr := collection.HostKeyRefusal(pinned, offered)
	if terr.Class != driver.ClassHostKey || terr.Pinned != pinned || terr.Offered != offered {
		t.Fatalf("HostKeyRefusal = %+v, want class host-key with both keys", terr)
	}
	if !strings.Contains(terr.Reason, pinned) || !strings.Contains(terr.Reason, offered) {
		t.Fatalf("the reason no longer names both keys: %q", terr.Reason)
	}
}
