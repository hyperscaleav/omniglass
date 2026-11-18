package storage

import "testing"

// holdAcross is the value in force at the end of each bucket (#1488): a
// bucket's own last, else the one before it holds, else the carried-in value
// before the grid.
func TestHoldAcross(t *testing.T) {
	t.Parallel()
	s := func(v string) *string { return &v }
	val := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	cases := []struct {
		name      string
		carriedIn *string
		lasts     []*string
		want      []string
	}{
		{"carried in through leading empties", s("on"), []*string{nil, nil, s("off"), nil}, []string{"on", "on", "off", "off"}},
		{"nothing before the first change", nil, []*string{nil, s("a"), nil}, []string{"<nil>", "a", "a"}},
		{"a bucket's own last wins over the carried-in", s("on"), []*string{s("off")}, []string{"off"}},
		{"no buckets", s("on"), nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			buckets := make([]PropertyBucket, len(c.lasts))
			for i, l := range c.lasts {
				buckets[i].Last = l
			}
			holdAcross(buckets, c.carriedIn)
			for i, b := range buckets {
				if val(b.Held) != c.want[i] {
					t.Fatalf("bucket %d held = %s, want %s", i, val(b.Held), c.want[i])
				}
			}
		})
	}
}
