package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// EffectiveMetric is one metric resolved for an instance, the metric twin of
// EffectiveProperty with the set-value side read from the metric SERIES: the
// split migration (#587) bars declared numerics from the property store, so a
// declared metric's "own value" is the latest observed or calculated sample
// (Value = coalesce(sample, contract default), IsSampled marking a live
// series). FromContract distinguishes a metric the instance's classifier
// declares from an ad-hoc series landing directly on the instance. There is no
// ValueID: a sample is telemetry, not an override a surface can clear.
type EffectiveMetric struct {
	MetricTypeName string
	MetricTypeID   string
	DisplayName    string // optional human label; empty when unset
	DataType       string
	Required       bool // from the contract; always false for an ad-hoc metric
	DefaultValue   json.RawMessage
	SampleValue    json.RawMessage // nil when the series holds no sample
	Value          json.RawMessage // coalesce(SampleValue, DefaultValue)
	IsSampled      bool
	FromContract   bool
}

// EffectiveMetrics resolves an instance's declared metrics: every metric its
// classifier's contract declares (value = coalesce(the series' latest sample,
// the contract default)), plus any ad-hoc metric the series holds that the
// contract does not declare. Parameterized over the same ownerContracts table
// as EffectiveProperties, so component, system, location, and node resolve
// through one code path and cannot drift apart. The instance must be within the
// read scope; an out-of-scope instance is its non-disclosing not-found.
//
// The sample side is the LatestMetric rule: the newest row per metric across
// every instance dimension (ts desc, id desc breaking same-instant ties),
// confined to the provenances that report reality (observed, calculated). An
// intended row is what a command told the device, not what the metric is, so it
// never satisfies the resolution.
func (p *PG) EffectiveMetrics(ctx context.Context, ownerKind, ownerID string, read scope.Set) ([]EffectiveMetric, error) {
	oc, ok := ownerContracts[ownerKind]
	if !ok {
		return nil, ErrUnknownOwnerKind
	}
	inScope, err := p.ownerInScope(ctx, p.pool, ownerKind, ownerID, read)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, oc.notFound
	}
	// Resolved once here (see EffectiveProperties, the property-lane twin
	// this mirrors exactly): the inst CTEs below used to look the instance
	// row up by name, a CTE that can hold more than one row once a name is
	// no longer unique (#627), silently joining samples from every
	// same-named row into one answer rather than raising an error.
	arc, err := p.ownerArcValue(ctx, p.pool, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}

	// The current sample per metric: the latest series row on this owner's arc,
	// rendered to jsonb so the coalesce against the contract default stays in
	// SQL. Shared by both arms.
	sampled := fmt.Sprintf(`
		sampled as (
			select distinct on (m.metric_type_id)
			       m.metric_type_id,
			       to_jsonb(m.value) as value
			from metric m
			join inst on m.%[1]s = inst.arc
			where m.provenance in ('observed', 'calculated')
			order by m.metric_type_id, m.ts desc, m.id desc
		)`, oc.arcCol)

	var q string
	if oc.metricContractTable == "" {
		// The ad-hoc arm alone when the owner kind has no classifier to inherit
		// from: every series the instance's arc holds.
		q = fmt.Sprintf(`
		with inst as (select %[3]s as arc from %[1]s where %[3]s = $1), %[2]s
		select mt.name as metric_type_name, mt.id as metric_type_id, mt.display_name, mt.data_type, false as required,
		       null::jsonb as default_value,
		       s.value as sample_value,
		       s.value as effective_value,
		       true as is_sampled,
		       false as from_contract
		from sampled s
		join metric_type mt on mt.id = s.metric_type_id
		order by 1`,
			oc.instanceTable, sampled, oc.arcMatch)
	} else {
		q = fmt.Sprintf(`
		with inst as (
			select %[6]s as arc, %[2]s as classifier from %[1]s where %[6]s = $1
		), %[5]s
		-- The contract arm: what the instance's classifier declares, resolved
		-- against the series' latest sample.
		select mt.name as metric_type_name, mt.id as metric_type_id, mt.display_name, mt.data_type, c.required,
		       c.default_value,
		       s.value as sample_value,
		       coalesce(s.value, c.default_value) as effective_value,
		       (s.value is not null) as is_sampled,
		       true as from_contract
		from inst
		join %[3]s c on c.%[4]s = inst.classifier
		join metric_type mt on mt.id = c.metric_type_id
		left join sampled s on s.metric_type_id = c.metric_type_id
		union all
		-- The ad-hoc arm: series landing directly on the instance for metrics the
		-- contract does not declare.
		select mt.name, mt.id, mt.display_name, mt.data_type, false,
		       null::jsonb,
		       s.value, s.value, true, false
		from inst
		cross join sampled s
		join metric_type mt on mt.id = s.metric_type_id
		where not exists (
			select 1 from %[3]s c
			where c.%[4]s = inst.classifier and c.metric_type_id = s.metric_type_id
		)
		order by 1`,
			oc.instanceTable, oc.classifierCol, oc.metricContractTable, oc.contractKeyCol, sampled, oc.arcMatch)
	}

	rows, err := p.pool.Query(ctx, q, arc)
	if err != nil {
		return nil, fmt.Errorf("storage: effective metrics %s/%s: %w", ownerKind, ownerID, err)
	}
	defer rows.Close()

	var out []EffectiveMetric
	for rows.Next() {
		var (
			e                EffectiveMetric
			def, sample, val []byte
			displayName      *string // NULL when unset
		)
		if err := rows.Scan(&e.MetricTypeName, &e.MetricTypeID, &displayName, &e.DataType, &e.Required,
			&def, &sample, &val, &e.IsSampled, &e.FromContract); err != nil {
			return nil, fmt.Errorf("storage: scan effective metric: %w", err)
		}
		if displayName != nil {
			e.DisplayName = *displayName
		}
		e.DefaultValue = copyRaw(def)
		e.SampleValue = copyRaw(sample)
		e.Value = copyRaw(val)
		out = append(out, e)
	}
	return out, rows.Err()
}
