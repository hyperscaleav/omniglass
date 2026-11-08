package bus_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/bus"
)

// max_payload is a setting the server states, not a default it inherits
// (#1579): left unset it is DefaultMaxPayload, and a Config's value reaches
// the running server.
func TestMaxPayloadIsSetExplicitly(t *testing.T) {
	f := newDeliveryFixture(t)
	for name, tc := range map[string]struct {
		cfg  int32
		want int64
	}{
		"unset is the named default": {0, bus.DefaultMaxPayload},
		"a configured limit":         {64 * 1024, 64 * 1024},
	} {
		t.Run(name, func(t *testing.T) {
			srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, MaxPayload: tc.cfg}, f.gw)
			if err != nil {
				t.Fatalf("start bus: %v", err)
			}
			defer srv.Shutdown()
			if got := srv.MaxPayload(); got != tc.want {
				t.Errorf("max payload = %d, want %d", got, tc.want)
			}
		})
	}
	if bus.DefaultMaxPayload != 1024*1024 {
		t.Errorf("DefaultMaxPayload = %d, want 1 MB", bus.DefaultMaxPayload)
	}
}
