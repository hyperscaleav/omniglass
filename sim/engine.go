package sim

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/fleet"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/wire"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// Engine manages all simulated devices for a site.
type Engine struct {
	cat        *spec.Catalog
	portMap    func(int) int // nil serves each profile port as declared
	syncMu     sync.Mutex    // serializes Sync
	mu         sync.Mutex    // guards devices and fleet
	devices    []*Device
	fleet      fleetState
	failed     map[string]string // arrivals that failed to start, by device id, as they were tried; guarded by syncMu
	seamMu     sync.RWMutex      // guards the observer seams below
	logger     RequestLogger
	reqPub     RequestPublisher
	diffPub    DiffPublisher
	hit        func(Hit)
	sessionPub SessionEventPublisher
	// obs counts what every device's faces were sent and how each request
	// settled, kept by device id so a rebuilt device counts on.
	obs *endpointObservation
	// cal is the lab calendar (#2757) devices read through their clock.
	cal atomic.Pointer[calendar.Calendar]
	// clock is the simulated world's clock (#2800), real time unless set.
	clock atomic.Pointer[Clock]
	// world is the world the simulator holds (#2773), which a service reads
	// to resolve a system to a room.
	world atomic.Pointer[world.World]
	// arb decides what each device suffers when several sources impair it
	// (#2799); its readers (a platform's offline listing) ask it.
	arb *Arbiter
	// rooms is the memo of room readings every device reads through (#2797).
	rooms roomMemo
}

// NewEngineOver creates a simulator engine over a catalog of Go families
// (#2829): a snapshot's profile name resolves to the family of that name, and
// its product to the family that claims it.
func NewEngineOver(cat *spec.Catalog) *Engine {
	e := &Engine{
		cat: cat,
		obs: newEndpointObservation(),
	}
	e.arb = NewArbiter(e.Now) // stamps an unplugged-since on the simulated world's clock
	e.arb.Derive("scripted-problem", e.scriptedOffline)
	return e
}

// Arbiter is the engine's impairment arbiter (#2799): the one writer to its
// devices' override sets and to the network impairer.
func (e *Engine) Arbiter() *Arbiter { return e.arb }

// SetSessionPublisher attaches a publisher that receives every
// SessionEvent (open / close / push) emitted by any persistent
// interface across the engine's devices. Safe to call before or
// after LoadDevices: any registries already built are updated
// in place via SessionRegistry.SetBus, so callers don't need to
// remember the order. Nil is safe (events are suppressed;
// sessions still work).
func (e *Engine) SetSessionPublisher(pub SessionEventPublisher) {
	e.sessionPub = pub
	for _, d := range e.devices {
		if reg := d.SSHSession(); reg != nil {
			reg.SetBus(pub)
		}
		if reg := d.TelnetSession(); reg != nil {
			reg.SetBus(pub)
		}
		for _, reg := range d.TCPSessions() {
			reg.SetBus(pub)
		}
		for _, reg := range d.UDPSessions() {
			reg.SetBus(pub)
		}
	}
}

// roomKey groups devices that share a room, so a device's defaults can
// name a sibling in the same room via {room-device:<profile>} (e.g. a video
// bar's paired touch panel). Mirrors the fleet compiler's seed-time
// expansion so reset-to-defaults yields the same per-room identity.
type roomKey struct{ site, building, room string }

func roomPeers(snapshots []DeviceSnapshot) map[roomKey]map[string]string {
	peersByRoom := make(map[roomKey]map[string]string)
	for _, snap := range snapshots {
		k := roomKey{snap.SiteCode, snap.Building, snap.Room}
		peers := peersByRoom[k]
		if peers == nil {
			peers = make(map[string]string)
			peersByRoom[k] = peers
		}
		peers[snap.ProfileName] = snap.Hostname
	}
	return peersByRoom
}

