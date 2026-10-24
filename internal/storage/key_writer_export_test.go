package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
	"github.com/jackc/pgx/v5"
)

// SampleWritePaths is the registry of every gateway function that lands a
// sample or an event row, each with the writers it speaks for (ADR-0161).
// TestEverySampleWritePathAdmitsItsWriter reads every insert into metric,
// property and event from this package's source and fails on a function
// missing here or missing its admitWrites call; TestWriterMismatchRefused
// drives every entry with a writer its key's class does not admit. A new write
// path is registered here, with a refusal case there, or both fail.
var SampleWritePaths = map[string][]writerclass.Writer{
	"writeMetricSamples":    {writerclass.Node, writerclass.Push, writerclass.CalcRule},
	"writePropertySamples":  {writerclass.Node, writerclass.Push, writerclass.CalcRule},
	"InsertEvents":          {writerclass.Node, writerclass.Push},
	"recordHealth":          {writerclass.Process},
	"IssueCommand":          {writerclass.Command},
	"writeReplyObservation": {writerclass.CommandReply},
	"SetProperty":           {writerclass.Operator},
	"ClearProperty":         {writerclass.Operator},
	"RetryEndpoint":         {writerclass.KeyPin},
	"writeRuleEvent":        {writerclass.EventRule},
}

// WriteMetricAsRule writes evs through the metric sample path as the calc rule
// ruleID's tick would, in its own transaction.
func (p *PG) WriteMetricAsRule(ctx context.Context, ruleID string, evs []MetricSampleWrite) error {
	return p.sampleWrite(ctx, "write as rule", func(tx pgx.Tx) error {
		return p.writeMetricSamples(ctx, tx, calcWriter(&sampleLineage{ruleID: ruleID, ruleName: "test", version: 1}), evs)
	})
}

// WritePropertyAsRule is WriteMetricAsRule on the property lane.
func (p *PG) WritePropertyAsRule(ctx context.Context, ruleID string, evs []PropertySampleWrite) error {
	return p.sampleWrite(ctx, "write as rule", func(tx pgx.Tx) error {
		return p.writePropertySamples(ctx, tx, calcWriter(&sampleLineage{ruleID: ruleID, ruleName: "test", version: 1}), evs)
	})
}

// WriteReplyObservation records value as a command reply's observation of key
// on a component, as settleFromResponse does.
func (p *PG) WriteReplyObservation(ctx context.Context, componentID, key, instance, value string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := writeReplyObservation(ctx, tx, "component", componentID, key, instance, value, "tcp", time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// WriteRuleEvent records the event rule ruleID's event of eventType on a
// component, as the rule engine's edge does.
func (p *PG) WriteRuleEvent(ctx context.Context, ruleID, eventType, componentID, instance string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	r := EventRule{ID: ruleID, Name: "test-rule", SampleKey: "video-input", Version: 1}
	if err := writeRuleEvent(ctx, tx, r, eventType, &Component{ID: componentID, Name: "test"}, instance, "", edgeSample{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AdmitWrite holds one write of key by writer to the gateway's check, outside
// any write path: what a path speaking for writer would be told.
func (p *PG) AdmitWrite(ctx context.Context, lane string, writer writerclass.Writer, key, instance string) error {
	switch writeLane(lane) {
	case writeMetric, writeProperty, writeEvent:
	default:
		return fmt.Errorf("unknown lane %q", lane)
	}
	return admitWrites(ctx, p.pool, writeLane(lane), gatewayWriter(writer), []keyedWrite{{key: key, instance: instance}})
}
