package sim

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"strings"
	"sync"

	"github.com/hyperscaleav/omniglass/internal/sim/fleet"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// Engine manages all simulated devices for a site.
type Engine struct {
	store      *profile.Store
	templates  fs.FS
	portMap    func(int) int // nil serves each profile port as declared
	syncMu     sync.Mutex    // serializes Sync
	mu         sync.Mutex    // guards devices
	devices    []*Device
	seamMu     sync.RWMutex // guards the observer seams below
	logger     RequestLogger
	reqPub     RequestPublisher
	diffPub    DiffPublisher
	hit        func(Hit)
	certs      certCache // lazy self-signed cert shared by TLS interfaces
	sessionPub SessionEventPublisher
}

// NewEngine creates a simulator engine over a profile store and a templates
// tree (profiles.Templates() for the embedded catalog). A nil templates tree
// is valid for profiles that render no templates.
func NewEngine(store *profile.Store, templates fs.FS) *Engine {
	return &Engine{
		store:     store,
		templates: templates,
	}
}

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
	prof, ok := e.store.Devices[snap.ProfileName]
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
	defaults := profile.ExpandIdentityTokens(prof.Defaults, snap.SiteCode, snap.Building, snap.Room, peers)
	if defaults == nil {
		defaults = map[string]any{}
	}
	if snap.Product != "" && snap.Product != snap.ProfileName {
		if _, overlay, ok := e.store.ForProduct(snap.Product); ok {
			mergeInto(defaults, deepCopy(overlay))
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
	// Install the profile's defaults snapshot so templates and
	// mutations on this device can read `{{ .Defaults.<path> }}` to
	// reset state paths to their declared defaults (used by reboot
	// and factory-reset routes / CLI verbs). Identity tokens are
	// expanded per room here too, so a reset restores the same
	// room-specific account/peer the device was seeded with rather
	// than the raw profile template.
	dev.SetDefaults(defaults)

	// Register routes, commands, and OID bindings from every interface
	// the profile declares. Listeners are started later in StartAll; this
	// step is pure state population.
	for _, iface := range prof.Interfaces {
		switch iface.Protocol {
		case "http":
			for i := range iface.Routes {
				route := &iface.Routes[i]
				tmplContent, err := e.loadTemplate(route.Template)
				if err != nil {
					slog.Warn("skipping route, template not found",
						"device", snap.Hostname, "route", route.Match,
						"template", route.Template, "error", err,
					)
					continue
				}
				if err := dev.AddRouteFromProfile(route, tmplContent); err != nil {
					return nil, fmt.Errorf("adding route %q to %q: %w", route.Match, snap.Hostname, err)
				}
			}
		case "ssh", "telnet", "tcp":
			for i := range iface.Commands {
				cmd := &iface.Commands[i]
				var tmplContent string
				switch {
				case cmd.Response != "":
					tmplContent = cmd.Response
				case cmd.Template != "":
					content, err := e.loadTemplate(cmd.Template)
					if err != nil {
						slog.Warn("skipping command, template not found",
							"device", snap.Hostname, "command", cmd.Match,
							"template", cmd.Template, "error", err,
						)
						continue
					}
					tmplContent = content
				}
				if err := dev.AddCommandFromProfile(cmd, tmplContent); err != nil {
					return nil, fmt.Errorf("adding command %q to %q: %w", cmd.Match, snap.Hostname, err)
				}
			}
			if iface.Persistent {
				cfg, err := CompileSessionConfig(&iface)
				if err != nil {
					return nil, fmt.Errorf("compiling session config for %q on %q: %w",
						iface.Name, snap.Hostname, err)
				}
				// Session events report the port the listener binds, like
				// every other hit, not the profile's declared port.
				cfg.Port = e.port(iface.Port)
				reg := NewSessionRegistry(dev, cfg, e.sessionPub)
				switch iface.Protocol {
				case "ssh":
					dev.AttachSSHSession(reg)
				case "telnet":
					dev.AttachTelnetSession(reg)
				case "tcp":
					dev.AttachTCPSession(reg)
				}
			} else if iface.Protocol == "tcp" {
				// Raw-TCP listeners exist solely to host
				// persistent sessions, there is no one-shot
				// TCP listener mode in the simulator (TCP probes
				// are agent-driven connect tests, not server-
				// side dispatch). A non-persistent tcp interface
				// is almost certainly a profile mistake.
				slog.Warn("tcp interface declared without persistent: true; listener will not start",
					"device", snap.Hostname, "name", iface.Name, "port", iface.Port)
			}
		case "snmp":
			// OID map is shared across all SNMP interfaces on a device;
			// in practice profiles declare only one.
			if dev.OIDMap == nil {
				dev.OIDMap = make(map[string]string, len(iface.OIDMap))
			}
			for statePath, oid := range iface.OIDMap {
				dev.OIDMap[oid] = statePath
			}
		}
	}

	// Behaviors compile last so they can see all interfaces if
	// a future phase wants to gate one on a declared port. Per
	// phase 1 of omniglass-lab#52, behavior compile failures stop the
	// load: a malformed schedule or template should fail loudly
	// at startup rather than silently not animating the device.
	for i := range prof.Behaviors {
		if err := dev.AddBehaviorFromProfile(&prof.Behaviors[i]); err != nil {
			return nil, fmt.Errorf("registering behavior on %q: %w", snap.Hostname, err)
		}
	}

	// Computed fields are read-derived (no goroutine, no event).
	// Installing seeds each elapsed anchor to "now" so uptime starts
	// at ~0 and counts up on read. No StartBehaviors equivalent,
	// there is nothing to run.
	for i := range prof.Computed {
		if err := dev.AddComputedFromProfile(&prof.Computed[i]); err != nil {
			return nil, fmt.Errorf("registering computed field on %q: %w", snap.Hostname, err)
		}
	}

	e.installSeams(dev)
	return dev, nil
}

// SetPortMap remaps every profile port at listen time, so a fleet can
// serve unprivileged (shift ports below 1024 above it) without setcap.
// Call before StartAll; nil restores the declared ports.
func (e *Engine) SetPortMap(f func(int) int) { e.portMap = f }

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
	prof, ok := e.store.Devices[dev.ProfileName]
	if !ok {
		return nil
	}
	var errs []error
	for _, iface := range prof.Interfaces {
		if err := e.startInterface(dev, iface); err != nil {
			errs = append(errs, fmt.Errorf("device %s: starting %s: %w", dev.DeviceID, iface.Protocol, err))
		}
	}
	dev.StartBehaviors()
	return errors.Join(errs...)
}

