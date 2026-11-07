package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// standardWire is the decoded standard wire shape for the e2e assertions.
type standardWire struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Label            string `json:"label"`
	ParentStandard   string `json:"parent_standard"`
	ParentStandardID string `json:"parent_standard_id"`
	Official         bool   `json:"official"`
}

// TestStandardsAPI drives the standard catalog over HTTP: a viewer reads the
// seeded official rows under the standard:read floor but cannot create, an admin
// (owner) creates a custom standard and a variant of it, an unknown parent is a
// 422, an official row is read-only (422 on patch and delete), a standard a
// system conforms to cannot be deleted (409), and the freed row deletes. Mirrors
// TestProductsAPI: standard is the system-side counterpart of product, so
// standard:* is wired exactly like product:*.
func TestStandardsAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// A plain viewer (read everywhere, write nothing) reads the standards via
	// the standard:read floor (*:read): one forked from a template here, since
	// the boot seeds none.
	c.do(ownerTok, http.MethodPost, "/standards:createFromTemplate", map[string]any{"template": "classroom"}, http.StatusCreated)
	viewerTok := principalWithGrants(t, ctx, dsn, "standard-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	var listed struct {
		Standards []struct {
			ID       string `json:"id"`
			Official bool   `json:"official"`
		} `json:"standards"`
	}
	if err := json.Unmarshal(c.do(viewerTok, http.MethodGet, "/standards", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Standards) == 0 {
		t.Fatalf("standards empty, want the forked row")
	}

	// The viewer cannot create (403, capability fast-reject).
	c.do(viewerTok, http.MethodPost, "/standards",
		map[string]any{"name": "nope", "label": "Nope"}, http.StatusForbidden)

	// Admin (owner) creates a custom standard, then a variant of it.
	var created standardWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/standards",
		map[string]any{"name": "kiosk", "label": "Kiosk"}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Name != "kiosk" || created.Official {
		t.Fatalf("created = %+v, want name=kiosk official=false", created)
	}
	var variant standardWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/standards",
		map[string]any{"name": "kiosk-outdoor", "label": "Outdoor Kiosk", "parent_standard_id": "kiosk"},
		http.StatusCreated), &variant); err != nil {
		t.Fatalf("decode create variant: %v", err)
	}
	if variant.ParentStandard != "kiosk" {
		t.Fatalf("variant parent = %q, want kiosk", variant.ParentStandard)
	}
	c.do(ownerTok, http.MethodDelete, "/standards/kiosk-outdoor", nil, http.StatusNoContent)

	// Duplicate id is a 409; an unknown parent is a 422.
	c.do(ownerTok, http.MethodPost, "/standards",
		map[string]any{"name": "kiosk", "label": "Dup"}, http.StatusConflict)
	c.do(ownerTok, http.MethodPost, "/standards",
		map[string]any{"name": "orphan", "label": "Orphan", "parent_standard_id": "no-such-standard"},
		http.StatusUnprocessableEntity)

	// The custom row is mutable, and the patch reads back on GET.
	c.do(ownerTok, http.MethodPatch, "/standards/kiosk",
		map[string]any{"label": "Info Kiosk"}, http.StatusOK)
	var reread standardWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/kiosk", nil, http.StatusOK), &reread); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if reread.Label != "Info Kiosk" {
		t.Fatalf("patched label = %q, want Info Kiosk", reread.Label)
	}

	// A system conforming to the standard blocks its delete (409); the freed
	// standard then deletes.
	c.do(ownerTok, http.MethodPost, "/systems",
		map[string]any{"name": "k1", "standard_id": "kiosk"}, http.StatusCreated)
	c.do(ownerTok, http.MethodDelete, "/standards/kiosk", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/systems/k1", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodDelete, "/standards/kiosk", nil, http.StatusNoContent)

	// A standard forked from a template is the operator's own, so it is
	// editable rather than read-only. The official read-only
	// guard still exists for genuinely official rows; it is proven at the storage
	// tier, where a test can mint one.
	c.do(ownerTok, http.MethodPatch, "/standards/classroom",
		map[string]any{"label": "Our Classroom"}, http.StatusOK)

	// Unknown id is a 404, on both read and delete.
	c.do(ownerTok, http.MethodGet, "/standards/nope", nil, http.StatusNotFound)
	c.do(ownerTok, http.MethodDelete, "/standards/nope", nil, http.StatusNotFound)
}

