package spectest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Conform holds a family to what the engine and the console rely on of every
// family, over the whole model table (#2832): a family's own test calls it
// first, then tests what is its own. It checks that

//   - the family is one the catalog accepts, and each model resolves to an
//     identity of its own that the device reports back through its state;
//   - every quick set a route lists is accepted by that route's handler,
//     writes the listed path, and a value outside the list is refused 400 with
//     nothing written;
//   - every route that takes no body answers 200 with a body;
//   - every OID the surface serves reads without a state behind it breaking,
//     and lists the state path it reads;
//   - every tick writes only the paths it declares, and runs with no calendar.
func Conform(t *testing.T, s spec.Spec) {
	t.Helper()
	if _, err := spec.NewFamily(s); err != nil {
		t.Fatalf("the catalog refuses the family: %v", err)
	}
	if len(s.Models) == 0 {
		t.Fatalf("family %s has no models: a family is a table", s.Name)
	}
	products := append([]string{s.Name}, modelProducts(s)...)
	for _, product := range products {
		t.Run(product, func(t *testing.T) {
			t.Helper()
			f := NewFake(s, product)
			conformIdentity(t, s, f)
			conformRoutes(t, s, product)
			conformSurface(t, s, f)
			conformTicks(t, s, product)
		})
	}
}

func modelProducts(s spec.Spec) []string {
	out := make([]string, len(s.Models))
	for i, m := range s.Models {
		out[i] = m.Product
	}
	return out
}

// conformIdentity: a model reports its own model name through the state its
// overlay sets, where the family keeps one.
func conformIdentity(t *testing.T, s spec.Spec, f *Fake) {
	t.Helper()
	if f.Row.Product == "" {
		return
	}
	if f.Row.Model == "" {
		t.Errorf("model %s names no model: its identity is the family's", f.Row.Product)
		return
	}
	if _, keeps := look(s.State, "configuration.device.model").(string); keeps {
		if got := String(f); got != f.Row.Model {
			t.Errorf("model %s reports %q as its model in state, its row says %q", f.Row.Product, got, f.Row.Model)
		}
	}
}

// String is the device's reported model name.
func String(f *Fake) string {
	v, _ := f.Read("configuration.device.model").(string)
	return v
}

func conformRoutes(t *testing.T, s spec.Spec, product string) {
	t.Helper()
	for _, r := range s.HTTP {
		method, path, _ := strings.Cut(r.Match, " ")
		for _, set := range r.Sets {
			for _, v := range set.Values {
				f := NewFake(s, product)
				res := r.Handle(f, spec.HTTPRequest{Method: method, Path: path, Body: map[string]any{set.Field: v}})
				if res.Status >= 400 || f.Read(set.Path) != v {
					t.Errorf("%s: %s=%q: status %d, %s reads %v; a listed quick set must be accepted and written", r.Match, set.Field, v, res.Status, set.Path, f.Read(set.Path))
				}
			}
			f := NewFake(s, product)
			res := r.Handle(f, spec.HTTPRequest{Method: method, Path: path, Body: map[string]any{set.Field: "\x00not-a-value"}})
			if res.Status != 400 || len(f.Writes) != 0 {
				t.Errorf("%s: a value outside %v: status %d, %d writes; want 400 and nothing written", r.Match, set.Values, res.Status, len(f.Writes))
			}
		}
		if method == "GET" {
			f := NewFake(s, product)
			res := r.Handle(f, spec.HTTPRequest{Method: method, Path: path})
			if res.Status != 0 && res.Status != 200 || res.Body == nil || len(fmt.Sprint(res.Body)) == 0 {
				t.Errorf("%s: status %d, body %v; a read answers 200 with a body", r.Match, res.Status, res.Body)
			}
		}
	}
}

func conformSurface(t *testing.T, s spec.Spec, f *Fake) {
	t.Helper()
	if s.Surface == nil {
		return
	}
	seen := map[string]bool{}
	for _, o := range s.Surface(f).OIDs {
		if seen[o.OID] {
			t.Errorf("OID %s is served twice", o.OID)
		}
		seen[o.OID] = true
		if o.Get() == nil {
			t.Errorf("OID %s reads nothing", o.OID)
		}
		if o.Path == "" {
			t.Errorf("OID %s lists no state path; the console shows what an OID reads", o.OID)
		}
	}
}

// IdleRoom is the room of a device with no calendar, as the engine reads it.
func IdleRoom() calendar.RoomState {
	return calendar.RoomState{Phase: calendar.PhaseIdle, Occupancy: calendar.PhaseIdle}
}

func conformTicks(t *testing.T, s spec.Spec, product string) {
	t.Helper()
	f := NewFake(s, product)
	ctx := spec.TickContext{Room: IdleRoom(), Now: f.Clock, Rand: behave.NewSource(f.ID)}
	for i := 0; i < 3; i++ {
		for _, u := range f.Ticks(ctx) {
			t.Errorf("a tick wrote what it does not declare: %s", u)
		}
	}
}
