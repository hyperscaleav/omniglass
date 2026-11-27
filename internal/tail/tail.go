// Package tail is the live edge of the sample store: the rows each ingest
// commit landed, published in commit order under a monotonic cursor, so a
// view binding reads history from a snapshot and then the tail from that
// snapshot's cursor with no gap and no duplicate at the seam (ADR-1830).
//
// The hub is in-process: it sees the commits of the server it runs in. A
// commit publishes only after it succeeds, and Mark fences commits out while
// a reader takes its snapshot, so every commit is either inside the snapshot
// (at or below the cursor Mark returns) or after it (above the cursor). Mark
// holds no lock over the snapshot: it counts commits begun and in flight, and
// retries (after the in-flight ones publish) when one overlapped the snapshot.
package tail

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Lane names the primitive a row belongs to, the view binding's primitive.
type Lane string

const (
	LaneMetric   Lane = "metric"
	LaneProperty Lane = "property"
	LaneEvent    Lane = "event"
	LaneLog      Lane = "log"
	// LaneChange is a state-change notice (#2731): an entity a write changed,
	// published after its transaction commits, so a live page refetches the
	// read that shows it. It carries no data: OwnerKind and OwnerID name the
	// entity, Key the operation.
	LaneChange Lane = "change"
)

// ChangeUpdate is the operation a change notice names when an entity's state
// moved (its health recomputed, a slot or member changed).
const ChangeUpdate = "update"

// Row is one landed sample, event, or log line, as the store kept it: owner
// resolved to its uuid, timestamp capped, value normalized. Fields a lane
// does not carry stay zero.
type Row struct {
	Lane      Lane
	OwnerKind string
	// OwnerID is the owning row's uuid, the identity a binding's resolved
	// entities are matched on.
	OwnerID  string
	Key      string
	Instance string
	TS       time.Time
	// Number is a metric's value; Text is a property's.
	Number float64
	Text   string
	// ID is an event or log row's id, its page-cursor tiebreak.
	ID int64
	// ReceivedAt is a log line's arrival, the order the log lane reads in.
	ReceivedAt time.Time
	Origin     string
	Message    string
	Attributes []byte
	Severity   string
	Facility   string
	// Channel is a log line's source channel (syslog, journal).
	Channel string
}

// Commit is one transaction's landed rows and the cursor just after them.
type Commit struct {
	Cursor string
	Rows   []Row
}

// ErrExpired is a cursor the hub cannot resume from: older than the ring
// holds, from another boot of the server, ahead of the hub, or malformed.
// The reader re-reads history to get a fresh one.
var ErrExpired = errors.New("tail: cursor expired")

// ErrMarkBusy is returned by Mark when commits keep racing its snapshot after
// markMaxRetries attempts. The caller returns a retryable error (503).
var ErrMarkBusy = errors.New("tail: mark busy")

// markMaxRetries is how many times Mark retries when a commit races its
// snapshot before giving up and returning ErrMarkBusy.
const markMaxRetries = 3

// markSettleWait bounds how long Mark waits for in-flight commits to publish
// before its next attempt.
const markSettleWait = 100 * time.Millisecond

// Hub is the published tail. A nil Hub is a store with no tail: Commit
// commits through and Mark marks nothing.
type Hub struct {
	mu   sync.Mutex
	boot string
	seq  uint64
	// begun counts commits started and inflight those not yet published (or
	// failed): a commit is durable before it is published, and Mark must not
	// mark between the two.
	begun, inflight uint64
	// idle is closed when inflight returns to zero; Mark waits on it between
	// attempts instead of retrying back to back.
	idle chan struct{}
	ring []Commit
	// sizes is each held commit's RowBytes total, slotted as ring is; held
	// is their sum, and oldest the sequence of the oldest commit held.
	sizes    []int
	held     int
	oldest   uint64
	maxBytes int
	wake     chan struct{}
	// followers counts the streams following change notices (#2731).
	followers atomic.Int64
	// dropped is one past the sequence at which a notice last went unpublished for want
	// of a follower.
	dropped uint64
}

