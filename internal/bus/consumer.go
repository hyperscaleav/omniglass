package bus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/key"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// telemetryStream is the JetStream stream capturing every node's telemetry
// publish; telemetryConsumer is the server's durable, at-least-once worker over
// it. The durable consumer IS the ingest worklist (no separate Postgres queue in
// this checkpoint): its handler derives, confines, writes, and acks inline.
//
// maxTelemetryDeliveries bounds how many times a single TelemetryBatch is
// redelivered before nakOrTerm gives up on it: without a bound, a
// permanently-failing message (a row the handler will never manage to write)
// would redeliver forever, the mirror problem to the stream's own retention
// (see startTelemetryConsumer).
//
// telemetryDuplicateWindow is how long the stream remembers a batch's
// Nats-Msg-Id (#453): a publisher retrying inside it (a node's next tick, the
// push route's bounded retry) is answered with the first copy's ack and
// nothing is stored twice. A retry past it is stored again, and the receipt
// (#311, LandBatch) keeps it from landing twice, so the window saves work and
// the receipt is the guarantee. ReceiptHorizon is how long a receipt is kept
// far past the stream's redelivery (maxTelemetryDeliveries acks) and a
// node's retry of a batch it could not ship, ADR-0311.
const (
	telemetryStream          = "OG_TELEMETRY"
	telemetryConsumer        = "og-telemetry-worker"
	maxTelemetryDeliveries   = 5
	telemetryDuplicateWindow = 5 * time.Minute
	ReceiptHorizon           = 24 * time.Hour
)

// telemetryAckWait is how long the stream waits for the consumer's ack
// before it redelivers: a consumer that died with a batch unacked hands it
// to the next one after this. A var so a test can shorten it.
var telemetryAckWait = 30 * time.Second

// telemetryNakDelay is the base of the backoff a transiently failed batch is
// redelivered after (the delay grows with each delivery). A var so a test can
// shorten it.
var telemetryNakDelay = 500 * time.Millisecond

// startTelemetryConsumer creates (idempotently) the telemetry stream and its
// durables over the full-permission internal client and begins consuming. The
// stream persists a TelemetryBatch the instant a node publishes it, and (WorkQueuePolicy)
// deletes it the instant it is acked, so disk stays bounded to the current
// backlog rather than growing forever; each durable redelivers a transient
// failure up to maxTelemetryDeliveries (nakOrTerm), so a DB hiccup never loses a
// sample but a permanently-failing one still leaves the queue.
//
// The stream files each node's batches under the node's partition among
// s.partitions (#1896, partitionTransform), and each partition has a durable
// of its own; the durable named telemetryConsumer keeps the push lane's
// subject and any batch stored under a node's own subject before the stream
// filed by partition. A partition past the count, filed under by a server
// that ran more lanes or left with a durable, is drained until empty and its
// durable then deleted. A count changed over a held backlog is logged: a
// node's batches filed before and after the change are on different lanes
// and may land out of order (reorderRisk).
//
// The stream keeps a duplicate window over each batch's Nats-Msg-Id (#453),
// and the stream's creation time is recorded as the epoch a batch with no id
// is keyed under (batchID).
func (s *Server) startTelemetryConsumer() error {
	js, err := jetstream.New(s.nc)
	if err != nil {
		return err
	}
	s.js = js
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transform := partitionTransform(s.partitions)
	prior, held, err := priorPartitions(ctx, js)
	if err != nil {
		return err
	}
	if reorderRisk(prior, s.partitions, held) {
		slog.Warn("ingest partitions changed over a held backlog: a node's batches filed before and after may land out of order",
			"facility", "ingest", "prior", prior, "partitions", s.partitions, "held", held)
	}
	// The stream also names the partitioned form among its subjects, so the
	// durable that keeps the unpartitioned subjects is filtered to a strict
	// subset of them. A durable whose filters equal the stream's subjects
	// counts as unfiltered, and the work queue then holds every partition
	// batch for it after its own partition acknowledged it. No node can
	// publish the partitioned form: a node's grant is its own subject alone.
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:             telemetryStream,
		Subjects:         []string{collection.TelemetryWildcard, telemetryPrefix + "*.*", collection.APITelemetrySubject},
		Retention:        jetstream.WorkQueuePolicy,
		Duplicates:       telemetryDuplicateWindow,
		SubjectTransform: &transform,
	})
	if err != nil {
		return err
	}
	s.streamEpoch = stream.CachedInfo().Created.UTC().Format(time.RFC3339Nano)
	s.telemetry = stream

	// The durable that keeps the unpartitioned subjects is filtered first: a
	// work queue refuses two durables whose filters overlap, and one an older
	// server created takes every subject.
	other := s.tallies[s.partitions]
	if err := s.consumeLane(ctx, stream, telemetryConsumer, []string{collection.TelemetryWildcard, collection.APITelemetrySubject}, other); err != nil {
		return err
	}
	for i := range s.partitions {
		if err := s.consumeLane(ctx, stream, partitionDurable(i), []string{partitionFilter(i)}, s.tallies[i]); err != nil {
			return err
		}
	}
	return s.drainPastPartitions(ctx, stream, prior, other)
}

// priorPartitions is the partition count the telemetry stream filed by
// before this start, 0 for none, and how many batches it holds; -1 when
// there is no stream yet.
func priorPartitions(ctx context.Context, js jetstream.JetStream) (int, uint64, error) {
	stream, err := js.Stream(ctx, telemetryStream)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		return -1, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read the telemetry stream: %w", err)
	}
	info := stream.CachedInfo()
	return transformPartitions(info.Config.SubjectTransform), info.State.Msgs, nil
}

// consumeLane creates (or updates) the durable name over filters and
// consumes it, each batch's time in the handler kept in tally.
func (s *Server) consumeLane(ctx context.Context, stream jetstream.Stream, name string, filters []string, tally *laneTally) error {
	cons, err := stream.CreateOrUpdateConsumer(ctx, laneConfig(name, filters))
	if err != nil {
		return fmt.Errorf("durable %s: %w", name, err)
	}
	return s.consume(name, cons, tally)
}

// laneConfig is a lane's durable name over filters.
func laneConfig(name string, filters []string) jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Durable:        name,
		FilterSubjects: filters,
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        telemetryAckWait,
		MaxDeliver:     maxTelemetryDeliveries,
		// One batch in flight: nothing is delivered past a batch the
		// durable has not acked or dropped, a redelivery included, so each
		// publisher's batches land in the order the stream stored them, and
		// a node's batches all share one durable. The property lane's
		// transition guard (a value equal to its series' latest is not
		// written) is right only in that order (ADR-0311, ADR-1896).
		MaxAckPending: 1,
	}
}

// consume runs cons's pull loop into the handler, as one of the server's
// lanes.
func (s *Server) consume(name string, cons jetstream.Consumer, tally *laneTally) error {
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		start := time.Now()
		s.handleTelemetry(msg)
		tally.observe(time.Since(start))
	})
	if err != nil {
		return fmt.Errorf("consume %s: %w", name, err)
	}
	s.lanesMu.Lock()
	s.lanes = append(s.lanes, &ingestLane{name: name, cons: cons, cc: cc})
	s.lanesMu.Unlock()
	return nil
}

