package profile

import "testing"

// TestValidateComputed pins the per-profile computed-block guards: each
// foot-gun a profile author might leave behind should fail at load time.
func TestValidateComputed(t *testing.T) {
	cases := []struct {
		name    string
		p       DeviceProfile
		wantErr bool
	}{
		{
			name: "valid elapsed",
			p: DeviceProfile{Computed: []Computed{
				{Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch"},
			}},
		},
		{
			name:    "missing path",
			p:       DeviceProfile{Computed: []Computed{{Kind: "elapsed", Since: "status.device.boot_epoch"}}},
			wantErr: true,
		},
		{
			name:    "missing kind",
			p:       DeviceProfile{Computed: []Computed{{Path: "status.device.uptime", Since: "status.device.boot_epoch"}}},
			wantErr: true,
		},
		{
			name:    "unsupported kind",
			p:       DeviceProfile{Computed: []Computed{{Path: "status.device.uptime", Kind: "counter", Since: "status.device.boot_epoch"}}},
			wantErr: true,
		},
		{
			name:    "elapsed without since",
			p:       DeviceProfile{Computed: []Computed{{Path: "status.device.uptime", Kind: "elapsed"}}},
			wantErr: true,
		},
		{
			name: "duplicate path",
			p: DeviceProfile{Computed: []Computed{
				{Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch"},
				{Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.other_epoch"},
			}},
			wantErr: true,
		},
		{
			name: "path also written by a behavior",
			p: DeviceProfile{
				Behaviors: []Behavior{{
					Name:      "uptime_tick",
					Mutations: []Mutation{{Path: "status.device.uptime"}},
				}},
				Computed: []Computed{{Path: "status.device.uptime", Kind: "elapsed", Since: "status.device.boot_epoch"}},
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateComputed(&tc.p)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestNoUptimeTickBehavior is the flood regression at the profile layer:
// no shipped device profile may drive status.device.uptime from a
// scheduled behavior (that was the per-10s event flood). Every profile
// that exposes uptime must do so via a computed field instead.
func TestNoUptimeTickBehavior(t *testing.T) {
	store, err := LoadStore(findProfilesRoot(t))
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	for name, dev := range store.Devices {
		for _, b := range dev.Behaviors {
			for _, m := range b.Mutations {
				if m.Path == "status.device.uptime" {
					t.Errorf("%s: behavior %q writes status.device.uptime on a timer, uptime must be a computed field, not a tick", name, b.Name)
				}
			}
		}
		hasUptimeComputed := false
		for _, c := range dev.Computed {
			if c.Path == "status.device.uptime" {
				hasUptimeComputed = true
			}
		}
		if !hasUptimeComputed {
			t.Errorf("%s: no computed uptime field declared", name)
		}
	}
}