// LoadDevices creates simulated devices from a fleet snapshot. Listeners
// start later, in StartAll.
func (e *Engine) LoadDevices(snapshots []DeviceSnapshot) error {
	peersByRoom := roomPeers(snapshots)
	for _, snap := range snapshots {
		dev, err := e.buildDevice(snap, peersByRoom)
		if err != nil {
			return err
		}
		if dev == nil {
			continue
		}
		e.mu.Lock()
		e.devices = append(e.devices, dev)
		e.mu.Unlock()
	}
	slog.Info("loaded devices", "count", len(e.Devices()))
	return nil
}

// buildDevice turns one snapshot into a device with its routes, commands,
// OIDs, sessions, behaviors and computed fields registered, and the engine's
// observer seams installed. It starts nothing. A snapshot naming a profile
// the catalog lacks yields (nil, nil).
func (e *Engine) buildDevice(snap DeviceSnapshot, peersByRoom map[roomKey]map[string]string) (*Device, error) {
	def, ok := e.cat.Lookup(snap.ProfileName)
	if !ok {
		slog.Warn("unknown profile, skipping device",
			"device", snap.DeviceID,
			"profile", snap.ProfileName,
		)
		return nil, nil
	}

	ip := net.ParseIP(snap.LoopbackIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP %q for device %q", snap.LoopbackIP, snap.DeviceID)
	}

	// The device's defaults carry its per-instance identity (hostname,
	// serial and MAC from its id, address), so a reset to defaults keeps
	// the identity the device started with.
	peers := peersByRoom[roomKey{snap.SiteCode, snap.Building, snap.Room}]
	defaults := wire.ExpandIdentityTokens(def.InitialState(), snap.SiteCode, snap.Building, snap.Room, peers)
	if defaults == nil {
		defaults = map[string]any{}
	}
	var model spec.Model
	if snap.Product != "" && snap.Product != snap.ProfileName {
		if r, ok := e.cat.ForProduct(snap.Product); ok {
			mergeInto(defaults, deepCopy(r.Overlay))
			model = r.Model
		}
	}
	fleet.Enrich(defaults, snap.Hostname, snap.DeviceID, ip)
	state := snap.State
	if state == nil {
		// A device listed without state (the server assigns devices, not
		// their state) starts from its defaults.
		state = deepCopy(defaults)
	}
	dev := NewDevice(snap.DeviceID, snap.Hostname, ip, snap.ProfileName, snap.Version, state)
	// The device reads time through the engine's simulated clock (#2800),
	// from the start, so its uptime anchor is seeded on the same clock its
	// reads use.
	dev.now = e.Now
	dev.Product = snap.Product
	dev.room = calendar.Room{Site: snap.SiteCode, Building: snap.Building, Room: snap.Room}
	// Install the profile's defaults snapshot so templates and
	// mutations on this device can read `{{ .Defaults.<path> }}` to
	// reset state paths to their declared defaults (used by reboot
	// and factory-reset routes / CLI verbs). Identity tokens are
	// expanded per room here too, so a reset restores the same
	// room-specific account/peer the device was seeded with rather
	// than the raw profile template.
	dev.SetDefaults(defaults)

	// A Go family answers in Go beside whatever its interfaces declare.
	if g, ok := def.(spec.Served); ok {
		if err := dev.addHandlers(g.Handlers(), model); err != nil {
			return nil, fmt.Errorf("registering handlers on %q: %w", snap.Hostname, err)
		}
	}

	// A family's shared protocol surfaces (sim/surface) are built for this
	// device and attached before it serves; the subjects they declare join
	// the persistent interface they are named for.
	var famSubjects map[string][]surface.Subject
	if g, ok := def.(spec.Served); ok && g.Handlers().Surface != nil {
		sf := g.Handlers().Surface(goDevice{dev})
		for _, c := range sf.Commanders {
			dev.AttachCommands(c)
		}
		for _, r := range sf.Routes {
			dev.AttachRoute(r)
		}
		dev.AttachOIDs(sf.OIDs...)
		famSubjects = sf.Subjects
	}

	// A persistent line interface holds its sessions: the registry the
	// family's commands and the subjects its surface declares are served
	// through. Listeners start later in StartAll; this step is pure state
	// population.
	for _, iface := range def.Surfaces() {
		switch iface.Protocol {
		case "ssh", "telnet", "tcp", "udp":
			if iface.Persistent {
				iface.Subjects = withSurfaceSubjects(iface.Subjects, famSubjects[iface.Name])
				cfg, err := CompileSessionConfig(&iface)
				if err != nil {
					return nil, fmt.Errorf("compiling session config for %q on %q: %w",
						iface.Name, snap.Hostname, err)
				}
				reg := NewSessionRegistry(dev, cfg, e.sessionPub)
				switch iface.Protocol {
				case "ssh":
					dev.AttachSSHSession(reg)
				case "telnet":
					dev.AttachTelnetSession(reg)
				case "tcp":
					dev.AttachTCPSession(reg)
				case "udp":
					dev.AttachUDPSession(reg)
				}
			} else if iface.Protocol == "tcp" {
				// Raw-TCP listeners exist solely to host persistent
				// sessions, there is no one-shot TCP listener mode in the
				// simulator (TCP probes are agent-driven connect tests,
				// not server-side dispatch). A non-persistent tcp
				// interface is almost certainly a family mistake.
				slog.Warn("tcp interface declared without persistent: true; listener will not start",
					"device", snap.Hostname, "name", iface.Name, "port", iface.Port)
			}
		}
	}

	// Computed fields are read-derived (no goroutine, no event).
	// Installing seeds each elapsed anchor to "now" so uptime starts
	// at ~0 and counts up on read. No StartBehaviors equivalent,
	// there is nothing to run.
	for _, el := range def.ElapsedList() {
		if err := dev.AddElapsed(el); err != nil {
			return nil, fmt.Errorf("registering elapsed field on %q: %w", snap.Hostname, err)
		}
	}

	dev.push = compilePush(def.PushFace())

	e.installSeams(dev)
	return dev, nil
}

