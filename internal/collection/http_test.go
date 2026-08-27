package collection_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The http ladder's sample shapes, over a faked prober so the emission logic
// is hermetic. The real request path is the integration test's job (the
// capability carve-out); these pin what each rung outcome turns into on the
// wire: which names, which lanes, and which samples are ABSENT.

type fakeHTTPProber struct{ res collection.HTTPResult }

func (f fakeHTTPProber) Probe(context.Context, string, time.Duration) (collection.HTTPResult, error) {
	return f.res, nil
}

func httpSamplesByName(t *testing.T, res collection.HTTPResult) map[string]collection.Sample {
	t.Helper()
	r := &collection.Runner{HTTP: fakeHTTPProber{res: res}}
	dps, err := r.CollectHTTP(context.Background(), collection.HTTPTask{Target: "10.0.0.1:80"})
	if err != nil {
		t.Fatalf("CollectHTTP: %v", err)
	}
	out := map[string]collection.Sample{}
	for _, d := range dps {
		out[d.Name] = d
	}
	if len(out) != len(dps) {
		t.Fatalf("duplicate sample names in %v", dps)
	}
	return out
}

func TestCollectHTTPResponded(t *testing.T) {
	dps := httpSamplesByName(t, collection.HTTPResult{
		Dialed: true, Responded: true, Status: 500, DialMS: 3.5, ResponseMS: 12.5,
		Reason: collection.Responded,
	})
	if dps["tcp-open"].Value != 1.0 {
		t.Errorf("tcp-open = %v, want 1 (the dial succeeded)", dps["tcp-open"].Value)
	}
	if dps["tcp-connect-time"].Value != 3.5 {
		t.Errorf("tcp-connect-time = %v, want the dial ms", dps["tcp-connect-time"].Value)
	}
	if dps["http-response-time"].Value != 12.5 {
		t.Errorf("http-response-time = %v, want the response ms", dps["http-response-time"].Value)
	}
	// A 500 is an API answering: the responds rung is up on ANY drawn response.
	v := dps["endpoint-responsive"]
	if !v.IsText || v.Text != "up" {
		t.Errorf("endpoint-responsive = %+v, want text up", v)
	}
}

func TestCollectHTTPReachedButNotResponsive(t *testing.T) {
	dps := httpSamplesByName(t, collection.HTTPResult{
		Dialed: true, Responded: false, DialMS: 2.0, Reason: collection.Timedout,
	})
	if dps["tcp-open"].Value != 1.0 {
		t.Errorf("tcp-open = %v, want 1: the port accepted", dps["tcp-open"].Value)
	}
	if _, ok := dps["http-response-time"]; ok {
		t.Error("http-response-time emitted with no response; absence is the fact")
	}
	v := dps["endpoint-responsive"]
	if !v.IsText || v.Text != "down" {
		t.Errorf("endpoint-responsive = %+v, want text down", v)
	}
	if v.Labels[collection.ReasonLabel] != string(collection.Timedout) {
		t.Errorf("responsive reason = %q, want timedout", v.Labels[collection.ReasonLabel])
	}
}

func TestCollectHTTPPortClosed(t *testing.T) {
	dps := httpSamplesByName(t, collection.HTTPResult{
		Dialed: false, Reason: collection.Refused,
	})
	if dps["tcp-open"].Value != 0.0 {
		t.Errorf("tcp-open = %v, want 0", dps["tcp-open"].Value)
	}
	if _, ok := dps["tcp-connect-time"]; ok {
		t.Error("tcp-connect-time emitted on a refused dial")
	}
	if dps["endpoint-responsive"].Text != "down" {
		t.Errorf("endpoint-responsive = %+v, want down", dps["endpoint-responsive"])
	}
}

func TestCollectHTTPEmptyTarget(t *testing.T) {
	r := &collection.Runner{HTTP: fakeHTTPProber{}}
	if _, err := r.CollectHTTP(context.Background(), collection.HTTPTask{}); err == nil {
		t.Fatal("an empty target must refuse, not probe nothing")
	}
}
