package ingress

import (
	"sort"
	"sync"
)

// PostTarget is the endpoint a webhook post was addressed to, and the
// component and endpoint name its counts land on.
type PostTarget struct {
	EndpointID  string
	ComponentID string
	Endpoint    string
}

// PostCount is one endpoint's webhook counts for a report window (#1369).
// Posts is nil when the window reports no post count for it, and Refusals
// when it refused none.
type PostCount struct {
	PostTarget
	Posts    *int
	Refusals *int
}

// PostWindow counts each webhook endpoint's accepted and refused posts over a
// collection report window (#1369): the server's account of a push
// endpoint, built-in data a webhook-fed component carries whatever its
// driver. A window with posts reports their count; the window after it
// reports zero once, so the series shows a stream stopping; a quiet endpoint
// then reports nothing, and its last heard says how long it has been quiet.
// Refusals report only when nonzero. The receiver takes a window when its
// report is due.
type PostWindow struct {
	mu       sync.Mutex
	posts    map[string]int
	refusals map[string]int
	targets  map[string]PostTarget
	// heard is the endpoints whose last window had posts: each owes one zero
	// when the next window has none.
	heard map[string]PostTarget
}

// NewPostWindow starts an empty window.
func NewPostWindow() *PostWindow {
	return &PostWindow{posts: map[string]int{}, refusals: map[string]int{}, targets: map[string]PostTarget{}, heard: map[string]PostTarget{}}
}

// Accepted counts a post the endpoint's scheme admitted and the server
// landed.
func (w *PostWindow) Accepted(t PostTarget) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.posts[t.EndpointID]++
	w.targets[t.EndpointID] = t
}

// Refused counts a post the endpoint's scheme refused.
func (w *PostWindow) Refused(t PostTarget) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refusals[t.EndpointID]++
	w.targets[t.EndpointID] = t
}

// Pending reports a window with something to report: a count, or a zero owed
// after a window with posts.
func (w *PostWindow) Pending() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.targets) > 0 || len(w.heard) > 0
}

// Take returns the window's counts, ordered by endpoint, and starts the next
// window.
func (w *PostWindow) Take() []PostCount {
	w.mu.Lock()
	defer w.mu.Unlock()
	byEndpoint := map[string]*PostCount{}
	row := func(t PostTarget) *PostCount {
		r := byEndpoint[t.EndpointID]
		if r == nil {
			r = &PostCount{PostTarget: t}
			byEndpoint[t.EndpointID] = r
		}
		return r
	}
	heard := map[string]PostTarget{}
	for id, n := range w.posts {
		n := n
		row(w.targets[id]).Posts = &n
		heard[id] = w.targets[id]
	}
	for id, t := range w.heard {
		if _, posted := w.posts[id]; !posted {
			zero := 0
			row(t).Posts = &zero
		}
	}
	for id, n := range w.refusals {
		n := n
		row(w.targets[id]).Refusals = &n
	}
	out := make([]PostCount, 0, len(byEndpoint))
	for _, r := range byEndpoint {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndpointID < out[j].EndpointID })
	w.posts, w.refusals, w.targets, w.heard = map[string]int{}, map[string]int{}, map[string]PostTarget{}, heard
	return out
}