// SetPortMap sets the port each face asks to bind, from the port its profile
// declares: the choice between fixed ports and ephemeral ones. Fixed is nil
// (the declared ports) or PrivilegedPortShift, which lifts those below 1024 so
// a fleet serves unprivileged without setcap; EphemeralPorts binds every face
// on port 0 and each reports the port it got (Device.Faces). Call before
// StartAll or Sync.
func (e *Engine) SetPortMap(f func(int) int) { e.portMap = f }

// PrivilegedPortShift lifts ports below 1024 (the ones binding needs a
// privilege for) by offset and leaves the rest as declared. Zero offset is
// nil, the declared ports.
func PrivilegedPortShift(offset int) func(int) int {
	if offset == 0 {
		return nil
	}
	return func(p int) int {
		if p < 1024 {
			return p + offset
		}
		return p
	}
}

func (e *Engine) port(declared int) int {
	if e.portMap == nil {
		return declared
	}
	return e.portMap(declared)
}

// StartAll starts listeners for every interface each device declares.
// Dispatches to per-protocol Start<Proto> methods so new protocols can be
// added in one place without touching the outer loop. Unknown protocols
// (including icmp, which is answered by the kernel) are skipped. A listener
// that fails to bind does not stop the rest: every device is started and the
// failures come back joined, each naming its device.
func (e *Engine) StartAll() error {
	var errs []error
	for _, dev := range e.Devices() {
		errs = append(errs, e.startDevice(dev))
	}
	return errors.Join(errs...)
}

func (e *Engine) startDevice(dev *Device) error {
	def, ok := e.cat.Lookup(dev.ProfileName)
	if !ok {
		return nil
	}
	var errs []error
	for _, iface := range def.Surfaces() {
		if err := e.startInterface(dev, iface); err != nil {
			errs = append(errs, fmt.Errorf("device %s: starting %s: %w", dev.DeviceID, iface.Protocol, err))
		}
	}
	dev.StartBehaviors()
	dev.StartPush()
	return errors.Join(errs...)
}

