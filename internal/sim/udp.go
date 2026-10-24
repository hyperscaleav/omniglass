package sim

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
)

// StartUDP binds a bare echo responder on the device's loopback IP:port.
// Echoes every inbound datagram back to the sender. Useful as a minimal
// proof of UDP reachability and for vendor-specific request/response
// protocols where the caller already controls both sides of the wire.
func (d *Device) StartUDP(port int) error {
	_, err := d.startUDP(port)
	return err
}

// startUDP binds and serves the UDP echo responder and returns the address
// it bound.
func (d *Device) startUDP(port int) (net.Addr, error) {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	addr = conn.LocalAddr().String()
	port = boundPort(conn.LocalAddr()) // what its hits report
	d.udpMu.Lock()
	d.udpConns = append(d.udpConns, conn)
	d.udpMu.Unlock()

	go func() {
		slog.Debug("device UDP started", "device", d.Hostname, "addr", addr)
		buf := make([]byte, 4096)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				if !strings.Contains(err.Error(), "closed") {
					slog.Error("device UDP read error", "device", d.Hostname, "error", err)
				}
				return
			}
			// Echo everything back.
			_, _ = conn.WriteTo(buf[:n], from)
			if d.Logger != nil {
				d.Logger(RequestLog{
					DeviceID:  d.DeviceID,
					Hostname:  d.Hostname,
					Protocol:  "udp",
					Port:      port,
					Method:    "ECHO",
					Path:      fmt.Sprintf("%d bytes", n),
					Remote:    from.String(),
					LocalAddr: addr,
					Status:    200,
				})
			}
		}
	}()
	return conn.LocalAddr(), nil
}

// StopUDP closes every UDP face: the echo responders and the datagram
// faces, with their sources' sessions.
func (d *Device) StopUDP() {
	d.udpMu.Lock()
	conns, faces := d.udpConns, d.udpFaces
	d.udpConns, d.udpFaces = nil, nil
	d.udpMu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	for _, f := range faces {
		f.stop()
	}
}

// AttachUDPSession adds a datagram face's session registry, one per
// persistent udp interface (#1037).
func (d *Device) AttachUDPSession(r *SessionRegistry) {
	d.udpMu.Lock()
	defer d.udpMu.Unlock()
	d.udpSessions = append(d.udpSessions, r)
}

// UDPSessions returns every datagram face's session registry, in attach
// order.
func (d *Device) UDPSessions() []*SessionRegistry {
	d.udpMu.Lock()
	defer d.udpMu.Unlock()
	return append([]*SessionRegistry(nil), d.udpSessions...)
}

// SetDatagramLoss sets the share of datagrams every datagram face on the
// device drops, in percent, each way, and restarts the seeded sequence, so
// the same calls on a fresh face drop the same datagrams.
func (d *Device) SetDatagramLoss(pct int) {
	d.udpMu.Lock()
	faces := append([]*udpFace(nil), d.udpFaces...)
	d.udpMu.Unlock()
	for _, f := range faces {
		f.loss.set(pct)
	}
}

// udpIdle is how long a source's session outlives its last datagram. A
// node's endpoint asks every tick, so its session never idles out; a source
// that went away (a node restarted onto another port) stops being pushed to.
const udpIdle = 5 * time.Minute

// datagramLoss drops a seeded share of datagrams.
type datagramLoss struct {
	mu   sync.Mutex
	seed int64
	pct  int
	rng  *rand.Rand
}

func newDatagramLoss(seed int64, pct int) *datagramLoss {
	l := &datagramLoss{seed: seed}
	l.set(pct)
	return l
}

func (l *datagramLoss) set(pct int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pct = pct
	l.rng = rand.New(rand.NewSource(l.seed)) //nolint:gosec // a repeatable simulation, not a secret
}

// drop draws whether the next datagram is lost.
func (l *datagramLoss) drop() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pct <= 0 {
		return false
	}
	return l.rng.Intn(100) < l.pct
}

