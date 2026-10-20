package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

// The validate route's site enum is a struct tag (Huma reads it for the
// OpenAPI), so it is checked against the registry: a site registered in
// internal/rules and not here, or the reverse, fails.
func TestValidateSiteEnumIsTheRegistry(t *testing.T) {
	f, ok := reflect.TypeFor[validateExpressionInput]().FieldByName("Body")
	if !ok {
		t.Fatal("no Body")
	}
	site, _ := f.Type.FieldByName("Site")
	if got, want := strings.Split(site.Tag.Get("enum"), ","), rules.SiteNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("the site enum is %v, the registry %v", got, want)
	}
}
