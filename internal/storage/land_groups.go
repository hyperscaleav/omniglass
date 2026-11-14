package storage

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/jackc/pgx/v5"
)

// IngestGroup is one task's share of an ingest batch (#1582): the rows one
// tick of a node collected for one task, under the writer the task resolves
// to (NodeWriter, its Node set). A batch carries many, and lands them all in
// one transaction; a group a row of which is refused (IsRefusedWrite) is
// dropped on its own, its siblings land.
type IngestGroup struct {
	// TaskID is the task the group's rows were collected for, reported back
	// on a refusal so the caller can name it.
	TaskID     string
	Writer     KeyWriter
	Metrics    []MetricSampleWrite
	Properties []PropertySampleWrite
	Events     []EventWrite
	LogLines   []LogLineWrite
	Fanned     *FannedRows
	Unbound    []UnboundIdentityWrite
	// SeenEndpoint is the endpoint that delivered the group's task, stamped
	// seen (endpoint_live) in the batch's transaction unless the group is
	// refused (#933, #1928). Empty stamps nothing.
	SeenEndpoint string
}

// GroupRefusal is one group a batch dropped: its position in
// IngestBatch.Groups, its task, and the refusal (IsRefusedWrite holds).
type GroupRefusal struct {
	Index  int
	TaskID string
	Err    error
}

// LandResult is what LandBatch did with a batch. Landed is false, with
// nothing written, when the batch id already landed (a redelivery). Refused
// is every group the batch dropped for a refusal, in their order; the rest of
// the batch, and its receipt, landed.
type LandResult struct {
	Landed  bool
	Refused []GroupRefusal
	// Outcome is the batch's items by kind and outcome (#1641): the
	// caller's IngestBatch.Tally plus what the committed pass stored,
	// coalesced and refused. Empty when nothing landed.
	Outcome delivery.Tally
}

// landGroup is one group as landBatch walks it. whole marks the batch's
// top-level rows (IngestBatch.Writer and its lanes): a refusal there fails
// the batch, as it always has, rather than dropping a group. index is the
// group's position in IngestBatch.Groups, -1 for the top level.
type landGroup struct {
	IngestGroup
	index int
	whole bool
}

// groups is the batch as the groups it lands: its top-level rows first, when
// it carries any, then each of Groups in its order.
func (b IngestBatch) groups() []landGroup {
	out := make([]landGroup, 0, len(b.Groups)+1)
	top := IngestGroup{Writer: b.Writer, Metrics: b.Metrics, Properties: b.Properties, Events: b.Events,
		LogLines: b.LogLines, Fanned: b.Fanned, Unbound: b.Unbound}
	if top.carries() {
		out = append(out, landGroup{IngestGroup: top, index: -1, whole: true})
	}
	for i, g := range b.Groups {
		out = append(out, landGroup{IngestGroup: g, index: i})
	}
	return out
}

// carries reports whether the group has any row to land.
func (g IngestGroup) carries() bool {
	f := g.Fanned != nil && (len(g.Fanned.Metrics)+len(g.Fanned.Properties)+len(g.Fanned.Events) > 0)
	return f || len(g.Metrics)+len(g.Properties)+len(g.Events)+len(g.LogLines)+len(g.Unbound) > 0
}

// allMetrics is every metric sample the batch carries, its groups' and
// fanned rows' and its node-self report's included: the days a partition
// miss has to make.
func (b IngestBatch) allMetrics() []MetricSampleWrite {
	var out []MetricSampleWrite
	if b.NodeSelf != nil {
		out = append(out, b.NodeSelf.writes("")...)
	}
	for _, g := range b.groups() {
		out = append(out, g.Metrics...)
		if g.Fanned != nil {
			out = append(out, g.Fanned.Metrics...)
		}
	}
	return out
}

