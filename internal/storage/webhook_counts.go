package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// WebhookCount is one webhook endpoint's counts for a collection report
// window (#1369), as the receiver took them: the posts it accepted (nil when
// the window reports none) and the posts it refused (nil when none), landed
// on the endpoint's component under the endpoint's name at the window's end.
type WebhookCount struct {
	ComponentID string
	Endpoint    string
	Posts       *int
	Refusals    *int
	At          time.Time
}

// RecordWebhookCounts lands a window's webhook counts as the components'
// metric series, observed. The process writes them (the webhook receiver, a
// platform writer), so the gateway refuses the keys to a node and to a push:
// the server counts its own posts. An event or calc rule reads them as it
// reads any metric.
//
// Each component's counts land in a transaction of their own, so a refusal
// costs only its own rows, as the node lane drops an orphaned batch alone: a
// component deleted since its last post (a decommissioned device, a lab torn
// down) is dropped, and every other component's window still lands. Any
// other failure is returned, after the rest have been tried.
func (p *PG) RecordWebhookCounts(ctx context.Context, counts []WebhookCount) error {
	byComponent := map[string][]MetricSampleWrite{}
	var order []string
	add := func(c WebhookCount, key string, n *int) {
		if n == nil {
			return
		}
		if _, seen := byComponent[c.ComponentID]; !seen {
			order = append(order, c.ComponentID)
		}
		byComponent[c.ComponentID] = append(byComponent[c.ComponentID], MetricSampleWrite{OwnerKind: "component",
			OwnerID: c.ComponentID, Key: key, Instance: c.Endpoint, Value: float64(*n), TS: c.At})
	}
	for _, c := range counts {
		add(c, writerclass.WebhookPostCount, c.Posts)
		add(c, writerclass.WebhookRefusalCount, c.Refusals)
	}
	var errs []error
	for _, id := range order {
		rows := byComponent[id]
		err := p.sampleWrite(ctx, "record webhook counts", func(tx pgx.Tx) error {
			return p.writeMetricSamples(ctx, tx, gatewayWriter(writerclass.Process), rows)
		})
		if err != nil && !errors.Is(err, ErrComponentNotFound) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CollectionSettings is the effective collection namespace (#1369): the
// platform's collection report window, which the webhook receiver reports
// its counts over and a node reads off its worklist.
func (p *PG) CollectionSettings(ctx context.Context) (settings.CollectionSettings, error) {
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return settings.CollectionSettings{}, fmt.Errorf("storage: collection settings: %w", err)
	}
	return s.Collection, nil
}