// lossSeed derives a face's loss seed from the device and the face.
func lossSeed(deviceID, face string) int64 {
	sum := sha256.Sum256([]byte("omniglass-sim-udp-loss\x00" + deviceID + "\x00" + face))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

// udpFace serves a persistent udp interface: the line protocol as
// datagrams (#1037). Each source address gets its own session from the
// registry, run by the same shell loop as the tcp face: a datagram's lines
// are its commands, each reply and push goes back as one datagram, and a
// source that subscribes is the destination its pushes are sent to.
type udpFace struct {
	d    *Device
	conn *net.UDPConn
	reg  *SessionRegistry
	port int
	loss *datagramLoss

	mu      sync.Mutex
	sources map[string]*udpSource
	stopped bool
}

// udpSource is one source address's session: its datagrams feed the shell
// loop's reader through a pipe, in order, without holding up the face.
type udpSource struct {
	in   chan []byte
	pw   *io.PipeWriter
	done chan struct{}
	once sync.Once
}

// end closes the source's pipe and stops its feed, once.
func (s *udpSource) end() {
	s.once.Do(func() {
		_ = s.pw.Close()
		close(s.done)
	})
}

// startUDPFace binds a datagram face and returns the address it bound.
func (d *Device) startUDPFace(port int, reg *SessionRegistry, name string, lossPct int) (net.Addr, error) {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	laddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	f := &udpFace{d: d, conn: conn, reg: reg, port: boundPort(conn.LocalAddr()),
		loss: newDatagramLoss(lossSeed(d.DeviceID, name), lossPct), sources: map[string]*udpSource{}}
	reg.setPort(f.port)
	d.udpMu.Lock()
	d.udpFaces = append(d.udpFaces, f)
	d.udpMu.Unlock()
	go f.serve()
	return conn.LocalAddr(), nil
}

func (f *udpFace) serve() {
	slog.Debug("device UDP face started", "device", f.d.Hostname, "addr", f.conn.LocalAddr().String())
	buf := make([]byte, 64*1024)
	for {
		n, from, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			if !strings.Contains(err.Error(), "closed") {
				slog.Error("device UDP face read error", "device", f.d.Hostname, "error", err)
			}
			f.stop()
			return
		}
		if f.loss.drop() {
			continue
		}
		src := f.source(from)
		if src == nil {
			continue
		}
		frame := append([]byte(nil), buf[:n]...)
		if delim := f.reg.cfg.ReadDelim(); len(frame) == 0 || frame[len(frame)-1] != delim {
			frame = append(frame, f.reg.cfg.LineEnding...)
		}
		select {
		case src.in <- frame:
		default: // the source's queue is full: lost, as a full socket buffer loses it
		}
	}
}

// source returns the session for a source address, opening one on its
// first datagram; nil when the registry refuses (its session limit).
func (f *udpFace) source(from *net.UDPAddr) *udpSource {
	key := from.String()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return nil
	}
	if s, ok := f.sources[key]; ok {
		return s
	}
	sess, err := f.reg.Open(key)
	if err != nil {
		slog.Debug("UDP face session refused", "device", f.d.Hostname, "remote", key, "error", err)
		return nil
	}
	pr, pw := io.Pipe()
	s := &udpSource{in: make(chan []byte, 64), pw: pw, done: make(chan struct{})}
	f.sources[key] = s
	to := &net.UDPAddr{IP: append(net.IP(nil), from.IP...), Port: from.Port, Zone: from.Zone}
	go func() {
		runShellSession(f.d, sess, bufio.NewReader(pr), datagramWriter{f: f, to: to}, f.port, "udp")
		_ = pr.Close()
	}()
	go f.feed(key, s)
	return s
}

// feed hands a source's datagrams to its shell loop, in order, and ends the
// session once the source has been quiet for udpIdle.
func (f *udpFace) feed(key string, s *udpSource) {
	idle := time.NewTimer(udpIdle)
	defer idle.Stop()
	for {
		select {
		case frame := <-s.in:
			if _, err := s.pw.Write(frame); err != nil {
				f.forget(key, s)
				return
			}
			if !idle.Stop() {
				<-idle.C
			}
			idle.Reset(udpIdle)
		case <-idle.C:
			f.forget(key, s)
			return
		case <-s.done:
			return
		}
	}
}

// forget ends a source's session: the pipe closes, and the shell loop reads
// the end and closes the session.
func (f *udpFace) forget(key string, s *udpSource) {
	f.mu.Lock()
	if f.sources[key] == s {
		delete(f.sources, key)
	}
	f.mu.Unlock()
	s.end()
}

// stop closes the face and ends every source's session.
func (f *udpFace) stop() {
	f.mu.Lock()
	if f.stopped {
		f.mu.Unlock()
		return
	}
	f.stopped = true
	sources := f.sources
	f.sources = map[string]*udpSource{}
	f.mu.Unlock()
	_ = f.conn.Close()
	for _, s := range sources {
		s.end()
	}
}

// datagramWriter sends each write (one reply or push line, as the shell
// loop queues them) as one datagram to its source, subject to the loss.
type datagramWriter struct {
	f  *udpFace
	to *net.UDPAddr
}

func (w datagramWriter) Write(p []byte) (int, error) {
	if w.f.loss.drop() {
		return len(p), nil
	}
	if _, err := w.f.conn.WriteToUDP(p, w.to); err != nil {
		return 0, err
	}
	return len(p), nil
}