// drainPastPartitions drains each partition past the count (pastPartitions:
// every one the prior count filed under, and every durable left past it):
// its durable, created again if it is gone, is deleted when it holds nothing
// and consumed while it still does, so a server started with fewer lanes
// lands what one with more left behind. The stream files no new batch under
// it, so it empties and a later start deletes it.
func (s *Server) drainPastPartitions(ctx context.Context, stream jetstream.Stream, prior int, tally *laneTally) error {
	names := stream.ConsumerNames(ctx)
	var durables []string
	for name := range names.Name() {
		durables = append(durables, name)
	}
	if err := names.Err(); err != nil {
		return fmt.Errorf("list telemetry durables: %w", err)
	}
	for _, i := range pastPartitions(s.partitions, prior, durables) {
		name := partitionDurable(i)
		cons, err := stream.CreateOrUpdateConsumer(ctx, laneConfig(name, []string{partitionFilter(i)}))
		if err != nil {
			return fmt.Errorf("durable %s: %w", name, err)
		}
		info, err := cons.Info(ctx)
		if err != nil {
			return fmt.Errorf("durable %s: %w", name, err)
		}
		if info.NumPending+uint64(info.NumAckPending) == 0 {
			if err := stream.DeleteConsumer(ctx, name); err != nil {
				return fmt.Errorf("delete durable %s: %w", name, err)
			}
			continue
		}
		slog.Info("ingest partition draining", "facility", "ingest", "durable", name,
			"pending", info.NumPending, "partitions", s.partitions)
		if err := s.consume(name, cons, tally); err != nil {
			return err
		}
	}
	return nil
}

// stopLanes stops every lane's pull loop.
func (s *Server) stopLanes() {
	s.lanesMu.Lock()
	defer s.lanesMu.Unlock()
	for _, l := range s.lanes {
		l.cc.Stop()
	}
	s.lanes = nil
}

// maxBatchIDLen bounds a Nats-Msg-Id the receipt keys verbatim; a longer one
// is keyed by its digest, so a publisher cannot make a receipt key the index
// refuses.
const maxBatchIDLen = 128

// batchID is a batch's idempotency key (#311): the subject it arrived on and
// its Nats-Msg-Id. The subject is the publisher's (a node's grant reaches only
// its own), so no publisher can claim another's id and suppress its batch. A
// batch with no id (a publisher from before #453) is keyed by its stream
// sequence under the stream's creation epoch, which holds across its own
// redeliveries though not across a publisher's retry; "" only when neither
// can be read, and the batch then lands with no receipt.
func (s *Server) batchID(msg jetstream.Msg) string {
	if id := msg.Headers().Get(jetstream.MsgIDHeader); id != "" {
		if len(id) > maxBatchIDLen {
			sum := sha256.Sum256([]byte(id))
			id = "sha256:" + hex.EncodeToString(sum[:])
		}
		return canonicalSubject(msg.Subject()) + " " + id
	}
	meta, err := msg.Metadata()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s seq:%s/%d", canonicalSubject(msg.Subject()), s.streamEpoch, meta.Sequence.Stream)
}

// ack confirms a batch to the stream and waits for the stream to take it
// (the double ack): a batch whose ack is lost anyway is redelivered after
// telemetryAckWait, and its receipt makes the second landing a no-op.
func (s *Server) ack(msg jetstream.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := msg.DoubleAck(ctx)
	s.metrics.failure(writerclass.BusTelemetryAckFailureCount, "", err != nil)
	if err != nil {
		slog.Warn("telemetry ack not confirmed, the batch will be redelivered and held by its receipt", "subject", msg.Subject(), "error", err)
	}
}

// handleTelemetry is the ingest handler for one telemetry Event: decode, bind +
// confine the owner, apply reject-not-project, write the surviving typed
// samples, and ack. The ack discipline is deliberate: a permanent condition
// (undecodable payload, or an orphan the confinement fence drops) is terminated /
// acked so it is not redelivered; only a transient failure (DB, registry read) is
// left for nakOrTerm, which redelivers (Nak) up to maxTelemetryDeliveries and then
// terminates the message so it does not loop forever.
func (s *Server) handleTelemetry(msg jetstream.Msg) {
	ctx := context.Background()
	subject := canonicalSubject(msg.Subject())

	var ev ogv1.TelemetryBatch
	if err := proto.Unmarshal(msg.Data(), &ev); err != nil {
		// No ledger can be read from it, so it records no receipt: its items
		// read as missing at the ledger's next read (#1641, ADR-1691); the
		// warning says why.
		slog.Warn(undecodableLog, "facility", "ingest", "subject", subject, "bytes", len(msg.Data()), "error", err.Error())
		s.drop(msg, nil) // undecodable: it will never succeed, stop redelivery
		return
	}
	id := s.batchID(msg)
	b := termed{id: id, ledger: ev.GetLedger()}

	// TRUST DECIDED BY SUBJECT, never by which fields are populated. The API lane
	// carries an owner the route already authorized; the node lane's owner is the
	// server's to resolve from the task's interface.
	if subject == collection.APITelemetrySubject {
		bind, ok := apiBinding(&ev)
		if !ok {
			s.drop(msg, delivery.BatchItems(&ev)) // malformed owner: it will never resolve, stop redelivery
			return
		}
		if err := s.land(ctx, id, &ev, bind); err != nil {
			s.failed(msg, b, delivery.BatchItems(&ev), err)
			return
		}
		s.ack(msg)
		return
	}

	// The node published only its own telemetry subject (per-node grant), so the
	// node extracted from the subject is trusted, the same trust the heartbeat sink
	// relies on.
	node := collection.NodeFromSubject(subject)
	b.node = node

	// A node does not get to assert its own owner. Populating owner on this lane is a
	// protocol error, not something to quietly ignore: ignoring it would leave a
	// field that looks authoritative and is not, so say so and stop redelivery.
	if ev.GetOwner() != nil {
		slog.Warn("node-lane batch asserted an owner, dropped", "node", node)
		s.term(msg, b, delivery.RefusedOwner, delivery.BatchItems(&ev))
		return
	}

	// A node's tick is its groups (#1582): every task-bound row rides in a
	// TaskGroup, so the batch's own task_id and sample lanes are the shape a
	// node no longer sends. Landing them would bind them to no task, and
	// ignoring them would drop data that looks delivered, so say so and stop
	// redelivery. The batch's self-logs are the only top-level rows it carries.
	if ev.GetTaskId() != "" || taskBound(&ev) > 0 {
		slog.Warn(topLevelTaskLog, "facility", "ingest", "node", node, "task", ev.GetTaskId())
		s.term(msg, b, delivery.RefusedTask, delivery.BatchItems(&ev))
		return
	}

	stamp, err := s.landNode(ctx, id, node, &ev)
	if err != nil {
		s.failed(msg, b, delivery.BatchItems(&ev), err) // a refused write is dropped; a transient failure redelivered (bounded)
		return
	}
	s.ack(msg)
	// Each landed group's endpoint delivered this tick, whatever the sinks
	// kept of it (#933), stamped in one write after the ack so it holds
	// nothing in flight. A failed stamp loses one tick's freshness, which the
	// next tick restamps; it is not worth a redelivery of the whole tick.
	if len(stamp) > 0 {
		if err := s.store.StampEndpointsSeen(ctx, stamp); err != nil {
			slog.Warn("endpoint seen stamp failed", "facility", "ingest", "node", node, "endpoints", len(stamp), "error", err.Error())
		}
	}
}