// Sync makes the served fleet match snapshots, the way a running simulator
// follows the server: a new device is built and started, a device no longer
// listed is stopped, and a device whose address, profile or product changed
// is restarted. A device unchanged on both sides keeps serving with its live
// state. Every departing device stops before any arriving one starts, so an
// address handed from one device to another (or swapped between two) is
// free when it is bound. A device that fails to start is stopped and left
// out, so it is not reported as served and the next Sync tries it again.
// Failures are joined, each naming its device; the rest still serve. Calls
// are serialized.
func (e *Engine) Sync(snapshots []DeviceSnapshot) error {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()

	e.mu.Lock()
	current := make(map[string]*Device, len(e.devices))
	for _, d := range e.devices {
		current[d.DeviceID] = d
	}
	e.mu.Unlock()

	// Phase one: keep the unchanged, stop everything else that is leaving
	// or changing.
	kept := make(map[string]*Device, len(snapshots))
	var arriving []DeviceSnapshot
	for _, snap := range snapshots {
		if d, ok := current[snap.DeviceID]; ok && d.LoopbackIP.String() == snap.LoopbackIP &&
			d.ProfileName == snap.ProfileName && d.Product == snap.Product {
			kept[snap.DeviceID] = d
			continue
		}
		arriving = append(arriving, snap)
	}
	// The fleet is changing from here when a device may start or stop. Its
	// generation advances below only for what did: an arrival that failed to
	// start changed nothing served.
	// An arrival that failed to start the way it was already tried binds
	// nothing served, so its retry does not make the fleet read as changing
	// (#1962).
	changing := len(kept) != len(current)
	for _, snap := range arriving {
		if e.failed[snap.DeviceID] != attemptKey(snap) {
			changing = true
		}
	}
	if changing {
		e.mu.Lock()
		e.fleet.changing = true
		e.mu.Unlock()
	}
	for id, d := range current {
		if kept[id] == nil {
			e.stopDevice(d)
		}
	}

	// Phase two: build and start the arrivals.
	peersByRoom := roomPeers(snapshots)
	started := make(map[string]*Device, len(arriving))
	var errs []error
	for _, snap := range arriving {
		dev, err := e.buildDevice(snap, peersByRoom)
		if err != nil {
			e.noteFailed(snap)
			errs = append(errs, fmt.Errorf("device %s: %w", snap.DeviceID, err))
			continue
		}
		if dev == nil {
			continue
		}
		if err := e.startDevice(dev); err != nil {
			e.stopDevice(dev)
			e.noteFailed(snap)
			errs = append(errs, err)
			continue
		}
		delete(e.failed, snap.DeviceID)
		started[snap.DeviceID] = dev
	}

	next := make([]*Device, 0, len(snapshots))
	for _, snap := range snapshots {
		if d := kept[snap.DeviceID]; d != nil {
			next = append(next, d)
		} else if d := started[snap.DeviceID]; d != nil {
			next = append(next, d)
		}
	}
	e.mu.Lock()
	e.devices = next
	if e.fleet.generation == 0 || len(started) > 0 || len(kept) != len(current) {
		e.fleet.generation++
	}
	e.fleet.changing = false
	e.mu.Unlock()
	return errors.Join(errs...)
}

// attemptKey is what a start attempt of a snapshot depends on.
func attemptKey(s DeviceSnapshot) string {
	return s.DeviceID + "|" + s.LoopbackIP + "|" + s.ProfileName + "|" + s.Product
}

// noteFailed remembers that a snapshot failed to start. Called under syncMu.
func (e *Engine) noteFailed(s DeviceSnapshot) {
	if e.failed == nil {
		e.failed = map[string]string{}
	}
	e.failed[s.DeviceID] = attemptKey(s)
}

// fleetState is which fleet the engine serves (#1895). The generation is zero
// until the first Sync and advances each time a Sync changes the devices
// served; changing holds while a Sync is stopping or starting devices. Two
// observations that read the same generation, neither of them changing, saw
// one fleet throughout, so a request sent to a device between them reached a
// bound face.
type fleetState struct {
	generation int64
	changing   bool
}