// The map declaration on the wire (#791, ADR-0128): PATCH stores it, GET
// carries it back verbatim, an invalid one is a 422 naming the reason, and a
// JSON null clears it.
func TestStandardMapOnTheWire(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/standards", map[string]any{"name": "mapped-room", "label": "Mapped Room"}, http.StatusCreated), &created); err != nil {
		t.Fatalf("create: %v", err)
	}

	mapDecl := map[string]any{
		"aspect": 1.6,
		"positions": []map[string]any{
			{"role": "room-mic", "position": 1, "x": 0.3, "y": 0.5},
			{"role": "main-display", "position": 1, "x": 0.5, "y": 0.05},
		},
	}
	c.do(ownerTok, http.MethodPatch, "/standards/"+created.ID, map[string]any{"map": mapDecl}, http.StatusOK)

	var got struct {
		Map *struct {
			Aspect    float64 `json:"aspect"`
			Positions []struct {
				Role string  `json:"role"`
				X    float64 `json:"x"`
			} `json:"positions"`
		} `json:"map"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/"+created.ID, nil, http.StatusOK), &got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Map == nil || got.Map.Aspect != 1.6 || len(got.Map.Positions) != 2 || got.Map.Positions[0].X != 0.3 {
		t.Fatalf("map on the wire = %+v", got.Map)
	}

	// Out of bounds is a 422, not stored garbage.
	c.do(ownerTok, http.MethodPatch, "/standards/"+created.ID, map[string]any{"map": map[string]any{"aspect": 1.6, "positions": []map[string]any{{"role": "r", "position": 1, "x": 1.2, "y": 0.5}}}}, http.StatusUnprocessableEntity)

	// JSON null clears the declaration.
	c.do(ownerTok, http.MethodPatch, "/standards/"+created.ID, map[string]any{"map": nil}, http.StatusOK)
	var cleared struct {
		Map json.RawMessage `json:"map"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/"+created.ID, nil, http.StatusOK), &cleared); err != nil {
		t.Fatalf("read cleared: %v", err)
	}
	if len(cleared.Map) != 0 && string(cleared.Map) != "null" {
		t.Fatalf("cleared map still on the wire: %s", cleared.Map)
	}
}