// Sync makes the served fleet match snapshots, the way a running simulator
// follows the server: a new device is built and started, a device no longer
// listed is stopped, and a device whose address or profile changed is
// restarted. A device unchanged on both sides keeps serving with its live
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
		if d, ok := current[snap.DeviceID]; ok && d.LoopbackIP.String() == snap.LoopbackIP && d.ProfileName == snap.ProfileName {
			kept[snap.DeviceID] = d
			continue
		}
		arriving = append(arriving, snap)
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
			errs = append(errs, fmt.Errorf("device %s: %w", snap.DeviceID, err))
			continue
		}
		if dev == nil {
			continue
		}
		if err := e.startDevice(dev); err != nil {
			e.stopDevice(dev)
			errs = append(errs, err)
			continue
		}
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
	e.mu.Unlock()
	return errors.Join(errs...)
}

// startInterface dispatches one profile interface to the matching
// per-protocol listener on the device. Returns nil silently for protocols
// this simulator does not (yet) implement so LoadDevices never blocks on
// declared-but-unimplemented interfaces.
func (e *Engine) startInterface(dev *Device, iface profile.Interface) error {
	switch iface.Protocol {
	case "http":
		var cert *tls.Certificate
		if iface.TLS {
			c, err := e.certs.get()
			if err != nil {
				return fmt.Errorf("obtaining TLS cert: %w", err)
			}
			cert = c
		}
		// Auth is only enforced when the profile sets enforce: true.
		// A profile may declare credentials for documentation
		// without gating traffic, which keeps existing devices
		// open-by-default until they opt in.
		dev.HTTPAuth = httpAuthFromProfile(iface.Auth)
		return dev.StartHTTP(e.port(iface.Port), cert)
	case "snmp":
		community := iface.Auth.Community
		if community == "" {
			community = "public"
		}
		return dev.StartSNMP(e.port(iface.Port), community)
	case "ssh":
		return dev.StartSSH(e.port(iface.Port), iface.Auth.DefaultUser, iface.Auth.DefaultPassword, iface.Auth.Permissive)
	case "telnet":
		return dev.StartTelnet(e.port(iface.Port), iface.Auth.DefaultUser, iface.Auth.DefaultPassword, iface.Auth.Permissive)
	case "tcp":
		// Only persistent: true tcp interfaces actually start a
		// listener; the LoadDevices path warned and skipped the
		// non-persistent case, so the absence of a registry here
		// is the same condition.
		port := e.port(iface.Port)
		for _, reg := range dev.TCPSessions() {
			if reg.cfg.Port == port {
				return dev.startTCPSession(port, reg)
			}
		}
		return nil
	case "udp":
		return dev.StartUDP(e.port(iface.Port))
	case "icmp":
		// Kernel answers ICMP in the shared netns; nothing to listen for.
		return nil
	default:
		slog.Warn("unknown interface protocol, skipping",
			"device", dev.Hostname, "protocol", iface.Protocol, "name", iface.Name,
		)
		return nil
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
	dev.StopBehaviors()
	dev.StopBehaviorOverrides()
	dev.StopHTTP()
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

// loadTemplate reads a template from the templates tree. Profile template
// paths are slash-separated and relative (api/kestrel-vroom/status.json.gotempl).
func (e *Engine) loadTemplate(tmplPath string) (string, error) {
	if e.templates == nil {
		return "", fmt.Errorf("template %q: engine has no templates tree", tmplPath)
	}
	data, err := fs.ReadFile(e.templates, tmplPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
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
