package spec_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

func TestCatalogResolvesAProductToItsYAMLProfileWhenNoFamilyClaimsIt(t *testing.T) {
	store := loadYAML(t)
	cat, err := spec.NewCatalog(store, spectest.Spec())
	if err != nil {
		t.Fatal(err)
	}
	for product, overlay := range map[string]map[string]any{
		"yaml-display":     nil,
		"yaml-display-big": {"size": 65},
	} {
		r, ok := cat.ForProduct(product)
		if !ok || r.Definition.ID() != "yaml-display" || r.Definition.Kind() != spec.KindYAML || !reflect.DeepEqual(r.Overlay, overlay) {
			t.Errorf("%s resolved to %+v (ok=%v)", product, r, ok)
		}
		if r.Identity.Manufacturer != "Acme" || r.Identity.DeviceType != "display" {
			t.Errorf("%s identity = %+v", product, r.Identity)
		}
	}
	if _, ok := cat.ForProduct("nothing"); ok {
		t.Error("an unknown product resolved")
	}
}

func TestCatalogResolvesAProductToTheFamilyThatClaimsIt(t *testing.T) {
	cat, err := spec.NewCatalog(loadYAML(t), spectest.Spec())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range spectest.Rows {
		r, ok := cat.ForProduct(row.Product)
		if !ok || r.Definition.ID() != spectest.Name || r.Definition.Kind() != spec.KindGo {
			t.Fatalf("%s resolved to %+v (ok=%v)", row.Product, r, ok)
		}
		if got, ok := spec.Row[spectest.Row](r.Model); !ok || got != row {
			t.Errorf("%s row = %+v (ok=%v), want %+v", row.Product, got, ok, row)
		}
		if r.Identity.Manufacturer != "Testfamily" || !strings.HasPrefix(r.Identity.Model, "Display ") || r.Identity.DeviceType != "display" {
			t.Errorf("%s identity = %+v: the model's overrides the family's, which fills the rest", row.Product, r.Identity)
		}
		if !reflect.DeepEqual(r.Definition.Products(), []string{spectest.Name, "tf-display-55", "tf-display-65"}) {
			t.Errorf("products = %v", r.Definition.Products())
		}
	}
	// The family's own name resolves to the family with no model.
	r, ok := cat.ForProduct(spectest.Name)
	if !ok || r.Definition.ID() != spectest.Name || r.Model.Product != "" || r.Overlay != nil {
		t.Errorf("the family's own name resolved to %+v (ok=%v)", r, ok)
	}
	if d, ok := cat.Lookup(spectest.Name); !ok || d.Kind() != spec.KindGo {
		t.Error("Lookup by the family's id did not find the family")
	}
}

// A family that claims a product a YAML profile also simulates wins it; the
// profile still serves the rest.
func TestCatalogFamilyWinsAProductAYAMLProfileAlsoSimulates(t *testing.T) {
	fam := spectest.Spec()
	fam.Models = append(fam.Models, spec.Model{Product: "yaml-display-big"})
	cat, err := spec.NewCatalog(loadYAML(t), fam)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := cat.ForProduct("yaml-display-big"); r.Definition.ID() != spectest.Name {
		t.Errorf("yaml-display-big resolved to %s, want the family", r.Definition.ID())
	}
	if r, _ := cat.ForProduct("yaml-display"); r.Definition.ID() != "yaml-display" {
		t.Errorf("yaml-display resolved to %s, want its profile", r.Definition.ID())
	}
}

