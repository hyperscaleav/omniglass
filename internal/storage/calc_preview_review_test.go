package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A preview's selector resolves in the caller's scope (#998 review 1): a
// name that does not exist and a name the caller may not read are refused
// alike, and a name shared in and out of scope resolves to the one in scope,
// naming no other.
func TestCalcPreviewSelectorInScope(t *testing.T) {
	r := newEvalRig(t)
	for _, loc := range []string{"east", "west"} {
		if _, err := r.gw.CreateLocation(r.ctx, "", storage.LocationSpec{Name: loc, LocationType: "campus"}, r.all); err != nil {
			t.Fatal(err)
		}
	}
	east, west := "east", "west"
	in, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "dup", LocationName: &east}, r.all, r.all, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "dup", LocationName: &west}, r.all, r.all, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "hidden-one", LocationName: &west}, r.all, r.all, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	viewer := scope.Set{SelfIDs: []string{in.ID}}
	spec := func(ref string) storage.CalcRuleSpec {
		return storage.CalcRuleSpec{OwnerKind: rules.OwnerComponent, Selector: storage.CalcRuleSelector{Components: []string{ref}},
			Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "avg(t)"}, OutputKey: "avg-temp"}
	}
	_, errMissing := r.gw.PreviewCalcRule(r.ctx, spec("no-such-one"), viewer, scope.Set{})
	_, errHidden := r.gw.PreviewCalcRule(r.ctx, spec("hidden-one"), viewer, scope.Set{})
	if errMissing == nil || errHidden == nil || !errors.Is(errHidden, storage.ErrCalcRuleInvalid) {
		t.Fatalf("missing %v, hidden %v: want both refused", errMissing, errHidden)
	}
	mask := func(err error, name string) string { return strings.ReplaceAll(err.Error(), name, "NAME") }
	if mask(errMissing, "no-such-one") != mask(errHidden, "hidden-one") {
		t.Fatalf("a missing name and an out-of-scope name differ:\n%s\n%s", errMissing, errHidden)
	}
	if strings.Contains(errHidden.Error(), hidden.ID) {
		t.Fatal("the refusal names the out-of-scope component's id")
	}
	pv, err := r.gw.PreviewCalcRule(r.ctx, spec("dup"), viewer, scope.Set{})
	if err != nil {
		t.Fatalf("a name shared in and out of scope: %v, want the in-scope one", err)
	}
	if pv.OwnersTotal > 1 {
		t.Fatalf("preview %+v reaches past the caller's scope", pv)
	}
	if strings.Contains(errHidden.Error(), out.ID) {
		t.Fatal("a refusal names an out-of-scope candidate")
	}
}

// A preview holds no lock a save waits on (#998 review 2): a save proceeds
// while a preview is held mid-computation.
func TestCalcPreviewBlocksNoSave(t *testing.T) {
	r := newEvalRig(t)
	r.metric("room-temperature", "", 20)
	held, release := make(chan struct{}), make(chan struct{})
	r.gw.SetCalcServeHook(func(stage string) error {
		if stage == "preview" {
			select {
			case <-held:
			default:
				close(held)
			}
			<-release
		}
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := r.gw.PreviewCalcRule(context.Background(), avgSpec(""), r.all, r.all)
		done <- err
	}()
	<-held
	saved := make(chan error, 1)
	go func() { _, err := r.create(avgSpec("temp-mean")); saved <- err }()
	select {
	case err := <-saved:
		if err != nil {
			t.Fatalf("the save: %v", err)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the save waited on a held preview")
	}
	close(release)
	r.gw.SetCalcServeHook(nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A component preview counts and computes the owners the tick would: those
// holding one of its inputs (#998 review 4), and bounds its total reads
// (review 3), saying so as truncation.
func TestCalcPreviewOwnersAndWork(t *testing.T) {
	r := newEvalRig(t)
	for _, name := range []string{"bare-1", "bare-2"} {
		if _, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: name}, r.all, r.all, r.all, r.all); err != nil {
			t.Fatal(err)
		}
	}
	var holders []string
	for _, name := range []string{"holds-1", "holds-2", "holds-3"} {
		c, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: name}, r.all, r.all, r.all, r.all)
		if err != nil {
			t.Fatal(err)
		}
		holders = append(holders, c.ID)
		if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: c.ID, Key: "room-temperature", Value: 1, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	pv, err := r.gw.PreviewCalcRule(r.ctx, avgSpec(""), r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	if pv.OwnersTotal != 3 || pv.OwnersJudged != 3 || pv.Truncated {
		t.Fatalf("preview %+v, want the three holders, not the bare components", pv)
	}
	restore := storage.SetCalcPreviewWorkCap(2)
	pv, err = r.gw.PreviewCalcRule(r.ctx, avgSpec(""), r.all, r.all)
	restore()
	if err != nil || pv.OwnersJudged != 2 || pv.OwnersTotal != 3 || !pv.Truncated {
		t.Fatalf("a work-capped preview %+v (%v), want 2 of 3, truncated", pv, err)
	}
}

// A rule's pending depth counts only the owners the caller may read (#998
// review 5).
func TestCalcStatePendingDepthScoped(t *testing.T) {
	r := newEvalRig(t)
	other, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "other-one"}, r.all, r.all, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.create(avgSpec("temp-mean")); err != nil {
		t.Fatal(err)
	}
	r.metric("room-temperature", "", 20)
	if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: other.ID, Key: "room-temperature", Value: 2, TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	all, err := r.gw.CalcRuleState(r.ctx, "temp-mean", r.all, r.all, 0)
	if err != nil || all.PendingDepth != 2 {
		t.Fatalf("fleet pending %+v (%v), want 2", all, err)
	}
	one, err := r.gw.CalcRuleState(r.ctx, "temp-mean", scope.Set{SelfIDs: []string{r.comp}}, scope.Set{}, 0)
	if err != nil || one.PendingDepth != 1 {
		t.Fatalf("scoped pending %+v (%v), want 1", one, err)
	}
}
