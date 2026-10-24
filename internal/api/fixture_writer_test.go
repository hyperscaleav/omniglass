package api_test

import (
	"context"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The fixture writers: a test batch speaks for the writer its keys admit
// (ADR-0161). A batch carrying a native probe key is the node's, and a node
// writes only for the component and the endpoint its task ran, so the batch
// lands as one node batch per component and endpoint its samples name, in
// order; every other batch is a push's. A native sample naming no endpoint of
// its component is a fixture the node lane could never produce, and the
// gateway refuses it as it would the node's.

type sampleSink interface {
	InsertMetricSamples(context.Context, storage.KeyWriter, []storage.MetricSampleWrite) error
	InsertPropertySamples(context.Context, storage.KeyWriter, []storage.PropertySampleWrite) error
	InsertEvents(context.Context, storage.KeyWriter, []storage.EventWrite) error
}

// fixtureBatches splits a batch of n samples into the writes its keys admit:
// each a writer and the sample indexes it carries.
func fixtureBatches(n int, key, owner, instance func(int) string) []fixtureBatch {
	native := false
	for i := 0; i < n; i++ {
		if c, ok := writerclass.Reserved(key(i)); ok && c == writerclass.NodeProbe {
			native = true
		}
	}
	if !native {
		all := make([]int, n)
		for i := range all {
			all[i] = i
		}
		return []fixtureBatch{{w: storage.PushWriter(), idx: all}}
	}
	var out []fixtureBatch
	at := map[string]int{}
	for i := 0; i < n; i++ {
		comp, ep := owner(i), instance(i)
		j, ok := at[comp+"\x00"+ep]
		if !ok {
			j = len(out)
			at[comp+"\x00"+ep] = j
			out = append(out, fixtureBatch{w: storage.NodeWriter(comp, ep)})
		}
		out[j].idx = append(out[j].idx, i)
	}
	return out
}

type fixtureBatch struct {
	w   storage.KeyWriter
	idx []int
}

func insertMetrics(ctx context.Context, gw sampleSink, evs []storage.MetricSampleWrite) error {
	for _, b := range fixtureBatches(len(evs), func(i int) string { return evs[i].Key }, func(i int) string { return evs[i].OwnerID }, func(i int) string { return evs[i].Instance }) {
		part := make([]storage.MetricSampleWrite, len(b.idx))
		for k, i := range b.idx {
			part[k] = evs[i]
		}
		if err := gw.InsertMetricSamples(ctx, b.w, part); err != nil {
			return err
		}
	}
	return nil
}

func insertProperties(ctx context.Context, gw sampleSink, evs []storage.PropertySampleWrite) error {
	for _, b := range fixtureBatches(len(evs), func(i int) string { return evs[i].Key }, func(i int) string { return evs[i].OwnerID }, func(i int) string { return evs[i].Instance }) {
		part := make([]storage.PropertySampleWrite, len(b.idx))
		for k, i := range b.idx {
			part[k] = evs[i]
		}
		if err := gw.InsertPropertySamples(ctx, b.w, part); err != nil {
			return err
		}
	}
	return nil
}

func insertEvents(ctx context.Context, gw sampleSink, evs []storage.EventWrite) error {
	for _, b := range fixtureBatches(len(evs), func(i int) string { return evs[i].Key }, func(i int) string { return evs[i].OwnerID }, func(i int) string { return evs[i].Instance }) {
		part := make([]storage.EventWrite, len(b.idx))
		for k, i := range b.idx {
			part[k] = evs[i]
		}
		if err := gw.InsertEvents(ctx, b.w, part); err != nil {
			return err
		}
	}
	return nil
}
