package bus

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// markStore records the from mark of each reconcile pass and answers with
// the next scripted result.
type markStore struct {
	fakeStore
	froms   []storage.RuleMark
	results []storage.RuleMark
	errs    []error
}

func (m *markStore) ReconcileEventRules(_ context.Context, from storage.RuleMark) (storage.RuleMark, error) {
	i := len(m.froms)
	m.froms = append(m.froms, from)
	return m.results[i], m.errs[i]
}

// A reconcile pass reads past the older of the last two marks (a batch in
// flight at one pass may commit ids below its mark), a failed correction
// (the store answers its own from) rereads that range, and an error keeps
// the marks.
func TestReconcileRulesLagsAPass(t *testing.T) {
	mk := func(p int64) storage.RuleMark { return storage.RuleMark{Property: p, Metric: p} }
	s := &markStore{
		results: []storage.RuleMark{mk(10), mk(20), mk(10), mk(40), {}, mk(50)},
		errs:    []error{nil, nil, nil, nil, errors.New("down"), nil},
	}
	var m ruleMarks
	for range s.results {
		_ = reconcileRules(context.Background(), s, &m)
	}
	want := []storage.RuleMark{mk(0), mk(0), mk(10), mk(10), mk(10), mk(10)}
	for i, w := range want {
		if s.froms[i] != w {
			t.Errorf("pass %d read past %+v, want %+v (all: %+v)", i, s.froms[i], w, s.froms)
		}
	}
}