// New returns a hub whose ring holds the last commits commits, and of those
// only as many of the newest as fit in maxBytes of rows (zero is no byte
// bound). The newest commit is held whatever its size.
func New(commits, maxBytes int) *Hub {
	if commits < 1 {
		commits = 1
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	return &Hub{boot: hex.EncodeToString(b[:]), ring: make([]Commit, commits), sizes: make([]int, commits),
		oldest: 1, maxBytes: maxBytes, wake: make(chan struct{})}
}

// RowBytes is what a row weighs against the ring's byte bound: the row
// itself and the bytes its strings and attributes carry.
func RowBytes(r Row) int {
	return int(unsafe.Sizeof(r)) + len(r.OwnerKind) + len(r.OwnerID) + len(r.Key) + len(r.Instance) + len(r.Text) +
		len(r.Origin) + len(r.Message) + len(r.Attributes) + len(r.Severity) + len(r.Facility) + len(r.Channel)
}

// Commit runs commit and, when it succeeds, publishes rows as the next
// commit. No lock is held across the database I/O so a concurrent Mark
// never stalls ingest; Commit counts itself begun and in flight until it has
// published (or failed), and Mark's begun/inflight check detects any commit
// that raced its snapshot and retries once those have published.
func (h *Hub) Commit(commit func() error, rows []Row) error {
	if h == nil {
		return commit()
	}
	h.mu.Lock()
	h.begun++
	if h.inflight == 0 {
		h.idle = make(chan struct{})
	}
	h.inflight++
	h.mu.Unlock()
	if err := commit(); err != nil {
		h.finish()
		return err
	}
	if len(rows) == 0 {
		h.finish()
		return nil
	}
	size := 0
	for _, r := range rows {
		size += RowBytes(r)
	}
	h.mu.Lock()
	h.settle()
	h.append(rows, size)
	wake := h.wake
	h.wake = make(chan struct{})
	h.mu.Unlock()
	close(wake)
	return nil
}

// FollowChanges registers a stream that follows the change lane and returns
// its release. While none is registered PublishChanges drops its notices, so
// a recompute storm nobody follows never lands a commit on the ring and
// evicts sample rows a binding's cursor resumes from (#2731).
func (h *Hub) FollowChanges() func() {
	if h == nil {
		return func() {}
	}
	h.followers.Add(1)
	var once sync.Once
	return func() { once.Do(func() { h.followers.Add(-1) }) }
}

// Following reports whether any stream follows the change lane.
func (h *Hub) Following() bool { return h != nil && h.followers.Load() > 0 }

// PublishChanges publishes change notices, only while a stream follows them.
// A notice dropped is remembered (ChangesDroppedSince), so a stream that
// resumes across the gap is told to refetch rather than trusted to have
// missed nothing.
func (h *Hub) PublishChanges(rows []Row) {
	if h == nil || len(rows) == 0 {
		return
	}
	if h.Following() {
		h.Publish(rows)
		return
	}
	h.mu.Lock()
	h.dropped = h.seq + 1
	h.mu.Unlock()
}

// ChangesDroppedSince reports whether a change notice went unpublished at or
// after the commit at seq, so a topics cursor at seq cannot vouch for it.
func (h *Hub) ChangesDroppedSince(seq uint64) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dropped > 0 && h.dropped-1 >= seq
}

// Publish lands rows that are already durable as the next commit: the change
// lane's notices, which a write's transaction publishes after it commits. It
// takes no part in Mark's fence, which orders a snapshot against the sample
// rows it reads; a notice is not in any snapshot, the client refetches its
// route instead. No rows publishes nothing.
func (h *Hub) Publish(rows []Row) {
	if h == nil || len(rows) == 0 {
		return
	}
	size := 0
	for _, r := range rows {
		size += RowBytes(r)
	}
	h.mu.Lock()
	h.append(rows, size)
	wake := h.wake
	h.wake = make(chan struct{})
	h.mu.Unlock()
	close(wake)
}

// append slots rows as the next commit, dropping the oldest held past the
// ring's bounds. The caller holds mu.
func (h *Hub) append(rows []Row, size int) {
	h.seq++
	slots := uint64(len(h.ring))
	if h.seq-h.oldest >= slots {
		h.drop()
	}
	h.ring[h.seq%slots] = Commit{Cursor: h.cursor(h.seq), Rows: rows}
	h.sizes[h.seq%slots] = size
	h.held += size
	for h.maxBytes > 0 && h.held > h.maxBytes && h.oldest < h.seq {
		h.drop()
	}
}

