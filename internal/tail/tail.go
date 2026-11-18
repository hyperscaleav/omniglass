// Package tail is the live edge of the sample store: the rows each ingest
// commit landed, published in commit order under a monotonic cursor, so a
// view binding reads history from a snapshot and then the tail from that
// snapshot's cursor with no gap and no duplicate at the seam (ADR-1830).
//
// The hub is in-process: it sees the commits of the server it runs in. A
// commit publishes only after it succeeds, and Mark fences commits out while
// a reader takes its snapshot, so every commit is either inside the snapshot
// (at or below the cursor Mark returns) or after it (above the cursor).
package tail

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
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
)

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

// Hub is the published tail. A nil Hub is a store with no tail: Commit
// commits through and Mark marks nothing.
type Hub struct {
	// fence orders commits against marks: commits share it, a mark holds it
	// alone while the reader takes its snapshot.
	fence sync.RWMutex

	mu   sync.Mutex
	boot string
	seq  uint64
	ring []Commit
	// sizes is each held commit's RowBytes total, slotted as ring is; held
	// is their sum, and oldest the sequence of the oldest commit held.
	sizes    []int
	held     int
	oldest   uint64
	maxBytes int
	wake     chan struct{}
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
// commit. The publish happens before the fence is released, so a Mark that
// follows sees it below its cursor.
func (h *Hub) Commit(commit func() error, rows []Row) error {
	if h == nil {
		return commit()
	}
	h.fence.RLock()
	defer h.fence.RUnlock()
	if err := commit(); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	size := 0
	for _, r := range rows {
		size += RowBytes(r)
	}
	h.mu.Lock()
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
	wake := h.wake
	h.wake = make(chan struct{})
	h.mu.Unlock()
	close(wake)
	return nil
}

// drop lets the oldest held commit go. The caller holds mu.
func (h *Hub) drop() {
	slot := h.oldest % uint64(len(h.ring))
	h.held -= h.sizes[slot]
	h.ring[slot], h.sizes[slot] = Commit{}, 0
	h.oldest++
}

// Mark runs open with every commit fenced out and returns the cursor of the
// last commit before it. open is where the reader pins its snapshot.
func (h *Hub) Mark(open func() error) (string, error) {
	if h == nil {
		return "", open()
	}
	h.fence.Lock()
	defer h.fence.Unlock()
	if err := open(); err != nil {
		return "", err
	}
	return h.Cursor(), nil
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