// undecodableLog is the warning for a telemetry message that does not
// decode, which is Term'd.
const undecodableLog = "telemetry batch undecodable, dropped"

// declaredShortLog and declaredOverLog are the warnings for a batch that
// decoded to fewer, or more, items of a kind than its ledger declared
// (#1641): the shortfall is counted missing, an excess only warned.
const (
	declaredShortLog = "batch decoded fewer items than its ledger declared, counted missing"
	declaredOverLog  = "batch decoded more items than its ledger declared"
)

// topLevelTaskLog is the warning for a node-lane batch that carries a task or
// samples outside its groups (#1582), which is Term'd.
const topLevelTaskLog = "node-lane batch carried task rows outside its groups, dropped"

// refusedGroupLog is the warning for one group of a node's tick the Storage
// Gateway refused (#1582): that group is dropped and its siblings land.
const refusedGroupLog = "task group refused, dropped; its siblings landed"

// nodeSelfRefusedLog is the warning for a node's self-report the Storage
// Gateway refused (#1625): the report drops and the tick's groups land.
const nodeSelfRefusedLog = "node-self report refused, dropped; the tick's groups landed"

// nodeSelfOwnerLog is the warning for a node-self block one of whose samples
// names an identity (#1625): the report is the node's own, never about an
// owner it names, so the block drops and the tick's groups land.
const nodeSelfOwnerLog = "node-self report named an identity, dropped; the tick's groups landed"

// nodeSelfReport is the tick's node-self block as the report the gateway
// lands on node's platform component (#1625), each sample's time resolved
// against the batch's. It is nil for a tick with no block. named reports a
// block one of whose samples names an identity: such a block is no report of
// the node's own, so it is nil too. The keys are not filtered here: the
// gateway's node-self writer refuses a key that is not the node's. Pure.
func nodeSelfReport(ev *ogv1.TelemetryBatch, node string) (r *storage.NodeSelfReport, named bool) {
	ms := ev.GetNodeSelf().GetMetrics()
	if len(ms) == 0 {
		return nil, false
	}
	r = &storage.NodeSelfReport{Node: node, Metrics: make([]storage.NodeSelfSample, 0, len(ms))}
	for _, m := range ms {
		if m.GetIdentity() != nil {
			return nil, true
		}
		r.Metrics = append(r.Metrics, storage.NodeSelfSample{Key: m.GetName(), Instance: m.GetInstance(),
			Value: m.GetValue(), TS: sampleTime(ev, m.GetTs())})
	}
	return r, false
}

// landNode lands one node tick (#1582): the node's self-logs, its self-report
// (#1625, on its platform component) and each task group under its own
// writer, in ONE LandBatch under the tick's receipt.
// Every group's task resolves in one read; a group whose task the node does
// not hold (another node's, unknown, a shared interface) is an orphan the
// fence drops (#1200) while its siblings land. A group the gateway refuses is
// warned and dropped, never the tick. It returns the endpoints to stamp seen:
// every resolved group's that was not refused. A returned error is the
// resolve's, the registry's or the batch's (failed decides what it means).
//
// The batch carries the node's ledger, and every item the consumer lands no
// row for is counted on its Tally under its outcome (#1641): refused by its
// lane, unplaced by the fan-out, orphaned, a node-self block that names an
// identity, or declared by the ledger and absent from the batch. The
// gateway counts the rest.
func (s *Server) landNode(ctx context.Context, id, node string, ev *ogv1.TelemetryBatch) ([]string, error) {
	batch := storage.IngestBatch{BatchID: id, Node: node, NodeLogs: nodeLogWrites(ev, node)}
	t := &batch.Tally
	if l := ev.GetLedger(); l != nil {
		stamp := delivery.FromProto(l)
		batch.Ledger = &stamp
		declaredShortfall(t, stamp, ev, node)
	}
	self, named := nodeSelfReport(ev, node)
	if named {
		slog.Warn(nodeSelfOwnerLog, "facility", "ingest", "node", node)
		t.Add(delivery.Metric, delivery.RefusedInvalid, len(ev.GetNodeSelf().GetMetrics()))
	}
	batch.NodeSelf = self

	views := make([]*ogv1.TelemetryBatch, 0, len(ev.GetGroups()))
	var tasks []string
	for _, g := range ev.GetGroups() {
		v := groupView(ev, g)
		if taskBound(v) == 0 {
			continue // nothing the group's task owns: no owner to resolve
		}
		views = append(views, v)
		tasks = append(tasks, g.GetTaskId())
	}
	var owners map[string]storage.TaskOwner
	if len(tasks) > 0 {
		var err error
		if owners, err = s.store.ResolveTaskOwners(ctx, node, tasks); err != nil {
			return nil, err
		}
	}

	// The tick's resolved groups, and every identity they name on each
	// group's conduit, so the fan-out reads once for the whole tick (#1582).
	type resolvedGroup struct {
		view  *ogv1.TelemetryBatch
		owner storage.TaskOwner
		bind  ingestBinding
	}
	resolved := make([]resolvedGroup, 0, len(views))
	wants := map[string][]storage.IdentityKey{}
	sampled := false
	for _, v := range views {
		owner, ok := owners[v.GetTaskId()]
		if !ok {
			// The fence drops an orphan group by design, but never silently
			// (#1200), and only that group: its siblings are the node's own.
			s.recordOrphan(node, v.GetTaskId(), sampleCount(v), len(v.GetTaskLogs()))
			tallySamples(t, v, delivery.Orphaned)
			t.Add(delivery.Log, delivery.Orphaned, len(v.GetTaskLogs()))
			continue
		}
		bind := nodeBinding(node, owner)
		if sampleCount(v) > 0 {
			sampled = true
			identityWants(wants, v, bind.Conduit)
		}
		resolved = append(resolved, resolvedGroup{view: v, owner: owner, bind: bind})
	}
	var reg collection.Registry
	var identities map[string]*storage.IdentityResolution
	if sampled {
		var err error
		if reg, batch.CommandTargeted, err = s.registry.get(ctx, s.store); err != nil {
			return nil, err
		}
		if identities, err = s.resolveIdentities(ctx, wants); err != nil {
			return nil, err
		}
	}

	endpoints := make([]string, 0, len(resolved)) // parallel to batch.Groups
	for _, r := range resolved {
		v := r.view
		group := storage.IngestGroup{TaskID: v.GetTaskId(), Writer: r.bind.Writer, LogLines: taskLogWrites(v, r.owner)}
		if sampleCount(v) > 0 {
			rows := s.deriveSamples(v, r.bind, reg, identities, t)
			group.Metrics, group.Properties, group.Events = rows.Metrics, rows.Properties, rows.Events
			group.Fanned, group.Unbound = rows.Fanned, rows.Unbound
		}
		batch.Groups = append(batch.Groups, group)
		endpoints = append(endpoints, r.owner.EndpointID)
	}

	res, err := s.store.LandBatch(ctx, batch)
	if err != nil {
		return nil, err
	}
	s.settled(ev, res)
	refused := make(map[int]bool, len(res.Refused))
	for _, r := range res.Refused {
		refused[r.Index] = true
		cause := ""
		if r.Err != nil {
			cause = r.Err.Error()
		}
		if r.Index == storage.NodeSelfIndex {
			slog.Warn(nodeSelfRefusedLog, "facility", "ingest", "node", node, "error", cause)
			continue
		}
		slog.Warn(refusedGroupLog, "facility", "ingest", "node", node, "task", r.TaskID, "error", cause)
	}
	if res.Landed {
		var l landing // the groups that landed, parallel to batch.Groups
		for i, r := range resolved {
			if !refused[i] {
				l.lanes(r.view)
			}
		}
		s.committed(l)
	}
	return seenEndpoints(endpoints, refused), nil
}

