package node

import (
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/rejection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The layered gate (#1056 ruling, ADR-0158). Every connection to an endpoint
// is verified bottom up on the existing ladder: reachable (ICMP, gating only
// where a ping is expected), responsive (the transport: a TCP connect, or the
// protocol's handshake: ssh key exchange, tls), authenticated (the session's
// login). A terminal refusal gates its rung and every rung above it, for that
// endpoint, while the connection fingerprint is unchanged; the rungs below
// keep running, so reachability stays observed while auth is rejected. A
// changed fingerprint lifts the gate (an operator's :retry moves it too), and
// so does the rung passing. The gate is published as endpoint-rejection and
// carried back on the worklist, so a restarted node honors it.
//
// The gate is a set of reasons (#1650): that refusal is one reason, refused,
// and the endpoint's own probe adds another, unreachable, on its second down
// verdict in a row and removes it on its next up. The gate is closed while
// any reason holds and open only when the set is empty. Admission (a poll, a
// command, a run now, a preview) asks whether the gate is closed and names
// the most causal reason; the ladder asks refusedAt, which sees the refused
// reason alone, so an unreachable endpoint keeps its probe credential, starts
// its held sessions and re-arms its listeners, and only the probe reopens it.

// rung is a ladder rung a gate holds at.
type rung string

const (
	rungReachable     rung = "reachable"
	rungResponsive    rung = "responsive"
	rungAuthenticated rung = "authenticated"
)

func (r rung) rank() int {
	switch r {
	case rungReachable:
		return 0
	case rungResponsive:
		return 1
	default:
		return 2
	}
}

// rungOf is the rung a terminal class refuses at: a credential at the login,
// everything else (a host key, a certificate, a protocol) at the transport.
func rungOf(class string) rung {
	if class == driver.ClassAuth {
		return rungAuthenticated
	}
	return rungResponsive
}

// rungIn is the rung a terminal class refuses at over a transport: over udp
// (#1037) nothing authenticates and there is no session rung, so every
// refusal holds the transport rung; elsewhere, snmp's community included
// (#821), a credential's refusal holds the session rung.
func rungIn(transportName, class string) rung {
	if transportName == "udp" {
		return rungResponsive
	}
	return rungOf(class)
}

// statelessRung is the rung a stateless poll sends at: the transport's,
// except where its request carries the credential (an snmp get carries the
// community, #821; an http request carries its session's scheme, #902, or
// rides its login's session, #1114), so an auth gate holds the poll too.
func statelessRung(task collection.TaskSpec) rung {
	if task.Transport == "snmp" || (driver.IsHTTP(task.Transport) && task.Session.IsCredentialed()) {
		return rungAuthenticated
	}
	return rungResponsive
}

// rejectionNone is the endpoint-rejection value when no gate holds.
const rejectionNone = rejection.None

// rejectionOf renders a gate as its endpoint-rejection value.
func rejectionOf(r rung, class, fingerprint string) string {
	return rejection.Gate{Rung: string(r), Class: class, Fingerprint: fingerprint}.String()
}

// reasonKind is a kind of reason a gate holds.
type reasonKind string

const (
	// reasonDisrupted is a disruptive command (a reboot) delivered to the
	// endpoint's component (#1651): added before the command is sent,
	// removed by the endpoint's first up verdict after the quiet period, or
	// at the hold backstop.
	reasonDisrupted reasonKind = "disrupted"
	// reasonRefused is a terminal refusal at a rung (ADR-0158).
	reasonRefused reasonKind = "refused"
	// reasonUnreachable is the endpoint's own probe reading down twice in a
	// row (#1650).
	reasonUnreachable reasonKind = "unreachable"
)

// reasonRule is what a kind of reason does. rank orders the kinds by cause,
// lowest first, so the reason a gate is named for is the one nearest the
// root: disrupted (#1651) names the command that took the device down ahead
// of the refusal and the unreachable reading it causes (a later slice adds
// upstream at 0).
// rungScoped holds only at its rung and above, read by refusedAt; every other
// kind holds the whole endpoint. host closes every connection to the
// endpoint's host, a preview's included. release is how it reopens, for an
// answer to name.
type reasonRule struct {
	rank       int
	rungScoped bool
	host       bool
	release    string
}

// reasonRules is the kind table.
var reasonRules = map[reasonKind]reasonRule{
	reasonDisrupted:   {rank: 1, host: true, release: "it reopens on the endpoint's first up verdict after its quiet period, or at its hold backstop"},
	reasonRefused:     {rank: 2, rungScoped: true, release: "it reopens when the connection fingerprint changes or the rung passes"},
	reasonUnreachable: {rank: 3, host: true, release: "it reopens on the probe's next up verdict"},
}

// downsToClose is how many down verdicts in a row add unreachable: one down
// is a blip, two is the endpoint (#1650).
const downsToClose = 2

// reason is one reason a gate holds: its kind, since when, the task whose
// observation added it, and the words an answer names it with.
type reason struct {
	kind   reasonKind
	since  time.Time
	source string
	text   string
}

// mostCausal is the reason a closed gate is named for: the lowest rank, a tie
// to the oldest. rs is never empty.
func mostCausal(rs []reason) reason {
	best := rs[0]
	for _, r := range rs[1:] {
		br, rr := reasonRules[best.kind].rank, reasonRules[r.kind].rank
		if rr < br || (rr == br && r.since.Before(best.since)) {
			best = r
		}
	}
	return best
}

// unreachableText is the words an unreachable reason is named with.
func unreachableText(since time.Time, source string) string {
	return "unreachable since " + since.UTC().Format(time.RFC3339) + " (probe " + source + ")"
}

// disruptedText is the words a disrupted reason is named with: the command
// type, the command and since when.
func disruptedText(commandType string, commandID int64, since time.Time) string {
	return "disrupted by " + commandType + " (command " + strconv.FormatInt(commandID, 10) + ") since " +
		since.UTC().Format(time.RFC3339)
}

// disruption is an endpoint's disrupted reason's clock (#1651): no probe,
// ping or session dial runs before quietUntil, no up verdict from a probe
// that began before it releases the reason, and holdUntil is the backstop.
type disruption struct {
	quietUntil time.Time
	holdUntil  time.Time
}

// downStreak is an endpoint's down verdicts in a row and the first of them.
type downStreak struct {
	downs int
	first time.Time
}

// hostOf is the host a target names, lowercased, whatever its form: a URL, a
// host:port, or a bare host. Empty when it names none.
func hostOf(target string) string {
	t := strings.TrimSpace(target)
	if strings.Contains(t, "://") {
		u, err := url.Parse(t)
		if err != nil {
			return ""
		}
		return strings.ToLower(u.Hostname())
	}
	if h, _, err := net.SplitHostPort(t); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(strings.Trim(t, "[]"))
}

// gate is one endpoint's held refusal. pinned and offered are a key
// refusal's evidence (#1078), published with the gate and adopted with it.
// adopted marks a gate taken from the worklist after a restart: its reason is
// the node's placeholder, not the refusal's, so it is never published (#1110).
// since is when this node recorded or adopted it.
type gate struct {
	since       time.Time
	fingerprint string
	rung        rung
	class       string
	reason      string
	pinned      string
	offered     string
	adopted     bool
}

// value renders the gate as its endpoint-rejection value.
func (gt gate) value() string {
	return rejection.Gate{Rung: string(gt.rung), Class: gt.class, Fingerprint: gt.fingerprint, PinnedKey: gt.pinned, OfferedKey: gt.offered}.String()
}

// gates is the node's gate per endpoint id, with the fingerprint each
// endpoint is connected with now. held is each endpoint's refused reason;
// others its every other reason, by kind; streak its probe's down verdicts in
// a row; hosts the host its target names.
type gates struct {
	mu      sync.Mutex
	held    map[string]gate
	current map[string]string
	others  map[string]map[reasonKind]reason
	streak  map[string]downStreak
	hosts   map[string]string
	// quiet is each disrupted endpoint's quiet and hold clock (#1651).
	quiet map[string]disruption
}

func newGates() *gates {
	return &gates{held: map[string]gate{}, current: map[string]string{},
		others: map[string]map[reasonKind]reason{}, streak: map[string]downStreak{}, hosts: map[string]string{},
		quiet: map[string]disruption{}}
}

// observe records the endpoint's fingerprint from the worklist and the
// rejection published for it. A gate under another fingerprint lifts; a
// published rejection under this one (a node restart) is adopted.
func (g *gates) observe(endpointID, fingerprint, published string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current[endpointID] = fingerprint
	if gt, ok := g.held[endpointID]; ok && gt.fingerprint != fingerprint {
		delete(g.held, endpointID)
	}
	if _, ok := g.held[endpointID]; ok || published == "" || published == rejectionNone {
		return
	}
	if gt, ok := rejection.Parse(published); ok && gt.Fingerprint == fingerprint && fingerprint != "" {
		g.held[endpointID] = gate{fingerprint: fingerprint, rung: rung(gt.Rung), class: gt.Class,
			reason:  gt.Class + ": refused under this connection fingerprint before the node restarted",
			pinned:  gt.PinnedKey,
			offered: gt.OfferedKey,
			adopted: true,
			since:   time.Now().UTC()}
	}
}

// record gates the endpoint at the refusal's rung under the fingerprint the
// refused attempt was made under. A refusal from an attempt under a
// fingerprint that is no longer current (a late report from a session being
// recycled onto a fixed secret) gates nothing: it is about inputs that are
// gone. false when dropped.
func (g *gates) record(endpointID, fingerprint string, terr *collection.TerminalError) bool {
	return g.recordAt(endpointID, fingerprint, terr, rungOf(terr.Class))
}

// recordAt is record at a rung the caller names.
func (g *gates) recordAt(endpointID, fingerprint string, terr *collection.TerminalError, at rung) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if fingerprint != g.current[endpointID] {
		return false
	}
	g.held[endpointID] = gate{fingerprint: fingerprint, rung: at, class: terr.Class, reason: terr.Error(),
		pinned: terr.Pinned, offered: terr.Offered, since: time.Now().UTC()}
	return true
}

// fingerprintOf is the endpoint's current fingerprint.
func (g *gates) fingerprintOf(endpointID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.current[endpointID]
}

// clear removes the endpoint's refused reason (its rung passed); true when
// one held. Every other reason stays.
func (g *gates) clear(endpointID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.held[endpointID]
	delete(g.held, endpointID)
	return ok
}

// refusedAt is the refused reason that holds rung r for the endpoint, if any:
// the ladder's question, which no other kind of reason answers.
func (g *gates) refusedAt(endpointID string, r rung) (gate, bool) {
	if g == nil {
		return gate{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	gt, ok := g.held[endpointID]
	if !ok || gt.fingerprint != g.current[endpointID] || r.rank() < gt.rung.rank() {
		return gate{}, false
	}
	return gt, true
}

// reasonsLocked is every reason the endpoint's gate holds, the refused one
// only while its fingerprint is current. Caller holds mu.
func (g *gates) reasonsLocked(endpointID string) []reason {
	var rs []reason
	if gt, ok := g.held[endpointID]; ok && gt.fingerprint == g.current[endpointID] {
		rs = append(rs, reason{kind: reasonRefused, since: gt.since, text: gt.reason})
	}
	for _, r := range g.others[endpointID] {
		rs = append(rs, r)
	}
	return rs
}

// closed is the most causal reason the endpoint's gate holds; false when the
// set is empty and the gate is open.
func (g *gates) closed(endpointID string) (reason, bool) {
	return g.pick(endpointID, func(reasonRule) bool { return true })
}

// holding is the most causal reason that holds the whole endpoint, whatever
// the rung: every kind but a rung-scoped refusal. Admission asks refusedAt
// first, at the rung it sends at, then this, so a refusal at a rung above
// never hides an unreachable endpoint.
func (g *gates) holding(endpointID string) (reason, bool) {
	return g.pick(endpointID, func(rule reasonRule) bool { return !rule.rungScoped })
}

func (g *gates) pick(endpointID string, keep func(reasonRule) bool) (reason, bool) {
	if g == nil {
		return reason{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var rs []reason
	for _, r := range g.reasonsLocked(endpointID) {
		if keep(reasonRules[r.kind]) {
			rs = append(rs, r)
		}
	}
	if len(rs) == 0 {
		return reason{}, false
	}
	return mostCausal(rs), true
}

// closedHost is the most causal host-wide reason held on any endpoint whose
// target names the same host as target: what gates a preview, which carries
// no endpoint of its own.
func (g *gates) closedHost(target string) (reason, bool) {
	if g == nil {
		return reason{}, false
	}
	h := hostOf(target)
	if h == "" {
		return reason{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var rs []reason
	for id, host := range g.hosts {
		if host != h {
			continue
		}
		for _, r := range g.reasonsLocked(id) {
			if reasonRules[r.kind].host {
				rs = append(rs, r)
			}
		}
	}
	if len(rs) == 0 {
		return reason{}, false
	}
	return mostCausal(rs), true
}

// place records the host the endpoint's target names.
func (g *gates) place(endpointID, target string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if h := hostOf(target); h != "" {
		g.hosts[endpointID] = h
	} else {
		delete(g.hosts, endpointID)
	}
}

// retain forgets the probe-derived state of endpoints no longer on the
// worklist, a disrupted reason among it, and an unreachable reason whose
// probe task is gone (nothing is left to reopen it). The refused reason is the fingerprint's to lift.
func (g *gates) retain(endpoints, taskIDs map[string]bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for id := range g.hosts {
		if !endpoints[id] {
			delete(g.hosts, id)
		}
	}
	for id := range g.streak {
		if !endpoints[id] {
			delete(g.streak, id)
		}
	}
	for id := range g.quiet {
		if !endpoints[id] {
			delete(g.quiet, id)
			delete(g.others[id], reasonDisrupted)
		}
	}
	for id, rs := range g.others {
		if r, ok := rs[reasonUnreachable]; ok && (!endpoints[id] || !taskIDs[r.source]) {
			delete(rs, reasonUnreachable)
		}
		if len(rs) == 0 {
			delete(g.others, id)
		}
	}
}

// rejectedReason prefixes the reason a rung's responsive reading carries when
// a refusal decided it, not the component: a gated rung's held reading (snmp,
// udp), or the terminal reply that just gated it.
const rejectedReason = "rejected: "

// probeVerdict is the verdict a probe's samples give the gate (#1650):
// EndpointVerdict, less a down that is only a refusal's echo. A rung that
// reads down because a refusal holds it says nothing of whether the endpoint
// can be reached, so it neither adds to the unreachable streak nor resets it,
// as an inconclusive probe does not. A probe result (a port opened or not) is
// always the endpoint's own, refused or not.
func probeVerdict(dps []collection.Sample) (up, ok bool) {
	up, ok = collection.EndpointVerdict(dps)
	if !ok || up {
		return up, ok
	}
	for _, d := range dps {
		if _, isResult := collection.ProbeReached(d); isResult {
			return up, ok
		}
	}
	for _, d := range dps {
		if d.Name == collection.SignalEndpointResponsive && d.IsText {
			if strings.HasPrefix(d.Labels[collection.ReasonLabel], rejectedReason) {
				return false, false
			}
			break
		}
	}
	return up, ok
}

// verdict takes the endpoint's probe verdict, at the time its probe began:
// the second down in a row adds unreachable, since the first down, and an up
// removes it and starts the streak over. An up from a probe that began at or
// after a disruption's quiet period ended removes disrupted too. A verdict
// from a probe that began before it ended (a probe in flight across the
// delivery, the stale-up race) read the device before or as it went down: it
// neither releases the reason nor feeds or resets the streak, so only downs
// read after quiet add unreachable. A probe with no verdict (it could not be
// attempted) never gets here either. true when the gate's set changed.
func (g *gates) verdict(endpointID, taskID string, up bool, at time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if q, ok := g.quiet[endpointID]; ok && at.Before(q.quietUntil) {
		return false
	}
	if up {
		delete(g.streak, endpointID)
		changed := false
		if _, ok := g.others[endpointID][reasonUnreachable]; ok {
			delete(g.others[endpointID], reasonUnreachable)
			changed = true
		}
		if _, ok := g.quiet[endpointID]; ok {
			delete(g.quiet, endpointID)
			delete(g.others[endpointID], reasonDisrupted)
			changed = true
		}
		if len(g.others[endpointID]) == 0 {
			delete(g.others, endpointID)
		}
		return changed
	}
	s := g.streak[endpointID]
	if s.downs == 0 {
		s.first = at.UTC()
	}
	s.downs++
	g.streak[endpointID] = s
	if s.downs < downsToClose {
		return false
	}
	if _, ok := g.others[endpointID][reasonUnreachable]; ok {
		return false
	}
	if g.others[endpointID] == nil {
		g.others[endpointID] = map[reasonKind]reason{}
	}
	g.others[endpointID][reasonUnreachable] = reason{kind: reasonUnreachable, since: s.first, source: taskID,
		text: unreachableText(s.first, taskID)}
	return true
}

// disrupt adds disrupted to each endpoint's gate (#1651), since the delivery,
// naming the command: quiet is how long nothing runs against it, hold the
// backstop that removes the reason when no up verdict does. The probe
// streak starts over: a down read before the reboot says nothing of the
// device after it. A second disruptive command restarts the clock and names
// itself.
func (g *gates) disrupt(endpoints []string, commandType string, commandID int64, since time.Time, quiet, hold time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	since = since.UTC()
	for _, ep := range endpoints {
		if ep == "" {
			continue
		}
		if g.others[ep] == nil {
			g.others[ep] = map[reasonKind]reason{}
		}
		g.others[ep][reasonDisrupted] = reason{kind: reasonDisrupted, since: since,
			source: "command-" + strconv.FormatInt(commandID, 10), text: disruptedText(commandType, commandID, since)}
		g.quiet[ep] = disruption{quietUntil: since.Add(quiet), holdUntil: since.Add(hold)}
		delete(g.streak, ep)
	}
}

// quietUntil is when the endpoint's quiet period ends; false when it is not
// in one at now.
func (g *gates) quietUntil(endpointID string, now time.Time) (time.Time, bool) {
	if g == nil {
		return time.Time{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	q, ok := g.quiet[endpointID]
	if !ok || !now.Before(q.quietUntil) {
		return time.Time{}, false
	}
	return q.quietUntil, true
}

// expiredHold is a disruption the hold backstop ended: the endpoint and the
// command that disrupted it (command-<id>), for the self-log line.
type expiredHold struct {
	endpoint string
	source   string
}

// expire removes disrupted from every endpoint whose hold backstop has come
// with no up verdict, and returns them with the command each named, sorted
// by endpoint.
func (g *gates) expire(now time.Time) []expiredHold {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []expiredHold
	for ep, q := range g.quiet {
		if now.Before(q.holdUntil) {
			continue
		}
		out = append(out, expiredHold{endpoint: ep, source: g.others[ep][reasonDisrupted].source})
		delete(g.quiet, ep)
		delete(g.others[ep], reasonDisrupted)
		if len(g.others[ep]) == 0 {
			delete(g.others, ep)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].endpoint < out[j].endpoint })
	return out
}

// nextExpiry is the earliest hold backstop held, for the run loop's timer.
func (g *gates) nextExpiry() (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var at time.Time
	for _, q := range g.quiet {
		if at.IsZero() || q.holdUntil.Before(at) {
			at = q.holdUntil
		}
	}
	return at, !at.IsZero()
}

// rejectionOf is the endpoint-rejection value the endpoint publishes now,
// every reason its gate holds, and the reason beside it (#1110): the
// refusal's own words, for a gate this node recorded; empty for a gate it
// adopted from the worklist, whose refusal it never saw, and when no refusal
// holds (the value names every other reason itself).
func (g *gates) rejectionOf(endpointID string) (value, why string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var others []rejection.Reason
	for _, r := range g.others[endpointID] {
		others = append(others, rejection.Reason{Kind: string(r.kind), Since: r.since, Source: r.source})
	}
	sort.Slice(others, func(i, j int) bool {
		ri, rj := reasonRules[reasonKind(others[i].Kind)].rank, reasonRules[reasonKind(others[j].Kind)].rank
		if ri != rj {
			return ri < rj
		}
		return others[i].Since.Before(others[j].Since)
	})
	gt, ok := g.held[endpointID]
	if !ok {
		return rejection.Join(nil, others), ""
	}
	refused := rejection.Gate{Rung: string(gt.rung), Class: gt.class, Fingerprint: gt.fingerprint, PinnedKey: gt.pinned, OfferedKey: gt.offered}
	if gt.adopted {
		return rejection.Join(&refused, others), ""
	}
	return rejection.Join(&refused, others), gt.reason
}

// refusal renders a held gate as the terminal error a gated attempt fails
// with.
func (gt gate) refusal() *collection.TerminalError {
	reason := strings.TrimPrefix(gt.reason, gt.class+": ")
	return &collection.TerminalError{Class: gt.class, Reason: reason, Pinned: gt.pinned, Offered: gt.offered}
}
