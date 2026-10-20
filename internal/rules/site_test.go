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
	if names := SiteNames(); !reflect.DeepEqual(names, []string{"criteria"}) {
		t.Errorf("SiteNames() = %v, want [criteria]", names)
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
	if s.Default != ValueText {
		t.Errorf("default value type %q, want text", s.Default)
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
	if len(doc.Sites) != 1 || doc.Sites[0].Name != "criteria" || doc.Sites[0].Default != ValueText {
		t.Fatalf("sites = %+v", doc.Sites)
	}
	site, _ := LookupSite("criteria")
	got := doc.Sites[0]
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
