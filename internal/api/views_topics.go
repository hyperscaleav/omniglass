package api

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/tail"
)

// Topics on the watch (#2731): beside its bindings' rows, a stream carries a
// changed notice for each entity it names whose state a write moved (the
// tail's change lane). A notice is an invalidation, not the data: the client
// refetches through the typed route it already reads, so authorization,
// redaction and the response's shape stay on that route.

// viewMaxTopics bounds one watch's topics, as viewMaxBindings its bindings.
const viewMaxTopics = 32

// changeTopicEvery is the most often one stream names an entity topic: a
// burst of writes to one entity (an alarm storm) reaches the stream as at
// most one notice per window, so no page refetches in a loop.
const changeTopicEvery = 250 * time.Millisecond

// viewChangedEvent is a changed notice: the topic as the client named it, the
// entity it resolved to, the operation, and the tail sequence of the newest
// change it stands for.
type viewChangedEvent struct {
	Topic string `json:"topic" doc:"The topic as the watch named it, kind:ref"`
	Kind  string `json:"kind" doc:"The entity's kind: component or system"`
	ID    string `json:"id" doc:"The entity's uuid"`
	Op    string `json:"op" doc:"What happened to it: update (its state moved; refetch it)"`
	V     uint64 `json:"v" doc:"The tail sequence of the newest change this notice stands for"`
}

// parseTopics splits a watch's topics (kind:ref, comma separated) and checks
// each names a kind a topic may follow. Refs resolve later, in scope.
func parseTopics(in string) ([][2]string, error) {
	if strings.TrimSpace(in) == "" {
		return nil, nil
	}
	parts := strings.Split(in, ",")
	if len(parts) > viewMaxTopics {
		return nil, fmt.Errorf("%d topics, at most %d", len(parts), viewMaxTopics)
	}
	out := make([][2]string, 0, len(parts))
	for i, p := range parts {
		kind, ref, ok := strings.Cut(strings.TrimSpace(p), ":")
		if !ok || ref == "" {
			return nil, fmt.Errorf("topic %d (%q) is not kind:ref", i, p)
		}
		if kind != "component" && kind != "system" {
			return nil, fmt.Errorf("topic %d names kind %q, not one of component, system", i, kind)
		}
		out = append(out, [2]string{kind, ref})
	}
	return out, nil
}

// topicMatch is a stream's topics resolved: each entity (kind:uuid) the
// caller may read, and the spellings the client named it by.
type topicMatch struct {
	after   uint64
	byOwner map[string][]string
}

// resolveTopics resolves each topic's entity inside the caller's read scope.
// Each needs <kind>:read on top of view:read, and a ref out of scope is the
// same 404 as one that does not exist, so a stream never names an entity
// its caller cannot read, and a refused topic says nothing of whether it
// exists. The set is the topics named, never the caller's whole scope.
func resolveTopics(ctx context.Context, a *authenticator, gw storage.Gateway, topics [][2]string) (map[string][]string, error) {
	out := map[string][]string{}
	for i, t := range topics {
		kind, ref := t[0], t[1]
		if !a.allows(ctx, kind, "read") {
			return nil, huma.Error403Forbidden(fmt.Sprintf("topic %d follows a %s, which needs %s:read", i, kind, kind))
		}
		ents, err := gw.ResolveViewEntities(ctx, storage.ViewBinding{Kind: kind, Refs: []string{ref},
			Read: a.scopeFor(ctx, kind, "read"), SystemRead: a.scopeFor(ctx, "system", "read")})
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			owner := kind + ":" + e.ID
			out[owner] = append(out[owner], kind+":"+ref)
		}
	}
	return out, nil
}

// changes is every notice a run of commits carries for the stream's topics.
func (m topicMatch) changes(commits []tail.Commit, seqs []uint64) []viewChangedEvent {
	var out []viewChangedEvent
	for c, commit := range commits {
		if seqs[c] <= m.after {
			continue
		}
		for _, r := range commit.Rows {
			if r.Lane != tail.LaneChange {
				continue
			}
			for _, topic := range m.byOwner[r.OwnerKind+":"+r.OwnerID] {
				out = append(out, viewChangedEvent{Topic: topic, Kind: r.OwnerKind, ID: r.OwnerID, Op: r.Key, V: seqs[c]})
			}
		}
	}
	return out
}

// changeCoalescer holds a stream to one notice per topic per window: a
// notice whose topic went out inside the window waits, folded into the
// newest one for its topic, until the window ends.
type changeCoalescer struct {
	every time.Duration
	last  map[string]time.Time
	held  map[string]viewChangedEvent
	order []string
}

func newChangeCoalescer(every time.Duration) *changeCoalescer {
	return &changeCoalescer{every: every, last: map[string]time.Time{}, held: map[string]viewChangedEvent{}}
}

// add takes a notice; it waits in the coalescer until due hands it out.
func (c *changeCoalescer) add(ev viewChangedEvent) {
	if _, ok := c.held[ev.Topic]; !ok {
		c.order = append(c.order, ev.Topic)
	}
	c.held[ev.Topic] = ev
}

// due hands out, in arrival order, every held notice whose topic's window
// has passed at now (all of them when force is set: the stream is ending),
// and starts each one's next window.
func (c *changeCoalescer) due(now time.Time, force bool) []viewChangedEvent {
	var out []viewChangedEvent
	keep := c.order[:0]
	for _, topic := range c.order {
		if last, ok := c.last[topic]; ok && !force && now.Sub(last) < c.every {
			keep = append(keep, topic)
			continue
		}
		out = append(out, c.held[topic])
		delete(c.held, topic)
		c.last[topic] = now
	}
	c.order = keep
	return out
}

// holding reports whether a notice waits on its window.
func (c *changeCoalescer) holding() bool { return len(c.order) > 0 }

// next is when the earliest held notice comes due; zero when none waits.
func (c *changeCoalescer) next() time.Time {
	var at time.Time
	for _, topic := range c.order {
		t := c.last[topic].Add(c.every)
		if at.IsZero() || t.Before(at) {
			at = t
		}
	}
	return at
}

// mergeTailRows folds a later run's rows into what the stream holds unsent,
// each binding's rows in commit order, the bindings in index order.
func mergeTailRows(held, more []viewTailBindingBody) []viewTailBindingBody {
	if len(held) == 0 {
		return more
	}
	at := map[int]int{}
	for i, b := range held {
		at[b.Binding] = i
	}
	for _, b := range more {
		i, ok := at[b.Binding]
		if !ok {
			at[b.Binding] = len(held)
			held = append(held, b)
			continue
		}
		h := &held[i]
		h.Metric = append(h.Metric, b.Metric...)
		h.Property = append(h.Property, b.Property...)
		h.Events = append(h.Events, b.Events...)
		h.Logs = append(h.Logs, b.Logs...)
	}
	sort.Slice(held, func(i, j int) bool { return held[i].Binding < held[j].Binding })
	return held
}
