package node

import (
	"sort"
	"time"
)

// Path propagation (#1652). A component reached through another (a path
// dependency: a peripheral behind a codec, a device behind a gateway) is
// held while its upstream's gate is closed, for any reason: each of its
// in-band endpoints gains the upstream reason, its polls are held and its
// probes are not run, so a gateway going down raises one root cause on the
// gateway and not a storm on everything behind it. When every upstream
// opens, the reason moves to its wait phase: the probes resume, the polls
// stay held, and the endpoint's own up verdict removes it. A dependent with
// no probe has nothing to wait for and is released at once; one that earns
// its own unreachable while waiting drops the upstream reason, so its gate
// names its own cause.
//
// deriveUpstream is the whole rule, pure: the node runs it whenever any
// endpoint's reason set moves, so a dependent closes in the same pass as its
// upstream.

// pathUpstream is one path upstream of an endpoint's component: the
// upstream component, its name, and its in-band endpoints on this node.
type pathUpstream struct {
	component string
	name      string
	endpoints []string
}

// upstreamHold is an endpoint's upstream reason. component and name are the
// component it names; root is the component whose own closure the hold
// descends from, which the cycle guard reads; wait marks the wait phase;
// adopted marks a hold a restarted node took off its published rejection,
// before any upstream closure was derived live.
type upstreamHold struct {
	root      string
	component string
	name      string
	since     time.Time
	wait      bool
	adopted   bool
}

// upstreamState is what the rule reads: each endpoint's component, its path
// upstreams, which endpoints are closed by a reason of their own (any but
// upstream), which hold their own unreachable, which have a probe task,
// which have read up since the node started, the holds the last derive gave,
// and the time a new hold is since.
type upstreamState struct {
	componentOf map[string]string
	edges       map[string][]pathUpstream
	ownClosed   map[string]bool
	unreachable map[string]bool
	probed      map[string]bool
	upSeen      map[string]bool
	prior       map[string]upstreamHold
	now         time.Time
}

// closure is why an upstream endpoint's gate is closed, as a dependent
// reads it: the root it descends from, the component a dependent names,
// and whether it is only a wait-phase hold.
type closure struct {
	root      string
	component string
	name      string
	waiting   bool
}

// deriveUpstream is every endpoint's upstream reason. An endpoint is held
// when an endpoint of one of its path upstreams has a closed gate; it names
// the root: an upstream closed only by an upstream reason in its held phase
// passes on that reason's component, any other closure names the upstream
// itself. No hold descends from a closure rooted in the endpoint's own
// component, and a wait-phase upstream never holds a component it depends on
// itself, so a path cycle terminates and never holds itself up. An endpoint
// that was held and no longer is waits for its own probe when it has one and
// holds no unreachable of its own, and is released otherwise. An adopted
// hold stays held until every probed endpoint of its upstreams has read up:
// an upstream's own unreachable is re-earned after a restart, not adopted,
// so until then nothing says the path is open.
func deriveUpstream(s upstreamState) map[string]upstreamHold {
	adopted := adoptedHeld(s)
	// waiting is every endpoint that would wait if nothing held it now:
	// the holds the last derive gave, on an endpoint still placed here that
	// has a probe to wait for and no unreachable of its own.
	waiting := map[string]upstreamHold{}
	for ep, h := range s.prior {
		if _, placed := s.componentOf[ep]; placed && s.probed[ep] && !s.unreachable[ep] {
			h.wait = true
			waiting[ep] = h
		}
	}
	dependsOn := componentGraph(s)
	dependents := sortedKeys(s.edges)

	held := copyHolds(adopted)
	for range len(s.componentOf) + 2 {
		next := copyHolds(adopted)
		for _, ep := range dependents {
			own := s.componentOf[ep]
			if own == "" {
				continue
			}
			var candidates []closure
			for _, pu := range s.edges[ep] {
				for _, u := range pu.endpoints {
					c, ok := closureOf(s, held, waiting, u, pu)
					if !ok || c.root == own || (c.waiting && reaches(dependsOn, pu.component, own)) {
						continue
					}
					candidates = append(candidates, c)
				}
			}
			if len(candidates) == 0 {
				continue
			}
			next[ep] = holdFrom(candidates, s.prior[ep], s.now)
		}
		if sameHolds(held, next) {
			break
		}
		held = next
	}

	out := map[string]upstreamHold{}
	for ep, h := range held {
		out[ep] = h
	}
	for ep, h := range waiting {
		if _, ok := out[ep]; !ok {
			out[ep] = h
		}
	}
	return out
}

// adoptedHeld is every adopted hold still in its held phase that stays so:
// on an endpoint still placed here, with a probed endpoint of its upstreams
// that has not read up since the node started.
func adoptedHeld(s upstreamState) map[string]upstreamHold {
	out := map[string]upstreamHold{}
	for ep, h := range s.prior {
		if !h.adopted || h.wait {
			continue
		}
		if _, placed := s.componentOf[ep]; !placed {
			continue
		}
		for _, pu := range s.edges[ep] {
			for _, u := range pu.endpoints {
				if s.probed[u] && !s.upSeen[u] {
					out[ep] = h
				}
			}
		}
	}
	return out
}

func copyHolds(m map[string]upstreamHold) map[string]upstreamHold {
	out := make(map[string]upstreamHold, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// closureOf is why upstream endpoint u, reached over pu, is closed; false
// when its gate is open.
func closureOf(s upstreamState, held, waiting map[string]upstreamHold, u string, pu pathUpstream) (closure, bool) {
	if s.ownClosed[u] {
		return closure{root: pu.component, component: pu.component, name: pu.name}, true
	}
	if h, ok := held[u]; ok {
		return closure{root: h.root, component: h.component, name: h.name}, true
	}
	if h, ok := waiting[u]; ok {
		return closure{root: h.root, component: pu.component, name: pu.name, waiting: true}, true
	}
	return closure{}, false
}

// holdFrom picks the hold among the candidates: the component the endpoint
// already names while it is still a candidate, so the answer does not flap
// between two closed upstreams, else the lowest component id. It is since
// the endpoint's prior hold, else now.
func holdFrom(candidates []closure, prior upstreamHold, now time.Time) upstreamHold {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].component != candidates[j].component {
			return candidates[i].component < candidates[j].component
		}
		return candidates[i].root < candidates[j].root
	})
	pick := candidates[0]
	for _, c := range candidates {
		if prior.component != "" && c.component == prior.component {
			pick = c
			break
		}
	}
	since := now.UTC()
	if !prior.since.IsZero() {
		since = prior.since
	}
	return upstreamHold{root: pick.root, component: pick.component, name: pick.name, since: since}
}

// componentGraph is each component's path upstream components.
func componentGraph(s upstreamState) map[string]map[string]bool {
	g := map[string]map[string]bool{}
	for ep, ups := range s.edges {
		c := s.componentOf[ep]
		if c == "" {
			continue
		}
		if g[c] == nil {
			g[c] = map[string]bool{}
		}
		for _, pu := range ups {
			g[c][pu.component] = true
		}
	}
	return g
}

// reaches is whether component from depends on component to, by way of any
// number of path edges.
func reaches(g map[string]map[string]bool, from, to string) bool {
	seen := map[string]bool{from: true}
	stack := []string{from}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for up := range g[c] {
			if up == to {
				return true
			}
			if !seen[up] {
				seen[up] = true
				stack = append(stack, up)
			}
		}
	}
	return false
}

func sameHolds(a, b map[string]upstreamHold) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
