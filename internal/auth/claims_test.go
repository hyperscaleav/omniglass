package auth_test

import (
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
)

// A groups claim's two shapes, and every other value read as absent (#2363):
// absent is the empty set to the sync, so a malformed claim fails closed.
func TestStringsClaimReadsAGroupsClaimInEitherShape(t *testing.T) {
	cases := []struct {
		name   string
		raw    any
		want   []string
		wantOK bool
	}{
		{"array", []any{"av-support", "it"}, []string{"av-support", "it"}, true},
		{"array skips non-strings and empties", []any{"a", 7.0, "", nil, "b"}, []string{"a", "b"}, true},
		{"empty array", []any{}, []string{}, true},
		{"one string", "av-support", []string{"av-support"}, true},
		{"empty string", "", []string{}, true},
		{"absent", nil, nil, false},
		{"a number", 3.0, nil, false},
		{"an object", map[string]any{"a": "b"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &auth.JWTClaims{Raw: map[string]any{}}
			if tc.raw != nil {
				c.Raw["groups"] = tc.raw
			}
			got, ok := c.StringsClaim("groups")
			if ok != tc.wantOK || !slices.Equal(got, tc.want) {
				t.Fatalf("StringsClaim = %v, %v; want %v, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