// declaredShortfall counts on t, as missing, each item stamp declared that
// ev does not carry, and warns of each kind ev carries more of than it
// declared (#1641).
func declaredShortfall(t *delivery.Tally, stamp delivery.Stamp, ev *ogv1.TelemetryBatch, node string) {
	short, over := stamp.Kinds.Against(delivery.BatchItems(ev))
	for _, k := range delivery.Kinds {
		if n := short[k]; n > 0 {
			slog.Warn(declaredShortLog, "facility", "ingest", "node", node, "boot", stamp.Boot, "seq", stamp.Seq, "kind", string(k), "missing", n)
			t.Add(k, delivery.Missing, n)
		}
		if n := over[k]; n > 0 {
			slog.Warn(declaredOverLog, "facility", "ingest", "node", node, "boot", stamp.Boot, "seq", stamp.Seq, "kind", string(k), "over", n)
		}
	}
}

// seenEndpoints is the endpoints a tick stamps seen: each landed group's,
// once, in group order, skipping the refused groups (by index) and a group
// with no endpoint.
func seenEndpoints(endpoints []string, refused map[int]bool) []string {
	var out []string
	seen := make(map[string]bool, len(endpoints))
	for i, e := range endpoints {
		if refused[i] || e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// groupView is one task group read as the single-task batch every derivation
// already takes: the group's task, rows and ts (its observation time, so a
// sample naming no ts of its own takes the group's), on the tick's node.
func groupView(ev *ogv1.TelemetryBatch, g *ogv1.TaskGroup) *ogv1.TelemetryBatch {
	return &ogv1.TelemetryBatch{
		TaskId:     g.GetTaskId(),
		NodeId:     ev.GetNodeId(),
		Ts:         g.GetTs(),
		Metrics:    g.GetMetrics(),
		Properties: g.GetProperties(),
		Events:     g.GetEvents(),
		TaskLogs:   g.GetTaskLogs(),
	}
}

// nodeBinding is a resolved task group's binding on the node lane. The node's
// writer is bound to the task's endpoint: the only instance a node-probe key
// lands under (ADR-0161). The task's component is the conduit a fanned
// sample's identity resolves against, and only it (#1282): the node is
// already confined to it.
func nodeBinding(node string, owner storage.TaskOwner) ingestBinding {
	w := storage.NodeWriter(owner.ComponentID, owner.EndpointName)
	w.Node = owner.NodeID
	return ingestBinding{
		LogNode:         node,
		SampleOwner:     &owner,
		Arc:             componentArc(owner),
		Writer:          w,
		Conduit:         owner.ComponentID,
		ConduitEndpoint: owner.EndpointName,
	}
}

// apiBinding builds the binding for a batch that arrived on the API push lane. The
// route already authorized the caller against this owner, so the batch lands on
// it: unlike the node lane, a push has one owner, not two.
//
// It returns false for a batch whose owner is missing or not addressable, which the
// caller terminates rather than redelivers: a malformed owner will never resolve.
// A push names a component or a system (ADR-0422); a location or a node owner is
// refused, since no route pushes for one and nothing authorized that arc. A
// system batch carries no log lines: log_line is component-only (#589, a log line
// comes off a component), so a system batch with one is refused whole rather
// than landing half of it; the route refuses the same batch with a 422 before it
// is ever published. Refusing here with a named error beats writing a row to the
// wrong home.
//
// o.GetRef() is copied through as-is, dual-accept per ADR-0062 (the push
// route's own input is documented "by name or id"): the route that publishes
// this wire message resolves the caller's owner and sends its id
// (internal/api/telemetry.go), so in practice this is always already a uuid,
// but nothing here assumes that. Downstream (InsertMetricSamples et al.)
// resolves either form the same way TaskOwner.ComponentID's node-lane sibling
// does.
func apiBinding(ev *ogv1.TelemetryBatch) (ingestBinding, bool) {
	o := ev.GetOwner()
	if o == nil || o.GetKind() == "" || o.GetRef() == "" {
		return ingestBinding{}, false
	}
	switch o.GetKind() {
	case "component":
		owner := storage.TaskOwner{ComponentID: o.GetRef()}
		// The component the route authorized is the conduit an identified
		// sample resolves against (#1282): the webhook token's one endpoint's
		// component, its endpoint the batch's source. The push route never
		// sets an identity.
		return ingestBinding{
			LogComponent:    o.GetRef(),
			SampleOwner:     &owner,
			Arc:             componentArc(owner),
			Writer:          storage.PushWriter(),
			Conduit:         o.GetRef(),
			ConduitEndpoint: ev.GetSource(),
		}, true
	case "system":
		if len(ev.GetLogs()) > 0 {
			slog.Warn("push batch refused: a system owns no log lines (log_line is component-owned), batch dropped",
				"ref", o.GetRef(), "logs", len(ev.GetLogs()))
			return ingestBinding{}, false
		}
		// No component and no endpoint: a system's samples are owner-level,
		// and nothing about this batch is a task's.
		return ingestBinding{
			SampleOwner: &storage.TaskOwner{},
			Arc:         sampleArc{Kind: "system", ID: o.GetRef()},
			Writer:      storage.PushWriter(),
		}, true
	}
	slog.Warn("push batch refused: a push names a component or a system, batch dropped",
		"kind", o.GetKind(), "ref", o.GetRef())
	return ingestBinding{}, false
}

// sampleArc is the owner arc a batch's samples land on: an owner_kind and
// the owner's id (or a name ADR-0062 resolves). It is the binding's, never a
// lane derivation's own guess (#422).
type sampleArc struct {
	Kind string
	ID   string
}

// componentArc is the arc of a task's (or a component push's) samples: the
// component the owner names.
func componentArc(owner storage.TaskOwner) sampleArc {
	return sampleArc{Kind: "component", ID: owner.ComponentID}
}

// ingestBinding is the resolved destination for one batch: where its log lines
// land and where its samples land. They differ by lane, which is the whole
// reason this is explicit rather than a single owner. On the node lane, log
// lines are the node's self-logs (node_log) while samples are owned by the
// task's component. On the push lane, both land under the component the API
// authorized, or the samples alone under the system it authorized. At most one
// of LogNode / LogComponent is set per lane; a nil SampleOwner means the
// batch's samples must not be written (no samples, or an orphan task). Arc is
// the owner arc the samples land on, set with SampleOwner; SampleOwner itself
// carries the observing endpoint, if any. Writer is who the samples speak
// for, which the Storage Gateway holds every key to (ADR-0161): the node bound
// to its task's endpoint, or the push lane.
type ingestBinding struct {
	LogComponent string
	LogNode      string
	SampleOwner  *storage.TaskOwner
	Arc          sampleArc
	Writer       storage.KeyWriter
	// Conduit is the component an identified sample's identity resolves
	// against (its id) and ConduitEndpoint the endpoint it arrived through
	// (#1282); empty where no identity may fan out (a system push).
	Conduit         string
	ConduitEndpoint string
}

// sampleCount is how many registry-resolved observations a batch carries across
// its three typed lanes. Zero means no registry to read.
func sampleCount(ev *ogv1.TelemetryBatch) int {
	return len(ev.GetMetrics()) + len(ev.GetProperties()) + len(ev.GetEvents())
}

// taskBound is how much of a batch belongs to its task: its samples and the
// log lines the task collected (#1145). Zero means a self-log batch: no owner
// to resolve.
func taskBound(ev *ogv1.TelemetryBatch) int {
	return sampleCount(ev) + len(ev.GetTaskLogs())
}

// land writes one single-owner batch to its sinks: the raw log lines and the
// three typed lanes (#594: the lane is the array, so each validates against
// its own catalog). The push lane calls it; a node's tick lands its groups
// through landNode (#1582). Both derive through deriveSamples, so neither
// lane can drift from the other's semantics (reject-not-project, per-lane
// validation, the transition-only state guard).
//
// It deliberately knows nothing about JetStream: no message, no ack, no nak. The
// caller owns delivery semantics and decides what a returned error means, which is
// what lets a synchronous API caller reuse it. A returned error is always transient
// (a registry read or a sink write); anything permanent is dropped in place, since
// re-delivering it would not help.
//
// The batch lands as ONE unit under id, its receipt (#311, ADR-0311): every sink
// in one transaction, so a failure part way writes nothing a redelivery would
// write again, and an id that already landed writes nothing at all, so a
// redelivery after a lost ack, a consumer that died after its commit, or a
// publisher's retry past the stream's duplicate window lands each row once.
func (s *Server) land(ctx context.Context, id string, ev *ogv1.TelemetryBatch, bind ingestBinding) error {
	batch := storage.IngestBatch{BatchID: id, Writer: bind.Writer}
	// The push lane's ledger (#1696), the server's own boot's; a batch from
	// a publisher before it carries none and its receipt no ledger.
	if l := ev.GetLedger(); l != nil {
		stamp := delivery.FromProto(l)
		batch.Ledger = &stamp
		declaredShortfall(&batch.Tally, stamp, ev, "")
	}
	// The raw log sink, routed by lane: a node's self-logs to node_log, a push's
	// component-owned lines to log_line (#589). The storage layer refuses any
	// other binding, so a future lane cannot quietly widen the log owner.
	if bind.LogNode != "" {
		batch.NodeLogs = nodeLogWrites(ev, bind.LogNode)
	} else if bind.LogComponent != "" {
		batch.LogLines = logLineWrites(ev, bind.LogComponent)
	}
	// A task's collected log lines (#1145), on the node lane alone: they land
	// on the task's component, owner-bound server-side from the task as every
	// sample is, so a node can land a line only for a component its own task
	// collects for. An orphan batch (no resolved owner) lands none.
	if bind.LogNode != "" && bind.SampleOwner != nil {
		batch.LogLines = append(batch.LogLines, taskLogWrites(ev, *bind.SampleOwner)...)
	}
	if bind.SampleOwner != nil && sampleCount(ev) > 0 {
		// The registry is kept between batches and rebuilt only when the
		// catalogs change (#1279): one fingerprint read, not three catalogs.
		// The command-targeted property names ride with it (#1299).
		reg, targeted, err := s.registry.get(ctx, s.store)
		if err != nil {
			return err
		}
		batch.CommandTargeted = targeted
		wants := map[string][]storage.IdentityKey{}
		identityWants(wants, ev, bind.Conduit)
		identities, err := s.resolveIdentities(ctx, wants)
		if err != nil {
			return err
		}
		rows := s.deriveSamples(ev, bind, reg, identities, &batch.Tally)
		batch.Metrics, batch.Properties, batch.Events = rows.Metrics, rows.Properties, rows.Events
		batch.Fanned, batch.Unbound = rows.Fanned, rows.Unbound
	}
	res, err := s.store.LandBatch(ctx, batch)
	if err != nil {
		return err
	}
	s.settled(ev, res)
	if res.Landed {
		s.committed(batchLanding(ev))
	}
	// The endpoint delivered this batch, whatever the sinks kept of it: an
	// instanced series, an unchanged property the guard skipped, a sample a
	// node's clock dated ahead, or a copy of a batch that already landed. Its
	// freshness is the server's receipt (#933), stamped after the batch's
	// transaction so it adds no lock to it. The push lane has no endpoint, so
	// nothing is stamped there.
	if bind.SampleOwner != nil && taskBound(ev) > 0 {
		if id := bind.SampleOwner.EndpointID; id != "" {
			return s.store.StampEndpointsSeen(ctx, []string{id})
		}
	}
	return nil
}

// sampleRows is what one task's (or one push's) samples derive to: its own
// rows on its arc, and the fan-out's.
type sampleRows struct {
	Metrics    []storage.MetricSampleWrite
	Properties []storage.PropertySampleWrite
	Events     []storage.EventWrite
	Fanned     *storage.FannedRows
	Unbound    []storage.UnboundIdentityWrite
}

// deriveSamples derives one batch's samples under bind. Each lane derives
// against its own catalog (#594): the array the sample rode says which table
// it lands in, and the catalog row says what a valid value looks like. All
// three survive the SAME owner binding and reject-not-project; the split is
// only the sink, not a second trust decision. The property lane is
// transition-only: a value equal to its series' latest is not written, the
// guard held in the insert itself (LandBatch), so a restart that re-emits an
// unchanged verdict adds no row, under any dispatch (#455).
//
// An identified sample is the conduit's report about another owner (#1282):
// it is split off here and lands only through the fan-out, never on the
// conduit, placed by identities, the batch's (or its tick's) one read.
// Every sample that derives no row is counted in t under its outcome
// (#1641); nil counts nothing.
func (s *Server) deriveSamples(ev *ogv1.TelemetryBatch, bind ingestBinding, reg collection.Registry, identities map[string]*storage.IdentityResolution, t *delivery.Tally) sampleRows {
	own, fanned := splitIdentified(ev)
	rows := sampleRows{
		Metrics:    deriveMetrics(own, bind.Arc, *bind.SampleOwner, reg, bind.Writer, t),
		Properties: deriveProperties(own, bind.Arc, *bind.SampleOwner, reg, bind.Writer, t),
		Events:     deriveEvents(own, bind.Arc, *bind.SampleOwner, reg, bind.Writer, t),
	}
	if fanned != nil {
		rows.Fanned, rows.Unbound = s.fanOut(fanned, bind, reg, identities[bind.Conduit], t)
	}
	return rows
}

// identityWants adds what one batch's identified samples name to wants,
// under the conduit they resolve on, once each: a tick gathers every group's
// so its fan-out reads once (#1582). A batch with no conduit, or no
// identified sample, adds nothing.
func identityWants(wants map[string][]storage.IdentityKey, ev *ogv1.TelemetryBatch, conduit string) {
	if conduit == "" {
		return
	}
	_, fanned := splitIdentified(ev)
	if fanned == nil {
		return
	}
	have := make(map[storage.IdentityKey]bool, len(wants[conduit]))
	for _, k := range wants[conduit] {
		have[k] = true
	}
	for _, k := range identityKeys(fanned) {
		if !have[k] {
			have[k] = true
			wants[conduit] = append(wants[conduit], k)
		}
	}
}

// resolveIdentities is the fan-out's one read: every identity wanted,
// resolved against its own conduit's bindings (#1282). Nothing wanted reads
// nothing. A returned error is the read's, transient.
func (s *Server) resolveIdentities(ctx context.Context, wants map[string][]storage.IdentityKey) (map[string]*storage.IdentityResolution, error) {
	if len(wants) == 0 {
		return nil, nil
	}
	return s.store.ResolveIdentityBindings(ctx, wants)
}

// fanOut derives a batch's identified samples from res, their conduit's
// resolution: the rows that land on their bound owners (nil when none), and
// what the conduit heard that no binding places, for its inbox. A batch with
// no conduit to resolve against (a system push, which never carries an
// identity) lands none of them, nor does one whose conduit resolved nothing:
// those count unidentified in t, an unbound one unbound (#1641).
func (s *Server) fanOut(fanned *ogv1.TelemetryBatch, bind ingestBinding, reg collection.Registry, res *storage.IdentityResolution, t *delivery.Tally) (*storage.FannedRows, []storage.UnboundIdentityWrite) {
	n := sampleCount(fanned)
	if bind.Conduit == "" {
		slog.Warn("identified samples on a batch with no conduit, dropped", "facility", "ingest", "samples", n)
		tallySamples(t, fanned, delivery.Unidentified)
		return nil, nil
	}
	if res == nil {
		// The conduit is no component any longer, or nothing the batch named
		// is a valid identity: nothing to land and nothing to keep.
		slog.Warn("identified samples dropped: no conduit to resolve them on, or no valid identity",
			"facility", "ingest", "conduit", bind.Conduit, "samples", n)
		tallySamples(t, fanned, delivery.Unidentified)
		return nil, nil
	}
	plan := planFanOut(fanned, bind.Conduit, res.ConduitName, bind.ConduitEndpoint, res.Owners, reg, time.Now().UTC(), t)
	var rows *storage.FannedRows
	if len(plan.rows.Metrics)+len(plan.rows.Properties)+len(plan.rows.Events) > 0 {
		rows = &plan.rows
	}
	if len(plan.unbound) > 0 {
		heard := 0
		for _, u := range plan.unbound {
			heard += int(u.Count)
		}
		s.unboundWarn.note(bind.Conduit, res.ConduitName, heard, len(plan.unbound), time.Now())
	}
	if plan.invalid > 0 {
		slog.Warn("samples naming no valid identity dropped", "facility", "ingest", "conduit", res.ConduitName, "samples", plan.invalid)
	}
	return rows, plan.unbound
}

// tallySamples counts every sample of ev, by its lane's kind, under o.
func tallySamples(t *delivery.Tally, ev *ogv1.TelemetryBatch, o delivery.Outcome) {
	t.Add(delivery.Metric, o, len(ev.GetMetrics()))
	t.Add(delivery.Property, o, len(ev.GetProperties()))
	t.Add(delivery.Event, o, len(ev.GetEvents()))
}

// failed answers a batch whose landing failed. A write the gateway refuses
// outright (storage.IsRefusedWrite: its owner gone, its key's writer class
// refusing the writer, a value Postgres refuses as data or by a CHECK) is Term'd at once,
// since one batch in flight means every batch behind it waits on it; any
// other failure is redelivered, bounded (nakOrTerm). A refused batch's items
// read refused-batch.
func (s *Server) failed(msg jetstream.Msg, b termed, items map[delivery.Kind]int, err error) {
	if storage.IsRefusedWrite(err) {
		s.term(msg, b, delivery.RefusedBatch, items)
		slog.Warn("telemetry batch refused, dropped", "subject", msg.Subject(), "error", err.Error())
		return
	}
	s.nakOrTerm(msg, b, items)
}

// nakOrTerm redelivers a telemetry message that failed for a transient reason
// (a Nak with a delay that grows with each delivery, so a database that is
// briefly away is waited out rather than raced), unless it has already been
// delivered maxTelemetryDeliveries times (or its delivery count can't be
// read), in which case it is Term'd: deleted from the OG_TELEMETRY work queue
// rather than left to redeliver forever. A message that only ever Nak's never
// leaves a work-queue-retention stream on its own, so this is what actually
// bounds the redelivery loop; ConsumerConfig.MaxDeliver is the JetStream-side
// backstop for the same bound. With one batch in flight, the redelivery comes
// before anything behind it. A batch Term'd for unreadable metadata reads
// refused-metadata, one Term'd at the bound abandoned.
func (s *Server) nakOrTerm(msg jetstream.Msg, b termed, items map[delivery.Kind]int) {
	meta, err := msg.Metadata()
	if err != nil {
		s.term(msg, b, delivery.RefusedMetadata, items)
		slog.Warn("telemetry message dropped: could not read delivery metadata", "subject", msg.Subject(), "error", err)
		return
	}
	if meta.NumDelivered >= maxTelemetryDeliveries {
		s.term(msg, b, delivery.Abandoned, items)
		slog.Warn("telemetry message dropped after repeated failures", "subject", msg.Subject(), "delivered", meta.NumDelivered)
		return
	}
	_ = msg.NakWithDelay(time.Duration(meta.NumDelivered) * telemetryNakDelay)
}

// termed is what the consumer knows of a batch it may Term whole: its id,
// the node that sent it ("" on the API lane), and the ledger it carried (the
// push lane's, under the server's boot, on the API lane; nil from a publisher
// that stamps none).
type termed struct {
	id, node string
	ledger   *ogv1.DeliveryLedger
}

// term Terms a batch whole and records its Term receipt (#1691): every item
// its ledger declared, under cause, so the loss table names why the batch
// never landed instead of reading it missing. A batch with no id or no ledger
// has nothing to record against. The receipt is best effort: one that fails
// to record is warned and the batch is Term'd anyway, its items then reading
// missing, since holding it would stall every batch behind it.
func (s *Server) term(msg jetstream.Msg, b termed, cause delivery.Outcome, items map[delivery.Kind]int) {
	if b.id != "" && b.ledger != nil {
		stamp := delivery.FromProto(b.ledger)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.store.RecordTermedBatch(ctx, storage.TermedBatch{BatchID: b.id, Node: b.node, Ledger: stamp, Outcome: stamp.Kinds.Under(cause)})
		cancel()
		if err != nil {
			slog.Warn("telemetry Term receipt not recorded, its items read missing", "facility", "ingest", "subject", msg.Subject(), "cause", string(cause), "error", err.Error())
		}
	}
	s.drop(msg, items)
}

// logLineWrites turns a push batch's raw log lines into component-owned
// log_line writes (ADR-0066, narrowed by #589): the component is the owner the
// API authorized. Pure: no I/O and no registry (a log line is untyped by
// design). Each line's own ts and source win when set, else the batch's; empty
// severity/facility/correlation stay "" and the storage layer maps them to NULL.
func logLineWrites(ev *ogv1.TelemetryBatch, component string) []storage.LogLineWrite {
	logs := ev.GetLogs()
	if len(logs) == 0 {
		return nil
	}
	out := make([]storage.LogLineWrite, 0, len(logs))
	for _, l := range logs {
		out = append(out, storage.LogLineWrite{
			OwnerKind:     "component",
			OwnerID:       component,
			Instance:      l.GetInstance(),
			Source:        lineSource(ev, l),
			Severity:      l.GetSeverity(),
			Facility:      l.GetFacility(),
			Message:       l.GetMessage(),
			Attributes:    l.GetAttributes(),
			Labels:        l.GetLabels(),
			CorrelationID: l.GetCorrelationId(),
			TS:            lineTime(ev, l),
		})
	}
	return out
}

// taskLogWrites turns a node-lane batch's task_logs (#1145) into log_line
// writes on the task's component: its collecting endpoint is the line's
// instance, and a line naming no source takes the endpoint's transport. Pure,
// and the same ts resolution as a push's lines; the severity table and the
// caps are the Storage Gateway's, held on every log_line write.
func taskLogWrites(ev *ogv1.TelemetryBatch, owner storage.TaskOwner) []storage.LogLineWrite {
	logs := ev.GetTaskLogs()
	if len(logs) == 0 {
		return nil
	}
	out := make([]storage.LogLineWrite, 0, len(logs))
	for _, l := range logs {
		source := l.GetSource()
		if source == "" {
			source = owner.Transport
		}
		out = append(out, storage.LogLineWrite{
			OwnerKind:     "component",
			OwnerID:       owner.ComponentID,
			Instance:      owner.EndpointName,
			Source:        source,
			Severity:      l.GetSeverity(),
			Facility:      l.GetFacility(),
			Message:       l.GetMessage(),
			Attributes:    l.GetAttributes(),
			Labels:        l.GetLabels(),
			CorrelationID: l.GetCorrelationId(),
			TS:            lineTime(ev, l),
		})
	}
	return out
}

// nodeLogWrites turns a node-lane batch's raw log lines into node_log writes:
// the node's self-logs (ADR-0066), owned by the publishing node the subject
// authenticated. Pure, and the same per-line ts and source resolution as
// logLineWrites, so the two lanes cannot drift on the fallbacks.
func nodeLogWrites(ev *ogv1.TelemetryBatch, node string) []storage.NodeLogWrite {
	logs := ev.GetLogs()
	if len(logs) == 0 {
		return nil
	}
	out := make([]storage.NodeLogWrite, 0, len(logs))
	for _, l := range logs {
		out = append(out, storage.NodeLogWrite{
			Node:          node,
			Source:        lineSource(ev, l),
			Severity:      l.GetSeverity(),
			Facility:      l.GetFacility(),
			Message:       l.GetMessage(),
			Attributes:    l.GetAttributes(),
			Labels:        l.GetLabels(),
			CorrelationID: l.GetCorrelationId(),
			TS:            lineTime(ev, l),
		})
	}
	return out
}

// lineTime resolves one log line's timestamp: its own ts wins, else the batch
// ts; when neither is set it stays zero so the storage layer stamps now()
// (GetTs().AsTime() on a nil timestamp is the 1970 epoch, not the zero time,
// and would slip past that guard).
func lineTime(ev *ogv1.TelemetryBatch, l *ogv1.LogLine) time.Time {
	switch {
	case l.GetTs() != nil:
		return l.GetTs().AsTime()
	case ev.GetTs() != nil:
		return ev.GetTs().AsTime()
	}
	return time.Time{}
}

// lineSource resolves one log line's source: its own wins, else the batch's (a
// push declares it once for the batch; a node's self-logs set it per line).
func lineSource(ev *ogv1.TelemetryBatch, l *ogv1.LogLine) string {
	if s := l.GetSource(); s != "" {
		return s
	}
	return ev.GetSource()
}

// The per-lane derivations (#594). Each turns one typed lane of a decoded
// TelemetryBatch + its resolved owner into the rows to persist. Pure: no I/O.
// reject-not-project is per lane: a sample whose name is not registered in the
// lane's OWN catalog is dropped, even when another catalog knows the name. The
// owner is stamped identically on every lane from the binding: the arc it
// resolved (the task's component on the node lane, the authorized component or
// system on the push lane), source=the endpoint's transport, instance=the
// endpoint's name where the sample names none;
// provenance is observed (the insert path fixes that). A sample whose type's
// writer class does not admit the batch's writer is dropped too (ADR-0161; the
// gateway refuses it again, so this keeps one stray row from failing a whole
// batch). A validation failure is permanent, so it is dropped in place (with a
// warning: the node lane has no reply channel), never returned as an error the
// caller would redeliver.

// deriveMetrics maps the metric lane to metric rows: the name must be a
// registered metric_type. The value is a double by wire type, so there is no
// shape to validate beyond the name.
func deriveMetrics(ev *ogv1.TelemetryBatch, arc sampleArc, owner storage.TaskOwner, reg collection.Registry, w storage.KeyWriter, t *delivery.Tally) []storage.MetricSampleWrite {
	var out []storage.MetricSampleWrite
	for _, dp := range ev.GetMetrics() {
		mt, ok := reg.Metric(dp.GetName())
		if !ok {
			t.Add(delivery.Metric, delivery.RefusedUnregistered, 1)
			continue // reject-not-project: not a metric_type name
		}
		instance, refused := laneInstance(mt.WriterClass, dp.GetName(), dp.GetInstance(), owner, w)
		if refused != "" {
			t.Add(delivery.Metric, refused, 1)
			continue
		}
		out = append(out, storage.MetricSampleWrite{
			OwnerKind: arc.Kind,
			OwnerID:   arc.ID,
			Key:       dp.GetName(),
			Instance:  instance,
			Value:     dp.GetValue(),
			Source:    laneSource(ev, owner),
			TS:        sampleTime(ev, dp.GetTs()),
		})
	}
	return out
}

// deriveProperties maps the property lane to property rows: the name must be a
// registered property_type, the value must be canonical JSON of the type's
// data_type, and when the type declares a validation schema the value must
// satisfy it (key.ValidateValue, the same validator every registered key obeys).
func deriveProperties(ev *ogv1.TelemetryBatch, arc sampleArc, owner storage.TaskOwner, reg collection.Registry, w storage.KeyWriter, t *delivery.Tally) []storage.PropertySampleWrite {
	var out []storage.PropertySampleWrite
	for _, dp := range ev.GetProperties() {
		pt, ok := reg.Property(dp.GetName())
		if !ok {
			t.Add(delivery.Property, delivery.RefusedUnregistered, 1)
			continue // reject-not-project: not a property_type name
		}
		instance, refused := laneInstance(pt.WriterClass, dp.GetName(), dp.GetInstance(), owner, w)
		if refused != "" {
			t.Add(delivery.Property, refused, 1)
			continue
		}
		raw := json.RawMessage(dp.GetValueJson())
		if err := key.ValidateValue(pt.DataType, raw, pt.Validation); err != nil {
			slog.Warn("property sample refused: value fails the type's validation",
				"name", dp.GetName(), "error", err)
			t.Add(delivery.Property, delivery.RefusedInvalid, 1)
			continue
		}
		out = append(out, storage.PropertySampleWrite{
			OwnerKind: arc.Kind,
			OwnerID:   arc.ID,
			Key:       dp.GetName(),
			Instance:  instance,
			Value:     propertyText(raw),
			Source:    laneSource(ev, owner),
			TS:        sampleTime(ev, dp.GetTs()),
		})
	}
	return out
}

// deriveEvents maps the event lane to caught occurrences: the name must be a
// registered event_type, a payload must be valid JSON satisfying the type's
// payload_schema when one is declared, and an entry carrying neither message
// nor payload says nothing (dropped, as the old oneof wire dropped a valueless
// sample). A component published the occurrence natively (an xAPI event, an
// SNMP trap): origin caught, the platform did not derive it. Raw log lines are
// a separate ingest lane, not this path (ADR-0066).
func deriveEvents(ev *ogv1.TelemetryBatch, arc sampleArc, owner storage.TaskOwner, reg collection.Registry, w storage.KeyWriter, t *delivery.Tally) []storage.EventWrite {
	var out []storage.EventWrite
	for _, dp := range ev.GetEvents() {
		et, ok := reg.Event(dp.GetName())
		if !ok {
			t.Add(delivery.Event, delivery.RefusedUnregistered, 1)
			continue // reject-not-project: not an event_type name
		}
		instance, refused := laneInstance(et.WriterClass, dp.GetName(), dp.GetInstance(), owner, w)
		if refused != "" {
			t.Add(delivery.Event, refused, 1)
			continue
		}
		payload := dp.GetPayload()
		if dp.GetMessage() == "" && len(payload) == 0 {
			t.Add(delivery.Event, delivery.RefusedEmpty, 1)
			continue // an occurrence with no content says nothing
		}
		if len(payload) > 0 {
			// data_type json pins no base type, so the payload_schema (or nothing)
			// governs; the JSON well-formedness check always runs.
			if err := key.ValidateValue("json", payload, et.PayloadSchema); err != nil {
				slog.Warn("event sample refused: payload fails the event_type's schema",
					"name", dp.GetName(), "error", err)
				t.Add(delivery.Event, delivery.RefusedInvalid, 1)
				continue
			}
		}
		out = append(out, storage.EventWrite{
			OwnerKind:  arc.Kind,
			OwnerID:    arc.ID,
			Key:        dp.GetName(),
			Instance:   instance,
			Origin:     "caught",
			Message:    dp.GetMessage(),
			Attributes: payload,
			Source:     laneSource(ev, owner),
			TS:         sampleTime(ev, dp.GetTs()),
		})
	}
	return out
}

// laneInstance resolves a sample's instance discriminator, or refuses the
// sample, naming the refusal's outcome (#1641); refused is empty for a
// sample it admits. The wire value wins where supplied, else the task
// endpoint's name: the node lane sets none (its endpoint name IS the
// discriminator), a driver may name its own (one series per port), and a push
// has no endpoint, so it must be able to say it.
//
// Two refusals come first (ADR-0161). A sample whose type's writer class does
// not admit the batch's writer is dropped: a push of a calc rule's output, a
// node lane carrying a key a stored driver may no longer emit. And a node-probe
// key is the node's account of the endpoint whose task it ran, so on the node
// lane its instance is that endpoint's name: empty is filled with it, and any
// other (another node's endpoint on the same component, a name no endpoint
// holds) is refused, never written into a series the console reads as that
// endpoint's.
func laneInstance(class writerclass.Class, name, instance string, owner storage.TaskOwner, w storage.KeyWriter) (string, delivery.Outcome) {
	if !writerclass.Admits(class, w.Kind, name) {
		slog.Warn("sample refused: its writer class does not admit the lane",
			"name", name, "class", class, "writer", w.Kind)
		return "", delivery.RefusedWriter
	}
	if class == writerclass.NodeProbe && w.Kind == writerclass.Node {
		if owner.EndpointName == "" || (instance != "" && instance != owner.EndpointName) {
			slog.Warn("node-probe sample refused: a node writes one only under its task's endpoint",
				"name", name, "instance", instance, "endpoint", owner.EndpointName)
			return "", delivery.RefusedInstance
		}
		return owner.EndpointName, ""
	}
	if instance != "" {
		return instance, ""
	}
	return owner.EndpointName, ""
}

// laneSource resolves a batch's provenance source: the batch's own wins, else
// the task interface's type, the same precedence as laneInstance and for the
// same reason.
func laneSource(ev *ogv1.TelemetryBatch, owner storage.TaskOwner) string {
	if s := ev.GetSource(); s != "" {
		return s
	}
	return owner.Transport
}

// propertyText maps a validated canonical-JSON property value to the text form
// the property sink stores (to_jsonb over text): a JSON string lands unquoted,
// any other shape lands as its compact JSON text, so the stored encoding is
// unchanged from the pre-lane wire and the transition guard keeps comparing
// like with like.
func propertyText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err == nil {
		return buf.String()
	}
	return string(raw)
}

// sampleTime resolves the timestamp for one sample on any lane, the #594 ts
// rule: a supplied per-sample ts survives verbatim, the batch ts fills an
// absent one, and zero means the insert path stamps ingest time.
func sampleTime(ev *ogv1.TelemetryBatch, ts *timestamppb.Timestamp) time.Time {
	if ts != nil {
		return ts.AsTime()
	}
	if ev.GetTs() != nil {
		return ev.GetTs().AsTime()
	}
	return time.Time{}
}
