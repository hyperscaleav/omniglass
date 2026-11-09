package storage

import (
	"reflect"
	"testing"
	"time"
)

// TestMergeUnbound folds a tick's groups hearing one unbound identity into
// the one row an upsert can touch (#1582), and leaves distinct identities as
// they came, in their order.
func TestMergeUnbound(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	cases := []struct {
		name string
		in   []UnboundIdentityWrite
		want []UnboundIdentityWrite
	}{
		{"nothing", nil, []UnboundIdentityWrite{}},
		{"distinct identities stay apart, in order",
			[]UnboundIdentityWrite{
				{ConduitID: "c1", Key: "serial", Value: "b", Count: 1, FirstAt: at(1), LastAt: at(1), Keys: []string{"k1"}},
				{ConduitID: "c1", Key: "serial", Value: "a", Count: 2, FirstAt: at(2), LastAt: at(3)},
				{ConduitID: "c2", Key: "serial", Value: "b", Count: 1, FirstAt: at(1), LastAt: at(1)},
				{ConduitID: "c1", Key: "mac", Value: "b", Count: 1, FirstAt: at(1), LastAt: at(1)},
			},
			[]UnboundIdentityWrite{
				{ConduitID: "c1", Key: "serial", Value: "b", Count: 1, FirstAt: at(1), LastAt: at(1), Keys: []string{"k1"}},
				{ConduitID: "c1", Key: "serial", Value: "a", Count: 2, FirstAt: at(2), LastAt: at(3)},
				{ConduitID: "c2", Key: "serial", Value: "b", Count: 1, FirstAt: at(1), LastAt: at(1)},
				{ConduitID: "c1", Key: "mac", Value: "b", Count: 1, FirstAt: at(1), LastAt: at(1)},
			}},
		{"one identity folds: counts summed, earliest first, latest last, keys unioned",
			[]UnboundIdentityWrite{
				{ConduitID: "c1", Key: "serial", Value: "a", Count: 2, FirstAt: at(5), LastAt: at(6), Keys: []string{"k1", "k2"}},
				{ConduitID: "c2", Key: "serial", Value: "a", Count: 1, FirstAt: at(0), LastAt: at(0)},
				{ConduitID: "c1", Key: "serial", Value: "a", Count: 3, FirstAt: at(1), LastAt: at(9), Keys: []string{"k2", "k3"}},
				{ConduitID: "c1", Key: "serial", Value: "a", Count: 1, FirstAt: at(4), LastAt: at(4)},
			},
			[]UnboundIdentityWrite{
				{ConduitID: "c1", Key: "serial", Value: "a", Count: 6, FirstAt: at(1), LastAt: at(9), Keys: []string{"k1", "k2", "k3"}},
				{ConduitID: "c2", Key: "serial", Value: "a", Count: 1, FirstAt: at(0), LastAt: at(0)},
			}},
		{"a zero first heard stands for none",
			[]UnboundIdentityWrite{
				{ConduitID: "c1", Key: "serial", Value: "a", Count: 1, LastAt: at(2)},
				{ConduitID: "c1", Key: "serial", Value: "a", Count: 1, FirstAt: at(3), LastAt: at(3)},
			},
			[]UnboundIdentityWrite{
				{ConduitID: "c1", Key: "serial", Value: "a", Count: 2, FirstAt: at(3), LastAt: at(3)},
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mergeUnbound(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mergeUnbound =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

// TestMergeUnboundLeavesItsInput holds the merge to its own rows: a caller
// may share the batch's rows, and the fold must not grow their keys.
func TestMergeUnboundLeavesItsInput(t *testing.T) {
	t.Parallel()
	in := []UnboundIdentityWrite{
		{ConduitID: "c1", Key: "serial", Value: "a", Count: 1, Keys: []string{"k1"}},
		{ConduitID: "c1", Key: "serial", Value: "a", Count: 1, Keys: []string{"k2"}},
	}
	mergeUnbound(in)
	if !reflect.DeepEqual(in[0].Keys, []string{"k1"}) || in[0].Count != 1 {
		t.Fatalf("input changed: %+v", in[0])
	}
}