func TestCatalogRefusesWhatItCouldNotServeFaithfully(t *testing.T) {
	cases := map[string]struct {
		mutate func(*spec.Spec)
		want   string
	}{
		"a name a profile has":     {func(s *spec.Spec) { s.Name = "yaml-display" }, "the name of another definition"},
		"a model with no product":  {func(s *spec.Spec) { s.Models = append(s.Models, spec.Model{}) }, "no product"},
		"a product listed twice":   {func(s *spec.Spec) { s.Models = append(s.Models, s.Models[0]) }, "twice"},
		"no name":                  {func(s *spec.Spec) { s.Name = "" }, "no name"},
		"a route with no handler":  {func(s *spec.Spec) { s.HTTP = append(s.HTTP, spec.HTTPRoute{Match: "GET /x"}) }, "no handler"},
		"a route with no method":   {func(s *spec.Spec) { s.HTTP[0].Match = "/x" }, "METHOD /path"},
		"routes with no http face": {func(s *spec.Spec) { s.Interfaces = nil }, "no http interface"},
		"a route listed twice":     {func(s *spec.Spec) { s.HTTP = append(s.HTTP, s.HTTP[0]) }, "route \"GET /api/info\" twice"},
		"a tick with no interval": {func(s *spec.Spec) {
			s.Ticks = []spec.Tick{{Name: "t", Run: func(spec.Device, spec.TickContext) []surface.Change { return nil }}}
		}, "no interval"},
		"a command listed twice":     {func(s *spec.Spec) { s.Commands = append(s.Commands, s.Commands[0]) }, "twice"},
		"commands with no line face": {func(s *spec.Spec) { s.Interfaces = s.Interfaces[:1] }, "no ssh, telnet, tcp or udp interface"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fam := spectest.Spec()
			c.mutate(&fam)
			_, err := spec.NewCatalog(loadYAML(t), fam)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one containing %q", err, c.want)
			}
		})
	}
	t.Run("two families claiming one product", func(t *testing.T) {
		other := spectest.Spec()
		other.Name = "testfamily-other"
		_, err := spec.NewCatalog(nil, spectest.Spec(), other)
		if err == nil || !strings.Contains(err.Error(), "claimed by both") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDefinitionsListsEveryDefinitionByID(t *testing.T) {
	cat, err := spec.NewCatalog(loadYAML(t), spectest.Spec())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range cat.Definitions() {
		ids = append(ids, d.ID())
	}
	if !reflect.DeepEqual(ids, []string{spectest.Name, "yaml-display"}) {
		t.Fatalf("ids = %v", ids)
	}
}

func TestTableKeepsEachRowBesideItsModel(t *testing.T) {
	type row struct {
		P string
		N int
	}
	ms := spec.Table([]row{{"a", 1}, {"b", 2}}, func(r row) spec.Model { return spec.Model{Product: r.P} })
	if len(ms) != 2 || ms[1].Product != "b" {
		t.Fatalf("models = %+v", ms)
	}
	if r, ok := spec.Row[row](ms[1]); !ok || r.N != 2 {
		t.Fatalf("row = %+v ok=%v", r, ok)
	}
	if _, ok := spec.Row[int](ms[0]); ok {
		t.Fatal("a row read back as another type")
	}
}

// A family of one product may be named for it: the product resolves to the
// family itself with the row's identity, listed once, and a product listed twice
// is refused.
func TestAFamilyOfOneIsNamedForItsProduct(t *testing.T) {
	one := spec.Spec{
		Name:     "solo-box",
		Identity: spec.Identity{Manufacturer: "Acme", Model: "Box", DeviceType: "box"},
		Models: []spec.Model{{Product: "solo-box", Model: "Box 1", Summary: "the only one",
			State: map[string]any{"configuration": map[string]any{"device": map[string]any{"model": "Box 1"}}}}},
	}
	cat, err := spec.NewCatalog(nil, one)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := cat.ForProduct("solo-box")
	if !ok || r.Definition.ID() != "solo-box" || r.Identity.Model != "Box 1" || r.Identity.Summary != "the only one" || r.Model.Product != "solo-box" {
		t.Fatalf("solo-box resolves to %+v (ok=%v)", r, ok)
	}
	if got := r.Definition.Products(); !reflect.DeepEqual(got, []string{"solo-box"}) {
		t.Errorf("products = %v, want the one product once", got)
	}
	one.Models = append(one.Models, one.Models[0])
	if _, err := spec.NewCatalog(nil, one); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("a product listed twice: err = %v, want a refusal", err)
	}
}

// A push face a family declares must have a body and a cadence a sender of record
// would keep.
func TestAFamilyPushFaceNeedsABodyAndACadence(t *testing.T) {
	body := func(spec.Device, string) ([]byte, error) { return []byte("{}"), nil }
	base := spec.Spec{Name: "pusher", Models: []spec.Model{{Product: "pusher-1"}}}
	for name, face := range map[string]*spec.PushFace{
		"no body": {Every: 30 * time.Second}, "too fast": {Every: time.Millisecond, Body: body},
	} {
		s := base
		s.Push = face
		if _, err := spec.NewFamily(s); err == nil {
			t.Errorf("%s: the family was accepted", name)
		}
	}
	s := base
	s.Push = &spec.PushFace{Every: 30 * time.Second, Body: body}
	f, err := spec.NewFamily(s)
	if err != nil {
		t.Fatal(err)
	}
	if p := f.PushFace(); p == nil || p.Every != "30s" {
		t.Errorf("push face = %+v, want one every 30s", p)
	}
	if f.WebhookFace() != nil {
		t.Error("a family that declares no webhook face has one")
	}
}
