package rules

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/expression"
)

// The criteria site is registered once, with a variant per value type: the
// lane and data type pick it, and no key picks the documented default.
func TestCriteriaSite(t *testing.T) {
	s, ok := LookupSite("criteria")
	if !ok {
		t.Fatal("the criteria site is not registered")
	}
	if names := SiteNames(); !reflect.DeepEqual(names, []string{"action", "calc", "criteria"}) {
		t.Errorf("SiteNames() = %v, want [action calc criteria]", names)
	}
	if _, ok := LookupSite("filter"); ok {
		t.Error("an unregistered site was found")
	}
	for _, tc := range []struct {
		lane     Lane
		dataType string
		want     string
		env      expression.Env
	}{
		{LaneMetric, "", ValueNumber, metricEnv},
		{LaneProperty, "string", ValueText, textEnv},
		{LaneProperty, "json", ValueText, textEnv},
		{LaneProperty, "bool", ValueBool, boolEnv},
	} {
		vt := ValueTypeFor(tc.lane, tc.dataType)
		v, ok := s.Variant(vt)
		if vt != tc.want || !ok || v.Env != tc.env || v.Env != CriteriaEnv(tc.lane, tc.dataType) {
			t.Errorf("%s %s: value type %q, variant env mismatch", tc.lane, tc.dataType, vt)
		}
	}
	if !s.KeyRequired {
		t.Error("the criteria site does not require a key; a rule always reads one")
	}
	if _, ok := s.Variant("matrix"); ok {
		t.Error("an unknown value type was found")
	}
}

// The facts the docs render are the site's own declaration: each variant's
// bindings are its Env's, with its builtins and budget.
func TestSiteFactsAreTheEnvs(t *testing.T) {
	var doc SitesFacts
	raw, err := SitesFactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Sites) != 3 || doc.Sites[0].Name != "action" || doc.Sites[1].Name != "calc" || doc.Sites[2].Name != "criteria" || !doc.Sites[2].KeyRequired || doc.Sites[0].KeyRequired {
		t.Fatalf("sites = %+v", doc.Sites)
	}
	site, _ := LookupSite("criteria")
	got := doc.Sites[2]
	if len(got.Variants) != len(site.Variants) {
		t.Fatalf("%d variants, want %d", len(got.Variants), len(site.Variants))
	}
	for i, v := range site.Variants {
		if got.Variants[i].ValueType != v.ValueType || !reflect.DeepEqual(got.Variants[i].Bindings, v.Env.Bindings()) {
			t.Errorf("variant %s: %+v, want the env's bindings %+v", v.ValueType, got.Variants[i], v.Env.Bindings())
		}
	}
	env := site.Variants[0].Env
	if !reflect.DeepEqual(got.Builtins, env.Builtins()) || got.Budget.Iterations != env.Budget().Iterations ||
		got.Budget.Memory != env.Budget().Memory || got.Budget.Deadline != env.Budget().Deadline.String() {
		t.Errorf("builtins %v budget %+v, want the env's", got.Builtins, got.Budget)
	}
	names := map[string]bool{}
	for _, b := range got.Variants[0].Bindings {
		names[b.Name] = true
	}
	for _, n := range []string{"value", "key", "instance", "component.name", "component.type"} {
		if !names[n] {
			t.Errorf("the criteria bindings lack %s", n)
		}
	}
}

// The committed facts the docs import equal a fresh render (make gen).
func TestSitesFactsMatchCommitted(t *testing.T) {
	fresh, err := SitesFactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/expressions.json")
	if err != nil {
		t.Fatalf("read committed expressions.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/expressions.json drifted from the expression sites: run make gen and commit the result")
	}
}

// The docs list each binding once across a site's variants, so a binding's
// doc must read true for every variant (only its type may differ).
func TestBindingDocsAgreeAcrossVariants(t *testing.T) {
	for _, name := range SiteNames() {
		s, _ := LookupSite(name)
		docs := map[string]string{}
		for _, v := range s.Variants {
			for _, b := range v.Env.Bindings() {
				if d, ok := docs[b.Name]; ok && d != b.Doc {
					t.Errorf("%s: binding %s reads %q for %s, %q for an earlier variant", name, b.Name, b.Doc, v.ValueType, d)
				}
				docs[b.Name] = b.Doc
			}
		}
	}
}

// The committed alias rules the console imports equal a fresh render (make
// gen), so the form refuses what the server refuses.
func TestCalcNamesMatchCommitted(t *testing.T) {
	committed, err := os.ReadFile("../../web/src/lib/calc_names.gen.ts")
	if err != nil {
		t.Fatalf("read committed calc_names.gen.ts (run make gen and commit it): %v", err)
	}
	if string(committed) != string(CalcNamesTS()) {
		t.Error("web/src/lib/calc_names.gen.ts drifted from the calc site: run make gen and commit the result")
	}
}

// Each site names itself on every environment it compiles against, a calc
// rule's built one included, so the diagnostic rules for that site run on
// its expressions (#1021).
func TestSitesNameThemselves(t *testing.T) {
	for _, name := range SiteNames() {
		s, _ := LookupSite(name)
		for _, v := range s.Variants {
			if got := v.Env.Site(); got != name {
				t.Errorf("%s %s variant names site %q", name, v.ValueType, got)
			}
		}
	}
	env, err := CalcEnv(OwnerSystem, []TypedInput{{Alias: "t", Key: "k", ValueType: ValueNumber}}, ValueNumber)
	if err != nil {
		t.Fatal(err)
	}
	if env.Site() != "calc" {
		t.Errorf("a calc rule's env names site %q", env.Site())
	}
}

// A diagnostic rule applies at registered sites only: a site name it gives
// that no site has is a typo that would silently never run.
func TestDiagnosticSitesAreRegistered(t *testing.T) {
	for _, d := range expression.DiagnosticRules() {
		for _, site := range d.Sites {
			if _, ok := LookupSite(site); !ok {
				t.Errorf("diagnostic %s applies at %q, which is not a registered site", d.Code, site)
			}
		}
	}
}