// startInterface dispatches one profile interface to the matching
// per-protocol listener on the device, at the port the engine's port map
// asks for, and records the face with the address it bound. Returns nil
// silently for protocols this simulator does not (yet) implement so
// LoadDevices never blocks on declared-but-unimplemented interfaces.
func (e *Engine) startInterface(dev *Device, iface wire.Interface) error {
	bound, err := e.bindInterface(dev, iface)
	if err != nil || bound == nil {
		return err
	}
	dev.addFace(Face{Name: iface.Name, Protocol: iface.Protocol, TLS: iface.TLS, Declared: iface.Port, Addr: addrPort(bound)})
	return nil
}

// bindInterface starts one interface's listener and returns the address it
// bound, nil for an interface that binds nothing.
func (e *Engine) bindInterface(dev *Device, iface wire.Interface) (net.Addr, error) {
	port := e.port(iface.Port)
	switch iface.Protocol {
	case "http":
		// Auth is only enforced when the profile sets enforce: true.
		// A profile may declare credentials for documentation
		// without gating traffic, which keeps existing devices
		// open-by-default until they opt in.
		dev.HTTPAuth = httpAuthFromProfile(iface.Auth)
		if iface.Events.Path != "" {
			dev.SetEvents(iface.Events)
		}
		if iface.TLS {
			// The device's own https identity (#902), the same on every
			// run, so a pinned https endpoint survives a restart.
			return dev.startHTTPS(port)
		}
		return dev.startHTTP(port, nil)
	case "snmp":
		community := iface.Auth.Community
		if community == "" {
			community = "public"
		}
		dev.SetSNMPRefusedCommunity(iface.Auth.RefusedCommunity)
		return dev.startSNMP(port, community)
	case "ssh":
		return dev.startSSH(port, iface.Auth.DefaultUser, iface.Auth.DefaultPassword, iface.Auth.Permissive)
	case "telnet":
		return dev.startTelnet(port, iface.Auth.DefaultUser, iface.Auth.DefaultPassword, iface.Auth.Permissive)
	case "tcp":
		// Only persistent: true tcp interfaces actually start a
		// listener; the LoadDevices path warned and skipped the
		// non-persistent case, so the absence of a registry here
		// is the same condition.
		for _, reg := range dev.TCPSessions() {
			if reg.cfg.Port == iface.Port {
				if iface.TLS {
					// The line protocol behind TLS (#1035), served with the
					// device's own key, the profile's validity window.
					return dev.startTLSFace(port, reg, time.Duration(iface.TLSValidityDays)*24*time.Hour)
				}
				return dev.startTCPSession(port, reg, nil)
			}
		}
		return nil, nil
	case "udp":
		// A persistent udp interface serves the line protocol as datagrams
		// (#1037); without persistent: true the face echoes.
		for _, reg := range dev.UDPSessions() {
			if reg.cfg.Port == iface.Port {
				return dev.startUDPFace(port, reg, iface.Name, iface.LossPct)
			}
		}
		return dev.startUDP(port)
	case "websocket":
		// A control socket of its own (#1034), behind the face's own auth,
		// over the device's https identity when it serves tls.
		dev.SetWebSocket(iface.WebSocket, httpAuthFromProfile(iface.Auth))
		return dev.StartWebSocket(port, iface.TLS)
	case "icmp":
		// Kernel answers ICMP in the shared netns; nothing to listen for.
		return nil, nil
	default:
		slog.Warn("unknown interface protocol, skipping",
			"device", dev.Hostname, "protocol", iface.Protocol, "name", iface.Name,
		)
		return nil, nil
	}
}

// StopAll stops all device listeners across every protocol and tears
// down each device's behavior runner. Behaviors stop first so an
// in-flight tick is given a chance to finish under d.mu before the
// listeners that share that lock teardown.
func (e *Engine) StopAll() {
	for _, dev := range e.Devices() {
		e.stopDevice(dev)
	}
}