// finish ends a commit that publishes nothing.
func (h *Hub) finish() {
	h.mu.Lock()
	h.settle()
	h.mu.Unlock()
}

// settle ends one in-flight commit, releasing Mark's waiters when it was the
// last. The caller holds mu.
func (h *Hub) settle() {
	h.inflight--
	if h.inflight == 0 {
		close(h.idle)
	}
}

// drop lets the oldest held commit go. The caller holds mu.
func (h *Hub) drop() {
	slot := h.oldest % uint64(len(h.ring))
	h.held -= h.sizes[slot]
	h.ring[slot], h.sizes[slot] = Commit{}, 0
	h.oldest++
}

// Mark runs open and returns the cursor of the last commit whose ring
// publish completed before open returned. No lock is held across open's
// database I/O, so ingest commits are never stalled by a view read.
//
// A commit is durable before it is published, so a snapshot opened while one
// is in flight holds its rows yet the cursor would not: Mark retries until no
// commit was in flight when it began and none began during open (#2357).
//
// If commits raced the snapshot (one began during open), Mark retries
// up to markMaxRetries times. After that it returns ErrMarkBusy; the
// caller should return a retryable 503 so the client backs off.
func (h *Hub) Mark(open func() error) (string, error) {
	if h == nil {
		return "", open()
	}
	for range markMaxRetries {
		h.mu.Lock()
		begun, clear := h.begun, h.inflight == 0
		h.mu.Unlock()

		err := open()
		h.mu.Lock()
		after := h.seq
		moved := h.begun != begun
		cur := h.cursor(after)
		h.mu.Unlock()

		if err != nil {
			return "", err
		}
		if clear && !moved {
			return cur, nil
		}
		h.awaitIdle()
	}
	return "", ErrMarkBusy
}

// awaitIdle waits, bounded by markSettleWait, for the commits in flight to
// publish, so a retry does not race steady ingest back to back.
func (h *Hub) awaitIdle() {
	h.mu.Lock()
	idle, busy := h.idle, h.inflight > 0
	h.mu.Unlock()
	if !busy {
		return
	}
	t := time.NewTimer(markSettleWait)
	defer t.Stop()
	select {
	case <-idle:
	case <-t.C:
	}
}

// Cursor is the hub's newest cursor.
func (h *Hub) Cursor() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cursor(h.seq)
}

// Wait returns a channel closed by the next commit that publishes rows.
func (h *Hub) Wait() <-chan struct{} {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.wake
}

// Read returns the commits after cursor, oldest first, at most max of them
// (zero is no cap), and the cursor to resume after what it returned.
func (h *Hub) Read(cursor string, max int) ([]Commit, string, error) {
	if h == nil {
		return nil, cursor, ErrExpired
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	after, ok := h.parse(cursor)
	if !ok || after > h.seq {
		return nil, "", ErrExpired
	}
	if after+1 < h.oldest {
		return nil, "", ErrExpired
	}
	size := uint64(len(h.ring))
	n := h.seq - after
	if max > 0 && n > uint64(max) {
		n = uint64(max)
	}
	out := make([]Commit, 0, n)
	for s := after + 1; s <= after+n; s++ {
		out = append(out, h.ring[s%size])
	}
	return out, h.cursor(after + n), nil
}

// Seq is a cursor's position on this hub, for ordering cursors; a cursor
// from another boot, ahead of the hub, or malformed is expired. A cursor the
// ring has dropped still orders: Read is what refuses it.
func (h *Hub) Seq(cursor string) (uint64, error) {
	if h == nil {
		return 0, ErrExpired
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	n, ok := h.parse(cursor)
	if !ok || n > h.seq {
		return 0, ErrExpired
	}
	return n, nil
}

func (h *Hub) cursor(seq uint64) string {
	return h.boot + "." + strconv.FormatUint(seq, 10)
}

func (h *Hub) parse(cursor string) (uint64, bool) {
	boot, seq, ok := strings.Cut(cursor, ".")
	if !ok || boot != h.boot {
		return 0, false
	}
	n, err := strconv.ParseUint(seq, 10, 64)
	return n, err == nil
}