// ownerRefs is every owner reference a group's rows name, as kind and
// reference.
func (g IngestGroup) ownerRefs() [][2]string {
	var out [][2]string
	add := func(kind, ref string) { out = append(out, [2]string{kind, ref}) }
	for _, r := range g.Metrics {
		add(r.OwnerKind, r.OwnerID)
	}
	for _, r := range g.Properties {
		add(r.OwnerKind, r.OwnerID)
	}
	for _, r := range g.Events {
		add(r.OwnerKind, r.OwnerID)
	}
	for _, l := range g.LogLines {
		add("component", l.OwnerID)
	}
	if f := g.Fanned; f != nil {
		for _, r := range f.keyed() {
			add(r.ownerKind, r.owner)
		}
	}
	return out
}

// preloadArcs resolves, in one statement, the primary key of every owner the
// batch names by id (and every node it names by name) that the batch has not
// resolved yet, so a tick of many groups, each on its own component, costs
// one read for its owners however many groups it carries (#1582). An owner
// the read does not find is left to ownerArcValue, which names the miss.
// selfNode is the node whose self-report the batch carries (#1625), "" for
// none: the same read resolves its platform component (nodeSelfArc).
func (p *PG) preloadArcs(ctx context.Context, bt *batchTx, groups []landGroup, nodeLogs []NodeLogWrite, selfNode string) error {
	ids := map[string][]string{}
	var nodeNames, selfNodes []string
	if _, ok := bt.arcs[nodeSelfArc(selfNode)]; selfNode != "" && !ok {
		selfNodes = append(selfNodes, selfNode)
	}
	seen := map[string]bool{}
	want := func(kind, ref string) {
		k := kind + "\x00" + ref
		if ref == "" || seen[k] {
			return
		}
		seen[k] = true
		if _, ok := bt.arcs[k]; ok {
			return
		}
		switch {
		case isUUID(ref) && (kind == "component" || kind == "system" || kind == "location" || kind == "node"):
			ids[kind] = append(ids[kind], ref)
		case kind == "node":
			nodeNames = append(nodeNames, ref)
		}
	}
	for _, g := range groups {
		for _, r := range g.ownerRefs() {
			want(r[0], r[1])
		}
	}
	for _, l := range nodeLogs {
		want("node", l.Node)
	}
	if len(ids)+len(nodeNames)+len(selfNodes) == 0 {
		return nil
	}
	rows, err := bt.Query(ctx, `select 'component', id::text, id::text from component where id = any($1::uuid[])
		union all select 'system', id::text, id::text from system where id = any($2::uuid[])
		union all select 'location', id::text, id::text from location where id = any($3::uuid[])
		union all select 'node', principal_id::text, principal_id::text from node where principal_id = any($4::uuid[])
		union all select 'node', name, principal_id::text from node where name = any($5::text[])
		union all select 'platform', name, platform_component_id::text from node
			where name = any($6::text[]) and platform_component_id is not null`,
		ids["component"], ids["system"], ids["location"], ids["node"], nodeNames, selfNodes)
	if err != nil {
		return fmt.Errorf("storage: land batch: resolve owners: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, ref, arc string
		if err := rows.Scan(&kind, &ref, &arc); err != nil {
			return fmt.Errorf("storage: land batch: resolve owners: %w", err)
		}
		bt.arcs[kind+"\x00"+ref] = arc
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: land batch: resolve owners: %w", err)
	}
	return nil
}

// fanGroups is the fanned rows' fence, read once for every group: which of
// their owners each group's conduit has bound, in one statement however many
// groups and conduits, held on a copy of each group's fan-out writer (the
// caller's rows are untouched) for every lane, and each bound owner's arc
// known without a read of its own (a binding's owner exists, by its foreign
// key). A row whose owner is no longer bound (an operator unbound it, or
// deleted the owner, after the ingest resolved it) is stale, not refused:
// refusing it would drop the conduit's own rows and every other owner's with
// it. The batch fails transiently, writing nothing, and its redelivery
// resolves the bindings afresh.
func fanGroups(ctx context.Context, bt *batchTx, groups []landGroup) error {
	var conduits, owners []string
	for _, g := range groups {
		if g.Fanned == nil || !isUUID(g.Fanned.Writer.Component) {
			continue
		}
		for _, r := range g.Fanned.keyed() {
			if isUUID(r.owner) {
				conduits, owners = append(conduits, g.Fanned.Writer.Component), append(owners, r.owner)
			}
		}
	}
	bound := map[string]map[string]bool{}
	if len(conduits) > 0 {
		rows, err := bt.Query(ctx, `select b.conduit_id::text, b.owner_kind, coalesce(b.component_id, b.system_id)::text
			from identity_binding b join unnest($1::uuid[], $2::uuid[]) as t(conduit, owner)
			on b.conduit_id = t.conduit and coalesce(b.component_id, b.system_id) = t.owner`, conduits, owners)
		if err != nil {
			return fmt.Errorf("storage: read the conduit's bindings: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var conduit, kind, id string
			if err := rows.Scan(&conduit, &kind, &id); err != nil {
				return fmt.Errorf("storage: read the conduit's bindings: %w", err)
			}
			if bound[conduit] == nil {
				bound[conduit] = map[string]bool{}
			}
			bound[conduit][fanOwnerKey(kind, id)] = true
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("storage: read the conduit's bindings: %w", err)
		}
	}
	for i, g := range groups {
		if g.Fanned == nil {
			continue
		}
		f := *g.Fanned
		got := bound[f.Writer.Component]
		if got == nil {
			got = map[string]bool{}
		}
		f.Writer.fanOwners = got
		for _, r := range f.keyed() {
			if !got[fanOwnerKey(r.ownerKind, r.owner)] {
				return fmt.Errorf("%w: %s/%s", ErrFanoutStale, r.ownerKind, r.owner)
			}
		}
		for k := range got {
			bt.arcs[k] = k[strings.IndexByte(k, 0)+1:]
		}
		groups[i].Fanned = &f
	}
	return nil
}

// primeKeyWriters reads the writer class of every key the batch's groups
// carry, own rows and fanned ones, in one statement per lane, into the
// batch's memo (batchTx.writers), so admitGroup and the lanes answer from it:
// a tick whose groups each bring keys of their own (many drivers, each its own
// metric and property names) costs what a tick of one group does (#1582).
func primeKeyWriters(ctx context.Context, bt *batchTx, groups []landGroup) error {
	var metrics, props []string
	for _, g := range groups {
		sets := []IngestGroup{g.IngestGroup}
		if f := g.Fanned; f != nil {
			sets = append(sets, IngestGroup{Metrics: f.Metrics, Properties: f.Properties})
		}
		for _, s := range sets {
			for _, r := range metricKeyed(s.Metrics) {
				metrics = append(metrics, r.key)
			}
			for _, r := range propertyKeyed(s.Properties) {
				props = append(props, r.key)
			}
		}
	}
	for _, l := range []struct {
		lane writeLane
		keys []string
	}{{writeMetric, metrics}, {writeProperty, props}} {
		if len(l.keys) == 0 {
			continue
		}
		if _, err := keyWriters(ctx, bt, l.lane, l.keys); err != nil {
			return err
		}
	}
	return nil
}

// admitGroup holds every row of one group to its writer before anything of
// the batch lands: each owner resolved, each key's writer class and each
// node's binding checked (admitWrites), each log line on a component. The
// lanes check them again as they write, from the batch's memos, at no cost; a
// refusal here drops the group before its rows could fail its siblings'.
func (p *PG) admitGroup(ctx context.Context, tx pgx.Tx, g landGroup) error {
	type lane struct {
		lane writeLane
		what string
		w    KeyWriter
		rows []keyedWrite
	}
	lanes := []lane{
		{writeProperty, "property sample", g.Writer, propertyKeyed(g.Properties)},
		{writeMetric, "sample", g.Writer, metricKeyed(g.Metrics)},
		{writeEvent, "event", g.Writer, eventKeyed(g.Events)},
	}
	if f := g.Fanned; f != nil {
		lanes = append(lanes,
			lane{writeProperty, "property sample", f.Writer, propertyKeyed(f.Properties)},
			lane{writeMetric, "sample", f.Writer, metricKeyed(f.Metrics)},
			lane{writeEvent, "event", f.Writer, eventKeyed(f.Events)})
	}
	for _, l := range lanes {
		if len(l.rows) == 0 {
			continue
		}
		for i, r := range l.rows {
			if _, err := ownerColumn(r.ownerKind); err != nil {
				return fmt.Errorf("storage: %s %s/%s: %w", l.what, r.owner, r.key, err)
			}
			arc, err := p.ownerArcValue(ctx, tx, r.ownerKind, r.owner)
			if err != nil {
				return fmt.Errorf("storage: %s %s/%s: %w", l.what, r.owner, r.key, err)
			}
			l.rows[i].owner = arc
		}
		if err := admitWrites(ctx, tx, l.lane, l.w, l.rows); err != nil {
			return err
		}
	}
	for _, ln := range g.LogLines {
		if ln.OwnerKind != "component" {
			return fmt.Errorf("storage: log line %s: log_line is component-owned (#589), refusing owner_kind %q (a node's self-logs land in node_log)", ln.OwnerID, ln.OwnerKind)
		}
		if _, err := p.ownerArcValue(ctx, tx, "component", ln.OwnerID); err != nil {
			return fmt.Errorf("storage: log line %s: %w", ln.OwnerID, err)
		}
	}
	return nil
}

// propertyKeyed is property rows as the writer check reads them, their text
// cleaned as the lane cleans it.
func propertyKeyed(evs []PropertySampleWrite) []keyedWrite {
	out := make([]keyedWrite, 0, len(evs))
	for _, ev := range cleanPropertyWrites(evs) {
		out = append(out, keyedWrite{key: ev.Key, instance: ev.Instance, ownerKind: ev.OwnerKind, owner: ev.OwnerID})
	}
	return out
}

// metricKeyed is metric rows as the writer check reads them.
func metricKeyed(evs []MetricSampleWrite) []keyedWrite {
	out := make([]keyedWrite, 0, len(evs))
	for _, ev := range cleanMetricWrites(evs) {
		out = append(out, keyedWrite{key: ev.Key, instance: ev.Instance, ownerKind: ev.OwnerKind, owner: ev.OwnerID})
	}
	return out
}

// eventKeyed is event rows as the writer check reads them.
func eventKeyed(evs []EventWrite) []keyedWrite {
	out := make([]keyedWrite, 0, len(evs))
	for _, ev := range cleanEventWrites(evs) {
		out = append(out, keyedWrite{key: ev.Key, instance: ev.Instance, ownerKind: ev.OwnerKind, owner: ev.OwnerID})
	}
	return out
}

// byWriteIdentity splits sets by the row identity their writers stamp
// (writeIdentity), in the order each identity first appears: each part is
// one lane writer call. Pure.
func byWriteIdentity[S any](sets []S, writer func(S) KeyWriter) [][]S {
	at := map[string]int{}
	var out [][]S
	for _, s := range sets {
		id := writer(s).writeIdentity()
		i, ok := at[id]
		if !ok {
			i = len(out)
			at[id] = i
			out = append(out, nil)
		}
		out[i] = append(out[i], s)
	}
	return out
}

// mergeUnbound folds rows naming one identity (conduit, key and value) into
// one, in the order each first appears: counts summed, the first and last
// heard the earliest and latest, the carried keys their union. One upsert
// cannot touch a row twice, and a tick's groups can each hear the same
// unbound identity. Pure.
func mergeUnbound(us []UnboundIdentityWrite) []UnboundIdentityWrite {
	at := map[[3]string]int{}
	out := make([]UnboundIdentityWrite, 0, len(us))
	for _, u := range us {
		k := [3]string{u.ConduitID, u.Key, u.Value}
		i, ok := at[k]
		if !ok {
			at[k] = len(out)
			u.Keys = slices.Clone(u.Keys)
			out = append(out, u)
			continue
		}
		m := &out[i]
		m.Count += u.Count
		m.FirstAt = earliest(m.FirstAt, u.FirstAt)
		if u.LastAt.After(m.LastAt) {
			m.LastAt = u.LastAt
		}
		for _, key := range u.Keys {
			if !slices.Contains(m.Keys, key) {
				m.Keys = append(m.Keys, key)
			}
		}
	}
	return out
}

// earliest is the earlier of a and b, a zero time standing for neither.
func earliest(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}
