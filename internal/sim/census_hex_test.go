package sim

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestByteOfHelper pins the byte_of template helper that packs an integer
// people_count into a single raw response byte. Covers the int/int64/
// float64/string inputs the state tree can hold after JSON coercion, plus
// the low-byte masking and the non-numeric fallback.
func TestByteOfHelper(t *testing.T) {
	byteOf := templateFuncs["byte_of"].(func(any) string)
	cases := []struct {
		name string
		in   any
		want byte
	}{
		{"int", 7, 7},
		{"int64", int64(14), 14},
		{"float64", float64(3), 3},
		{"string", "5", 5},
		{"zero", 0, 0},
		{"masks_low_byte", 257, 1},
		{"non_numeric", "x", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := byteOf(tc.in)
			if len(got) != 1 {
				t.Fatalf("byte_of(%v) = %q, want a single byte", tc.in, got)
			}
			if got[0] != tc.want {
				t.Errorf("byte_of(%v) = 0x%02X, want 0x%02X", tc.in, got[0], tc.want)
			}
		})
	}
}

// TestCensusHexPollCommand drives the control-hex command exactly as
// authored in the tessellate-census-c1 profile: a binary "AA 01" poll
// matched via MatchHex, answered with the inline response template
// "AA 81 <count>". It asserts the 3-byte response frame decodes back to
// the live people_count for several counts, including the ETX/CR-valued
// edge cases (3 and 13) the fixed-width client read is designed to
// survive.
func TestCensusHexPollCommand(t *testing.T) {
	for _, count := range []int{0, 1, 3, 7, 13, 14} {
		state := map[string]any{
			"status": map[string]any{
				"occupancy": map[string]any{"people_count": count},
			},
		}
		d := newTestDevice(t, "census-hex", state)

		cmd := &profile.Command{
			MatchHex: "AA 01",
			Response: `{{ hex_byte "AA" }}{{ hex_byte "81" }}{{ byte_of .State.status.occupancy.people_count }}`,
		}
		if err := d.AddCommandFromProfile(cmd, cmd.Response); err != nil {
			t.Fatalf("AddCommandFromProfile: %v", err)
		}

		// The dispatch key is the decoded binary payload AA 01.
		result, ok, err := d.RunCommand("\xAA\x01")
		if err != nil {
			t.Fatalf("count=%d RunCommand error: %v", count, err)
		}
		if !ok {
			t.Fatalf("count=%d: hex command did not match", count)
		}

		resp := []byte(result.Response)
		if len(resp) != 3 {
			t.Fatalf("count=%d: response = % X, want 3 bytes", count, resp)
		}
		if resp[0] != 0xAA {
			t.Errorf("count=%d: magic = 0x%02X, want 0xAA", count, resp[0])
		}
		if resp[1] != 0x81 {
			t.Errorf("count=%d: opcode = 0x%02X, want 0x81", count, resp[1])
		}
		if int(resp[2]) != count {
			t.Errorf("count=%d: count byte = %d, want %d", count, resp[2], count)
		}
	}
}
