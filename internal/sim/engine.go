package sim

import (
	"crypto/tls"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// Engine manages all simulated devices for a site.
type Engine struct {
	store          *profile.Store
	templates      fs.FS
	portMap        func(int) int // nil serves each profile port as declared
	devices        []*Device
	certs          certCache // lazy self-signed cert shared by TLS interfaces
	sessionPub     SessionEventPublisher
	sessionConfigs map[string]map[string]*SessionConfig // device hostname → interface name → cfg
}

// NewEngine creates a simulator engine over a profile store and a templates
// tree (profiles.Templates() for the embedded catalog). A nil templates tree
// is valid for profiles that render no templates.
func NewEngine(store *profile.Store, templates fs.FS) *Engine {
	return &Engine{
		store:          store,
		templates:      templates,
		sessionConfigs: make(map[string]map[string]*SessionConfig),
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
		if reg := d.TCPSession(); reg != nil {
			reg.SetBus(pub)
		}
	}
}

// LoadDevices creates simulated devices from a site snapshot.
func (e *Engine) LoadDevices(snapshots []DeviceSnapshot) error {
	// Index hostnames by room + profile so a device's defaults can name a
	// sibling in the same room via {room-device:<profile>} (e.g. a video
	// bar's paired touch panel). Mirrors the fleet compiler's seed-time
	// expansion so reset-to-defaults yields the same per-room identity.
	type roomKey struct{ site, building, room string }
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

	for _, snap := range snapshots {
		prof, ok := e.store.Devices[snap.ProfileName]
		if !ok {
			slog.Warn("unknown profile, skipping device",
				"device", snap.DeviceID,
				"profile", snap.ProfileName,
			)
			continue
		}

		ip := net.ParseIP(snap.LoopbackIP)
		if ip == nil {
			return fmt.Errorf("invalid IP %q for device %q", snap.LoopbackIP, snap.DeviceID)
		}

		dev := NewDevice(snap.DeviceID, snap.Hostname, ip, snap.ProfileName, snap.Version, snap.State)
		// Install the profile's defaults snapshot so templates and
		// mutations on this device can read `{{ .Defaults.<path> }}` to
		// reset state paths to their declared defaults (used by reboot
		// and factory-reset routes / CLI verbs). Identity tokens are
		// expanded per room here too, so a reset restores the same
		// room-specific account/peer the device was seeded with rather
		// than the raw profile template.
		peers := peersByRoom[roomKey{snap.SiteCode, snap.Building, snap.Room}]
		dev.SetDefaults(profile.ExpandIdentityTokens(prof.Defaults, snap.SiteCode, snap.Building, snap.Room, peers))

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
						return fmt.Errorf("adding route %q to %q: %w", route.Match, snap.Hostname, err)
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
						return fmt.Errorf("adding command %q to %q: %w", cmd.Match, snap.Hostname, err)
					}
				}
				if iface.Persistent {
					cfg, err := CompileSessionConfig(&iface)
					if err != nil {
						return fmt.Errorf("compiling session config for %q on %q: %w",
							iface.Name, snap.Hostname, err)
					}
					if e.sessionConfigs[snap.Hostname] == nil {
						e.sessionConfigs[snap.Hostname] = make(map[string]*SessionConfig)
					}
					e.sessionConfigs[snap.Hostname][iface.Name] = cfg
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
		// phase 1 of issue #52, behavior compile failures stop the
		// load: a malformed schedule or template should fail loudly
		// at startup rather than silently not animating the device.
		for i := range prof.Behaviors {
			if err := dev.AddBehaviorFromProfile(&prof.Behaviors[i]); err != nil {
				return fmt.Errorf("registering behavior on %q: %w", snap.Hostname, err)
			}
		}

		// Computed fields are read-derived (no goroutine, no event).
		// Installing seeds each elapsed anchor to "now" so uptime starts
		// at ~0 and counts up on read. No StartBehaviors equivalent,
		// there is nothing to run.
		for i := range prof.Computed {
			if err := dev.AddComputedFromProfile(&prof.Computed[i]); err != nil {
				return fmt.Errorf("registering computed field on %q: %w", snap.Hostname, err)
			}
		}

		e.devices = append(e.devices, dev)
	}

	slog.Info("loaded devices", "count", len(e.devices))
	return nil
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
// (including icmp, which is answered by the kernel) are skipped.
func (e *Engine) StartAll() error {
	for _, dev := range e.devices {
		prof, ok := e.store.Devices[dev.ProfileName]
		if !ok {
			continue
		}
		for _, iface := range prof.Interfaces {
			if err := e.startInterface(dev, iface); err != nil {
				return fmt.Errorf("starting %s on %q: %w", iface.Protocol, dev.Hostname, err)
			}
		}
		dev.StartBehaviors()
	}
	return nil
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
		if dev.TCPSession() == nil {
			return nil
		}
		return dev.StartTCPSession(e.port(iface.Port))
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
	for _, dev := range e.devices {
		dev.StopBehaviors()
		dev.StopBehaviorOverrides()
		dev.StopHTTP()
		dev.StopSNMP()
		dev.StopSSH()
		dev.StopTelnet()
		dev.StopTCPSession()
		dev.StopUDP()
	}
}

// Devices returns all loaded devices.
func (e *Engine) Devices() []*Device {
	return e.devices
}

// SetLogger attaches a RequestLogger to every loaded device. Call this after
// LoadDevices and before StartAll so the first request on any listener is
// observed.
func (e *Engine) SetLogger(logger RequestLogger) {
	for _, d := range e.devices {
		d.Logger = logger
	}
}

// SetDiffPublisher attaches a DiffPublisher to every loaded device.
// Call this after LoadDevices and before StartAll so the first mutate
// batch on any device is shipped upstream.
func (e *Engine) SetDiffPublisher(p DiffPublisher) {
	for _, d := range e.devices {
		d.DiffPublisher = p
	}
}

// SetRequestPublisher attaches a RequestPublisher to every loaded
// device. Call this after LoadDevices and before StartAll so the
// first inbound request on any listener is observed. Used by
// listener paths that don't go through RequestLogger, currently
// SNMP, expected to grow to cover any future protocol whose
// observability is richer than the per-request-row Logger shape.
func (e *Engine) SetRequestPublisher(p RequestPublisher) {
	for _, d := range e.devices {
		d.RequestPublisher = p
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