func (e *Engine) stopDevice(dev *Device) {
	dev.StopPush()
	dev.StopBehaviors()
	dev.StopBehaviorOverrides()
	dev.StopHTTP()
	dev.StopWebSocket()
	dev.StopSNMP()
	dev.StopSSH()
	dev.StopTelnet()
	dev.StopTCPSession()
	dev.StopUDP()
}

// Devices returns the devices currently loaded.
func (e *Engine) Devices() []*Device {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]*Device(nil), e.devices...)
}

// SetLogger sets the RequestLogger every device reports through, including
// devices added later by Sync.
func (e *Engine) SetLogger(logger RequestLogger) {
	e.seamMu.Lock()
	e.logger = logger
	e.seamMu.Unlock()
}

// SetDiffPublisher sets the DiffPublisher every device ships its local mutate
// batches through, including devices added later by Sync.
func (e *Engine) SetDiffPublisher(p DiffPublisher) {
	e.seamMu.Lock()
	e.diffPub = p
	e.seamMu.Unlock()
}

// SetRequestPublisher sets the RequestPublisher every device reports through
// (today SNMP, whose request detail does not fit RequestLog), including
// devices added later by Sync.
func (e *Engine) SetRequestPublisher(p RequestPublisher) {
	e.seamMu.Lock()
	e.reqPub = p
	e.seamMu.Unlock()
}

// SetHitReporter sets the one stream every protocol's hits arrive on, the
// seam a consumer that only needs "this device was reached" uses instead of
// the per-shape logger and publisher.
func (e *Engine) SetHitReporter(f func(Hit)) {
	e.seamMu.Lock()
	e.hit = f
	e.seamMu.Unlock()
}

// installSeams points a device's observer fields at adapters that read the
// engine's current seams, so a seam set after a device starts serving reaches
// it without writing fields its listeners are reading.
func (e *Engine) installSeams(dev *Device) {
	dev.calSrc = e.Calendar
	dev.worldSrc = e.World
	dev.arbSrc = func() *Arbiter { return e.arb }
	dev.memo = &e.rooms
	dev.observe = e.obs.device(dev.DeviceID, dev.Hostname)
	dev.Logger = func(l RequestLog) {
		e.seamMu.RLock()
		logger, hit := e.logger, e.hit
		e.seamMu.RUnlock()
		if logger != nil {
			logger(l)
		}
		if hit != nil {
			hit(hitFromLog(l))
		}
	}
	dev.RequestPublisher = func(ev RequestEvent) {
		e.seamMu.RLock()
		pub, hit := e.reqPub, e.hit
		e.seamMu.RUnlock()
		if pub != nil {
			pub(ev)
		}
		if hit != nil {
			hit(hitFromEvent(ev))
		}
	}
	dev.DiffPublisher = func(c StateChange) {
		e.seamMu.RLock()
		pub := e.diffPub
		e.seamMu.RUnlock()
		if pub != nil {
			pub(c)
		}
	}
}

// AddLoopbackIPs adds loopback IP addresses to the local network interface.
// On Linux this adds to the "lo" interface.
func AddLoopbackIPs(devices []*Device) error {
	seen := make(map[string]bool)
	for _, dev := range devices {
		ipStr := dev.LoopbackIP.String()
		if seen[ipStr] {
			continue
		}
		seen[ipStr] = true

		// Check if we can bind to this IP (skip if already configured).
		lis, err := net.Listen("tcp", ipStr+":0")
		if err == nil {
			lis.Close()
			continue
		}

		// Need to add the IP. Use ip addr add.
		if !strings.HasPrefix(ipStr, "127.") {
			slog.Warn("skipping non-loopback IP", "ip", ipStr)
			continue
		}

		slog.Info("adding loopback IP", "ip", ipStr)
		// We'll use the exec approach for simplicity.
		if err := addLoopbackIP(ipStr); err != nil {
			return fmt.Errorf("adding loopback IP %s: %w", ipStr, err)
		}
	}
	return nil
}