// The series reads on the wire (#794): raw samples newest first for both
// owner arcs, sharing the effective reads' route grammar.
func TestSeriesReadsOnTheWire(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "series-probe"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	now := time.Now().UTC()
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "series-probe", Key: "icmp-rtt-avg", Instance: "icmp", Value: 5.0, Source: "t", TS: now.Add(-2 * time.Hour)},
		{OwnerKind: "component", OwnerID: "series-probe", Key: "icmp-rtt-avg", Instance: "icmp", Value: 6.1, Source: "t", TS: now},
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var got struct {
		Samples []struct {
			Value float64 `json:"value"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/series-probe/metrics/icmp-rtt-avg/samples?hours=24", nil, http.StatusOK), &got); err != nil {
		t.Fatalf("read series: %v", err)
	}
	if len(got.Samples) != 2 || got.Samples[0].Value != 6.1 {
		t.Fatalf("series = %+v, want two samples newest first", got.Samples)
	}
}

// The bucketed series read on the wire (#1269): a resolution turns the newest-N
// tail into every regular bucket of the range, a spike survives as a bucket's
// max, and the raw body is untouched when no resolution is asked for.
func TestBucketedSeriesReadsOnTheWire(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bucket-probe"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	hour := time.Now().UTC().Truncate(time.Hour)
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "bucket-probe", Key: "icmp-rtt-avg", Instance: "icmp", Value: 5, Source: "t", TS: hour.Add(-2*time.Hour + time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-probe", Key: "icmp-rtt-avg", Instance: "icmp", Value: 480, Source: "t", TS: hour.Add(-2*time.Hour + 2*time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-probe", Key: "icmp-rtt-avg", Instance: "icmp", Value: 6, Source: "t", TS: hour.Add(-2*time.Hour + 3*time.Minute)},
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "bucket-probe", Key: "firmware-version", Value: "2.1.4", Source: "collection", TS: hour.Add(-2*time.Hour + time.Minute)},
	}); err != nil {
		t.Fatalf("insert property: %v", err)
	}

	var got struct {
		Samples           []json.RawMessage `json:"samples"`
		ResolutionSeconds int               `json:"resolution_seconds"`
		CoveredFrom       time.Time         `json:"covered_from"`
		Buckets           []struct {
			TS    time.Time `json:"ts"`
			Count int       `json:"count"`
			Min   *float64  `json:"min"`
			Max   *float64  `json:"max"`
			Avg   *float64  `json:"avg"`
			Last  *float64  `json:"last"`
		} `json:"buckets"`
	}
	body := c.do(ownerTok, http.MethodGet, "/components/bucket-probe/metrics/icmp-rtt-avg/samples?hours=720&resolution=3600", nil, http.StatusOK)
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("read buckets: %v", err)
	}
	if len(got.Buckets) != 720 || got.ResolutionSeconds != 3600 || len(got.Samples) != 0 {
		t.Fatalf("got %d buckets at %ds with %d raw samples, want 720 at 3600 and no raw samples", len(got.Buckets), got.ResolutionSeconds, len(got.Samples))
	}
	if got.CoveredFrom.IsZero() || got.CoveredFrom.After(time.Now().Add(-13*24*time.Hour)) {
		t.Fatalf("covered_from = %s, want about 14 days back: the hot window, said on the wire", got.CoveredFrom)
	}
	spike := got.Buckets[719-2]
	if spike.Count != 3 || spike.Max == nil || *spike.Max != 480 || spike.Min == nil || *spike.Min != 5 {
		t.Fatalf("spike bucket = %+v, want 3 samples, min 5, max 480", spike)
	}
	if empty := got.Buckets[719-1]; empty.Count != 0 || empty.Max != nil || empty.Avg != nil || empty.Last != nil {
		t.Fatalf("empty bucket = %+v, want count 0 and null aggregates", empty)
	}
	if !strings.Contains(string(body), `"max":null`) {
		t.Fatalf("an empty bucket's aggregates must be present and null on the wire, got %.300s", body)
	}

	var prop struct {
		Buckets []struct {
			Count int     `json:"count"`
			Last  *string `json:"last"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/bucket-probe/properties/firmware-version/samples?hours=24&resolution=3600", nil, http.StatusOK), &prop); err != nil {
		t.Fatalf("read property buckets: %v", err)
	}
	if len(prop.Buckets) != 24 || prop.Buckets[21].Count != 1 || prop.Buckets[21].Last == nil || *prop.Buckets[21].Last != "2.1.4" {
		t.Fatalf("property buckets = %+v, want 24 with the change in the third from last", prop.Buckets)
	}

	// No resolution: the raw body, key for key as before, no bucket fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/bucket-probe/metrics/icmp-rtt-avg/samples?hours=24", nil, http.StatusOK), &raw); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	for _, k := range []string{"buckets", "covered_from", "resolution_seconds"} {
		if _, ok := raw[k]; ok {
			t.Fatalf("raw body carries %q, want the unchanged raw shape", k)
		}
	}

	// A resolution that would make more buckets than the cap is a 422, never a
	// truncated grid; an out-of-scope owner stays the non-disclosing 404.
	c.do(ownerTok, http.MethodGet, "/components/bucket-probe/metrics/icmp-rtt-avg/samples?hours=720&resolution=1", nil, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodGet, "/components/no-such/metrics/icmp-rtt-avg/samples?resolution=60", nil, http.StatusNotFound)

	// A resolution past 30 days is the schema's 422, not a 500 from an
	// overflowed duration (#1485).
	c.do(ownerTok, http.MethodGet, "/components/bucket-probe/metrics/icmp-rtt-avg/samples?resolution=2592001", nil, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodGet, "/components/bucket-probe/properties/firmware-version/samples?resolution=9223372036", nil, http.StatusUnprocessableEntity)

	// A second instance of the same metric on the same owner is another series
	// (#1485): unnamed, the read is refused with both instances listed; named,
	// the buckets hold that instance's samples alone.
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "wire-level", DataType: "float"}); err != nil {
		t.Fatalf("create metric type: %v", err)
	}
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "bucket-probe", Key: "wire-level", Instance: "a", Value: 5, Source: "t", TS: hour.Add(-2*time.Hour + time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-probe", Key: "wire-level", Instance: "a", Value: 480, Source: "t", TS: hour.Add(-2*time.Hour + 2*time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-probe", Key: "wire-level", Instance: "a", Value: 6, Source: "t", TS: hour.Add(-2*time.Hour + 3*time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-probe", Key: "wire-level", Instance: "b", Value: 900, Source: "t", TS: hour.Add(-2*time.Hour + 4*time.Minute)},
	}); err != nil {
		t.Fatalf("insert two instances: %v", err)
	}
	var problem struct {
		Detail string `json:"detail"`
		Errors []struct {
			Location string `json:"location"`
			Value    string `json:"value"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/bucket-probe/metrics/wire-level/samples?hours=24&resolution=3600", nil, http.StatusUnprocessableEntity), &problem); err != nil {
		t.Fatalf("read the ambiguity problem: %v", err)
	}
	var listed []string
	for _, e := range problem.Errors {
		if e.Location == "query.instance" {
			listed = append(listed, e.Value)
		}
	}
	if strings.Join(listed, ",") != "a,b" {
		t.Fatalf("ambiguity problem = %+v, want both instances listed at query.instance", problem)
	}
	var one struct {
		Instance string `json:"instance"`
		Buckets  []struct {
			Count int      `json:"count"`
			Max   *float64 `json:"max"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/bucket-probe/metrics/wire-level/samples?hours=24&resolution=3600&instance=a", nil, http.StatusOK), &one); err != nil {
		t.Fatalf("read one instance: %v", err)
	}
	if b := one.Buckets[23-2]; one.Instance != "a" || b.Count != 3 || b.Max == nil || *b.Max != 480 {
		t.Fatalf("instance a read = instance %q bucket %+v, want a's 3 samples with max 480, not b's 900", one.Instance, b)
	}
}
