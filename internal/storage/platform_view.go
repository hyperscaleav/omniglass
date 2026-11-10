package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The platform projection (#1644): the read behind the Platform health page.
//
// The page answers one question, whether Omniglass itself is well, and it
// answers it from parts the fleet already has: every node and every server is
// a component of the platform system (#1625, #1627), and every figure on the
// page is a sample on one of those components. So this read invents no data;
// it gathers the running build, the platform verdict and each node's current
// figure per key (PlatformWindowKeys) into one body, in a fixed number of statements however many
// nodes there are.
//
// Scope is injected per tier, as FleetProjection does. node:read lets the
// caller see the rows at all, and only an all-scope read reaches a node
// (ListNodes). component:read is the figures, the verdicts and the server's
// identity: the platform components are unplaced, so only an all-scope read
// reaches them, and anything less leaves the figures out rather than zeroing
// them. system:read is the platform system's own verdict, null short of an
// all-scope read for the same reason.

// PlatformViewGroup is one titled run of columns in the node table.
type PlatformViewGroup struct {
	Name  string
	Label string
	Keys  []string
}

// PlatformViewGroups is the node table's columns, in order: the one place the
// page's key set is written. The page renders the groups the view returns and
// has no list of its own, so a key added here is a column there. Each key is
// a seeded platform metric (TestPlatformViewKeysAreSeeded).
var PlatformViewGroups = []PlatformViewGroup{
	{Name: "checkin", Label: "Checkin", Keys: []string{writerclass.CheckinAge}},
	{Name: "schedule", Label: "Schedule", Keys: []string{
		writerclass.ScheduleLateness, writerclass.TickOverrunCount, writerclass.ScheduleSkipCount,
	}},
	{Name: "commands", Label: "Commands", Keys: []string{
		writerclass.CommandPendingCount, writerclass.CommandPendingAge,
		writerclass.CommandStuckCount, writerclass.CommandExpiredCount,
	}},
}

// platformViewKeys is every key in PlatformViewGroups, in order.
func platformViewKeys() []string {
	var out []string
	for _, g := range PlatformViewGroups {
		out = append(out, g.Keys...)
	}
	return out
}

// PlatformServerKey is one key of a server panel. Of names the key it is read
// against, a pair the page prints as "a / b" (connections in use against the
// pool's size). Rank above zero makes it a ranked list: the view returns only
// the Rank highest instances of the key's newest window, highest first,
// sorted in SQL, so a large route table never reaches the browser. Window
// marks a key the server writes only for a report window that observed
// something (a request, a landed batch, a slow query): its last
// sample is that window's figure, not a current reading, so the view leaves
// it out once platformWindowHorizon has passed, as PlatformWindowKeys does
// for a node. Every other key is a gauge sampled each window or a retention
// fact its hourly pass writes, current however old.
type PlatformServerKey struct {
	Name   string
	Of     string
	Rank   int
	Window bool
}

// PlatformServerGroup is one panel of a server's figures.
type PlatformServerGroup struct {
	Name  string
	Label string
	Keys  []PlatformServerKey
}

// platformRouteRank is how many routes a ranked request key lists.
const platformRouteRank = 10

// PlatformServerGroups is each server's panels, in order (#1645): the one
// place their key set is written, as PlatformViewGroups is the node table's.
// The page renders the panels the view returns and has no list of its own.
// Each key is a seeded platform metric (TestPlatformServerGroupKeysAreSeeded).
// The request keys are ranked: the ten slowest and the ten busiest routes of
// the newest window. Every other split key (a retention lane,
// a table) reads back its latest sample per instance, a set as small as the
// code that names it.
var PlatformServerGroups = []PlatformServerGroup{
	{Name: "requests", Label: "Requests", Keys: []PlatformServerKey{
		{Name: writerclass.HTTPRequestDurationMax, Rank: platformRouteRank, Window: true},
		{Name: writerclass.HTTPRequestCount, Rank: platformRouteRank, Window: true},
		{Name: writerclass.DBSlowQueryCount, Window: true},
		{Name: writerclass.DBPoolAcquired, Of: writerclass.DBPoolMax},
		{Name: writerclass.DBPoolMax},
		{Name: writerclass.DBPoolWaitCount},
	}},
	{Name: "ingest", Label: "Ingest", Keys: []PlatformServerKey{
		{Name: writerclass.IngestLag, Window: true},
		{Name: writerclass.IngestLagMax, Window: true},
		{Name: writerclass.IngestBatchCount, Window: true},
		{Name: writerclass.IngestSampleCount, Window: true},
	}},
	{Name: "queues", Label: "Queues", Keys: []PlatformServerKey{
		{Name: writerclass.BusConsumerPending},
		{Name: writerclass.BusConsumerAckPending},
		{Name: writerclass.CalcPendingDepth},
		{Name: writerclass.OutboxQueuedDepth},
		{Name: writerclass.OutboxDueAge},
		{Name: writerclass.IngestLaneBusy},
	}},
	{Name: "retention", Label: "Retention", Keys: []PlatformServerKey{
		{Name: writerclass.RetentionBehind},
		{Name: writerclass.RetentionDeletedCount},
		{Name: writerclass.MetricOldestDayAge, Of: writerclass.RetentionWindowDays},
		{Name: writerclass.RetentionWindowDays},
		{Name: writerclass.DatabaseBytes},
		{Name: writerclass.TableBytes},
	}},
}

// platformServerKeys is every key in PlatformServerGroups, in order, with
// each one's rank and whether it is a window figure.
func platformServerKeys() (names []string, ranks []int, windows []bool) {
	for _, g := range PlatformServerGroups {
		for _, k := range g.Keys {
			names = append(names, k.Name)
			ranks = append(ranks, k.Rank)
			windows = append(windows, k.Window)
		}
	}
	return names, ranks, windows
}

// PlatformKey is one column's catalog facts: its metric type's label, unit
// and description, the tooltip the page shows (ADR-0127). Read from the
// catalog so no prose about a key is written twice.
type PlatformKey struct {
	Name        string
	Label       string
	Unit        string
	Description string
}

// PlatformFigure is a key's current value on one component and when it was
// observed: a gauge's latest sample, or a window count's newest window summed
// over the instances written in it.
type PlatformFigure struct {
	Value      float64
	ObservedAt time.Time
}

// PlatformInstanceFigure is one (key, instance) figure on a server panel: the
// latest sample of that series, unsummed, so a split key reads per instance.
type PlatformInstanceFigure struct {
	Key        string
	Instance   string
	Value      float64
	ObservedAt time.Time
}

// PlatformPanelKey is a server panel key with its catalog facts.
type PlatformPanelKey struct {
	PlatformKey
	Of   string
	Rank int
}

// PlatformPanel is one server group as one server reads it: its keys' catalog
// facts and its figures, in key order and then, for a ranked key, highest
// first, otherwise by instance. A key never sampled has no figure.
type PlatformPanel struct {
	Name    string
	Label   string
	Keys    []PlatformPanelKey
	Figures []PlatformInstanceFigure
}

// PlatformAlarm is an open alarm on a server's platform component: the rule
// that raised it and the key that rule samples (Key nil for a hand-raised
// alarm or one whose rule is gone), so the page flags the panel holding that
// key from the rule's own verdict rather than a threshold of its own.
type PlatformAlarm struct {
	Rule     string
	Key      *string
	Severity string
	Message  string
	RaisedAt time.Time
}

// PlatformSystem is the platform system and its recorded verdict.
type PlatformSystem struct {
	ID      string
	Name    string
	Verdict string
}

// PlatformServer is one server's platform component and the identity its
// boot wrote: the build it runs and when it started. Version and StartedAt
// are nil before a boot has written them.
type PlatformServer struct {
	ComponentID string
	Name        string
	Version     *string
	StartedAt   *time.Time
	Verdict     string
	Groups      []PlatformPanel
	Alarms      []PlatformAlarm
}

// PlatformNode is one node's row. ComponentID is nil for a node the boot has
// not yet given a platform component. Verdict and Figures are the
// component's, nil without an all-scope component read; a key the node has
// never reported is absent from Figures, never zero.
type PlatformNode struct {
	Name            string
	Label           string
	Enrolled        bool
	LastHeartbeatAt *time.Time
	ComponentID     *string
	Verdict         *string
	Figures         map[string]PlatformFigure
}

// PlatformView is the whole projection. FiguresRead says whether the caller's
// component read reached the platform components: false means Servers is
// empty and every node's Verdict and Figures are nil because the caller may
// not read them, not because nothing was reported.
type PlatformView struct {
	System      *PlatformSystem
	Servers     []PlatformServer
	Groups      []PlatformViewGroup
	Keys        []PlatformKey
	Nodes       []PlatformNode
	FiguresRead bool
}

// PlatformView reads the platform as the Platform health page shows it. A
// node read short of all-scope is ErrNodeForbidden, as ListNodes answers.
func (p *PG) PlatformView(ctx context.Context, nodeRead, sysRead, compRead scope.Set) (*PlatformView, error) {
	nodes, err := p.ListNodes(ctx, nodeRead)
	if err != nil {
		return nil, err
	}
	view := &PlatformView{
		Servers: []PlatformServer{},
		Groups:  PlatformViewGroups,
		Nodes:   make([]PlatformNode, 0, len(nodes)),
	}
	if view.Keys, err = p.platformKeys(ctx); err != nil {
		return nil, err
	}
	var componentIDs []string
	for _, n := range nodes {
		view.Nodes = append(view.Nodes, PlatformNode{
			Name: n.Name, Label: n.Label, Enrolled: n.Enrolled,
			LastHeartbeatAt: n.LastHeartbeatAt, ComponentID: n.PlatformComponentID,
		})
		if n.PlatformComponentID != nil {
			componentIDs = append(componentIDs, *n.PlatformComponentID)
		}
	}
	if sysRead.All {
		if view.System, err = p.platformSystem(ctx); err != nil {
			return nil, err
		}
	}
	if !compRead.All {
		return view, nil
	}
	view.FiguresRead = true
	if view.Servers, err = p.platformServers(ctx); err != nil {
		return nil, err
	}
	if err := p.platformServerPanels(ctx, view.Servers); err != nil {
		return nil, err
	}
	verdicts, err := p.componentVerdicts(ctx, componentIDs)
	if err != nil {
		return nil, err
	}
	figures, err := p.platformFigures(ctx, componentIDs)
	if err != nil {
		return nil, err
	}
	for i := range view.Nodes {
		id := view.Nodes[i].ComponentID
		if id == nil {
			continue
		}
		if v, ok := verdicts[*id]; ok {
			view.Nodes[i].Verdict = &v
		}
		view.Nodes[i].Figures = figures[*id]
		if view.Nodes[i].Figures == nil {
			view.Nodes[i].Figures = map[string]PlatformFigure{}
		}
	}
	return view, nil
}

// platformKeys reads the catalog facts of every key in PlatformViewGroups, in
// group order. A key the catalog does not hold is left out.
func (p *PG) platformKeys(ctx context.Context) ([]PlatformKey, error) {
	return p.catalogKeys(ctx, platformViewKeys())
}

// catalogKeys reads the catalog facts of each named platform metric, in the
// order given. A key the catalog does not hold is left out.
func (p *PG) catalogKeys(ctx context.Context, names []string) ([]PlatformKey, error) {
	rows, err := p.pool.Query(ctx, `
		select mt.name, coalesce(mt.label, ''), coalesce(mt.unit, ''), coalesce(mt.description, '')
		from unnest($1::text[]) with ordinality as k(name, ord)
		join metric_type mt on mt.namespace = 'og' and mt.name = k.name
		order by k.ord`, names)
	if err != nil {
		return nil, fmt.Errorf("storage: platform view keys: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PlatformKey, error) {
		var k PlatformKey
		err := r.Scan(&k.Name, &k.Label, &k.Unit, &k.Description)
		return k, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: platform view keys: %w", err)
	}
	return out, nil
}

// platformSystem reads the platform system and its verdict, nil on a database
// the boot seed has not reached.
func (p *PG) platformSystem(ctx context.Context) (*PlatformSystem, error) {
	var s PlatformSystem
	err := p.pool.QueryRow(ctx, `
		select system.id::text, system.name, `+latestVerdict("system_id", "system")+`
		from system join system_type st on st.id = system.system_type_id
		where st.namespace = 'og' and st.name = $1`, platformTypeName).Scan(&s.ID, &s.Name, &s.Verdict)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: platform view system: %w", err)
	}
	return &s, nil
}

// latestIdentity is a server component's newest observed value of one of its
// boot's identity properties, as text.
func latestIdentity(key string) string {
	return `(select v.value #>> '{}' from property v
		where v.component_id = component.id
		  and v.property_type_id = (select id from property_type where namespace = 'og' and name = '` + key + `')
		  and v.instance = '' and v.provenance = 'observed'
		order by v.id desc limit 1)`
}

// platformServers reads every server's platform component, its identity and
// its verdict, in name order.
func (p *PG) platformServers(ctx context.Context) ([]PlatformServer, error) {
	rows, err := p.pool.Query(ctx, `
		select component.id::text, component.name,
		       `+latestIdentity(writerclass.ServerVersion)+`,
		       `+latestIdentity(writerclass.ServerStartedAt)+`,
		       `+latestVerdict("component_id", "component")+`
		from platform_server ps join component on component.id = ps.platform_component_id
		order by component.name`)
	if err != nil {
		return nil, fmt.Errorf("storage: platform view servers: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PlatformServer, error) {
		var (
			s       PlatformServer
			started *string
		)
		if err := r.Scan(&s.ComponentID, &s.Name, &s.Version, &started, &s.Verdict); err != nil {
			return s, err
		}
		if started != nil {
			if t, err := time.Parse(time.RFC3339Nano, *started); err == nil {
				s.StartedAt = &t
			}
		}
		return s, nil
	})
	if err != nil {
		return nil, fmt.Errorf("storage: platform view servers: %w", err)
	}
	return out, nil
}

// componentVerdicts reads the recorded verdict of each component in ids.
func (p *PG) componentVerdicts(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx, `
		select component.id::text, `+latestVerdict("component_id", "component")+`
		from component where component.id = any($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("storage: platform view verdicts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, v string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, fmt.Errorf("storage: platform view verdicts: %w", err)
		}
		out[id] = v
	}
	return out, rows.Err()
}

// PlatformWindowKeys are the PlatformViewGroups keys a writer reports once a
// window and only when nonzero. Such a key's last sample is the last window
// that counted something, not a current reading: a later window that counted
// nothing writes no row. So the view shows one only while it is the newest
// window's and leaves it absent after (platformWindowHorizon). Every other key
// is written whether or not it moved, whose latest sample is current however
// old. Since the 2026-11-10 ruling every windowed platform count is written
// every window, zero when quiet (ADR-1625), so no grouped key is one and the
// set is empty; it stays as the guard for a count written only when nonzero.
// The catalog describes exactly these as "reported only when nonzero"
// (TestPlatformWindowKeysAreTheNonzeroCounts).
var PlatformWindowKeys = map[string]bool{}

// platformWindowHorizon is how far back a window count still reads as the
// current window: two report windows, so the newest window has ended and a
// later one has too. The command pass's window (15 s) is shorter than the
// shortest report window doubled (2 x 10 s), so the same horizon holds it.
func platformWindowHorizon(reportSeconds int) time.Duration {
	return 2 * time.Duration(reportSeconds) * time.Second
}

// platformFigures reads, in one statement, the current figure of every
// PlatformViewGroups key on every component in ids. Per series instance it
// takes the newest row (metricNewestSQL, the read EffectiveMetrics makes)
// across the provenances that report reality (observed, calculated); an
// intended row is what a command asked for, never a figure. A key's figure is
// then the sum of its instances written at its newest time: a key split by
// instance (command-expired-count's undelivered and unexecuted) is one count
// written at one pass, and an instance from an earlier pass is not this
// one's. A window count past the horizon is left out (PlatformWindowKeys).
func (p *PG) platformFigures(ctx context.Context, ids []string) (map[string]map[string]PlatformFigure, error) {
	out := map[string]map[string]PlatformFigure{}
	if len(ids) == 0 {
		return out, nil
	}
	collection, err := p.CollectionSettings(ctx)
	if err != nil {
		return nil, err
	}
	// Never nil: a nil slice binds as NULL, and "name = any(NULL)" would
	// drop every row older than the horizon, gauges too.
	windowKeys := []string{}
	for _, k := range platformViewKeys() {
		if PlatformWindowKeys[k] {
			windowKeys = append(windowKeys, k)
		}
	}
	rows, err := p.pool.Query(ctx, `
		with latest as (
			select distinct on (s.component_id, mt.name, s.instance)
			       s.component_id, mt.name, n.value, n.ts
			from metric_series s
			join metric_type mt on mt.id = s.metric_type_id
			cross join lateral (`+metricNewestSQL("s.id")+`) n
			where s.owner_kind = 'component' and s.component_id = any($1::uuid[])
			  and mt.namespace = 'og' and mt.name = any($2::text[])
			  and s.provenance in ('observed', 'calculated')
			order by s.component_id, mt.name, s.instance, n.ts desc, n.id desc
		), ranked as (
			select *, max(ts) over (partition by component_id, name) as newest from latest
		)
		select component_id::text, name, sum(value), newest
		from ranked
		where ts = newest
		  and not (name = any($3::text[]) and newest < now() - $4::interval)
		group by component_id, name, newest`,
		ids, platformViewKeys(), windowKeys, platformWindowHorizon(collection.ReportSeconds))
	if err != nil {
		return nil, fmt.Errorf("storage: platform view figures: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, key string
			f       PlatformFigure
		)
		if err := rows.Scan(&id, &key, &f.Value, &f.ObservedAt); err != nil {
			return nil, fmt.Errorf("storage: platform view figures: %w", err)
		}
		if out[id] == nil {
			out[id] = map[string]PlatformFigure{}
		}
		out[id][key] = f
	}
	return out, rows.Err()
}

// platformServerPanels fills each server's Groups and Alarms (#1645) in three
// statements however many servers there are: the catalog facts of every
// server key, every server's figures, and every server's open alarms.
func (p *PG) platformServerPanels(ctx context.Context, servers []PlatformServer) error {
	names, ranks, windows := platformServerKeys()
	catalog, err := p.catalogKeys(ctx, names)
	if err != nil {
		return err
	}
	facts := make(map[string]PlatformKey, len(catalog))
	for _, k := range catalog {
		facts[k.Name] = k
	}
	ids := make([]string, len(servers))
	for i, s := range servers {
		ids[i] = s.ComponentID
	}
	figures, err := p.serverFigures(ctx, ids, names, ranks, windows)
	if err != nil {
		return err
	}
	alarms, err := p.serverAlarms(ctx, ids)
	if err != nil {
		return err
	}
	for i := range servers {
		id := servers[i].ComponentID
		servers[i].Groups = make([]PlatformPanel, 0, len(PlatformServerGroups))
		for _, g := range PlatformServerGroups {
			panel := PlatformPanel{Name: g.Name, Label: g.Label, Figures: []PlatformInstanceFigure{}}
			for _, k := range g.Keys {
				f, ok := facts[k.Name]
				if !ok {
					continue
				}
				panel.Keys = append(panel.Keys, PlatformPanelKey{PlatformKey: f, Of: k.Of, Rank: k.Rank})
				panel.Figures = append(panel.Figures, figures[id][k.Name]...)
			}
			servers[i].Groups = append(servers[i].Groups, panel)
		}
		servers[i].Alarms = alarms[id]
		if servers[i].Alarms == nil {
			servers[i].Alarms = []PlatformAlarm{}
		}
	}
	return nil
}

// serverFigures reads, in one statement, the latest sample of every
// (key, instance) series of the named keys on every component in ids, across
// the provenances that report reality, as platformFigures does but unsummed.
// A ranked key (rank above zero) keeps only the instances written at its
// newest time, the newest window, cut to its rank highest first; the cut
// and the order are the database's, so the page ranks nothing. Every other
// key is returned in instance order. A window key's (PlatformServerKey.Window)
// sample older than platformWindowHorizon is left out, instance by instance:
// its window is over and the ones since observed nothing on that instance,
// so an instance last seen an hour ago is not read beside this window's.
func (p *PG) serverFigures(ctx context.Context, ids, names []string, ranks []int, windows []bool) (map[string]map[string][]PlatformInstanceFigure, error) {
	out := map[string]map[string][]PlatformInstanceFigure{}
	if len(ids) == 0 {
		return out, nil
	}
	collection, err := p.CollectionSettings(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		with k as (
			select * from unnest($2::text[], $3::int[], $4::bool[]) as k(name, rank, win)
		), latest as (
			select distinct on (s.component_id, mt.name, s.instance)
			       s.component_id, mt.name, s.instance, n.value, n.ts
			from metric_series s
			join metric_type mt on mt.id = s.metric_type_id
			cross join lateral (`+metricNewestSQL("s.id")+`) n
			where s.owner_kind = 'component' and s.component_id = any($1::uuid[])
			  and mt.namespace = 'og' and mt.name = any($2::text[])
			  and s.provenance in ('observed', 'calculated')
			order by s.component_id, mt.name, s.instance, n.ts desc, n.id desc
		), placed as (
			select l.*, k.rank, k.win, max(l.ts) over (partition by l.component_id, l.name) as newest
			from latest l join k on k.name = l.name
		), ordered as (
			select p.*, row_number() over (
				partition by p.component_id, p.name
				order by (p.ts = p.newest) desc, p.value desc, p.instance) as rn
			from placed p
		)
		select component_id::text, name, instance, value, ts
		from ordered
		where (rank = 0 or (ts = newest and rn <= rank))
		  and not (win and ts < now() - $5::interval)
		order by component_id, name, case when rank > 0 then rn end, instance`,
		ids, names, ranks, windows, platformWindowHorizon(collection.ReportSeconds))
	if err != nil {
		return nil, fmt.Errorf("storage: platform view server figures: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id string
			f  PlatformInstanceFigure
		)
		if err := rows.Scan(&id, &f.Key, &f.Instance, &f.Value, &f.ObservedAt); err != nil {
			return nil, fmt.Errorf("storage: platform view server figures: %w", err)
		}
		if out[id] == nil {
			out[id] = map[string][]PlatformInstanceFigure{}
		}
		out[id][f.Key] = append(out[id][f.Key], f)
	}
	return out, rows.Err()
}

// serverAlarms reads the open alarms on every component in ids, newest first,
// each with the rule that raised it and the key that rule samples.
func (p *PG) serverAlarms(ctx context.Context, ids []string) (map[string][]PlatformAlarm, error) {
	out := map[string][]PlatformAlarm{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx, `
		select a.component_id::text, coalesce(r.name, ''), r.sample_key, a.severity, a.message, a.raised_at
		from alarm a left join event_rule r on r.id = a.raised_by_rule_id
		where a.component_id = any($1::uuid[]) and a.cleared_at is null
		order by a.raised_at desc, a.id desc`, ids)
	if err != nil {
		return nil, fmt.Errorf("storage: platform view server alarms: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id string
			a  PlatformAlarm
		)
		if err := rows.Scan(&id, &a.Rule, &a.Key, &a.Severity, &a.Message, &a.RaisedAt); err != nil {
			return nil, fmt.Errorf("storage: platform view server alarms: %w", err)
		}
		out[id] = append(out[id], a)
	}
	return out, rows.Err()
}
