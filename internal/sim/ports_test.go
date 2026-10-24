//go:build linux

package sim

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// servingSide is one simulator serving one device, with the hits and session
// events its engine reported.
type servingSide struct {
	dev      *Device
	hits     chan Hit
	sessions chan SessionEvent
}

// Two simulators serve the same device address at once (#1066), the shape of
// two `go test` runs side by side on one host. Under EphemeralPorts every
// face of both binds with no collision and reports the address it got, and
// each answers there on its own engine, reporting the port it bound.
func TestTwoEnginesServeOneAddressSideBySide(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	ip := "127.88.10.1"
	start := func(id string) (servingSide, error) {
		e := NewEngine(store, profiles.Templates())
		e.SetPortMap(EphemeralPorts)
		t.Cleanup(e.StopAll)
		s := servingSide{hits: make(chan Hit, 256), sessions: make(chan SessionEvent, 64)}
		e.SetHitReporter(func(h Hit) {
			select {
			case s.hits <- h:
			default:
			}
		})
		e.SetSessionPublisher(func(ev SessionEvent) {
			select {
			case s.sessions <- ev:
			default:
			}
		})
		if err := e.Sync([]DeviceSnapshot{dspSnapshot(id, ip)}); err != nil {
			return s, err
		}
		if n := len(e.Devices()); n != 1 {
			return s, fmt.Errorf("%s: engine serves %d devices, want 1", id, n)
		}
		s.dev = e.Devices()[0]
		return s, nil
	}

	// Both start at once, as two test processes would.
	var sides [2]servingSide
	var errs [2]error
	var wg sync.WaitGroup
	for i, id := range []string{"dsp-a", "dsp-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sides[i], errs[i] = start(id)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("two simulators on one address: %v", err)
		}
	}

	want := map[string]int{"http-api": 80, "ssh-console": 22, "telnet-console": 23, "qrc": 1710, "snmp": 161, "udp-echo": 9000}
	for _, s := range sides {
		faces := s.dev.Faces()
		if len(faces) != len(want) {
			t.Fatalf("%s reports faces %+v, want the six its profile declares", s.dev.DeviceID, faces)
		}
		for _, f := range faces {
			if declared, ok := want[f.Name]; !ok || f.Declared != declared {
				t.Errorf("%s face %+v, want one of %v at its declared port", s.dev.DeviceID, f, want)
			}
			if f.Addr.Addr().String() != ip || f.Addr.Port() == 0 {
				t.Errorf("%s face %s bound %s, want a port of its own on %s", s.dev.DeviceID, f.Name, f.Addr, ip)
			}
		}
	}
	for name := range want {
		a, _ := sides[0].dev.Face(name)
		b, _ := sides[1].dev.Face(name)
		if a.Addr == b.Addr {
			t.Errorf("both simulators report %s at %s", name, a.Addr)
		}
	}

	// Each face answers at the address it reported, on its own engine, and
	// the hit names the port it bound.
	for _, s := range sides {
		face := func(name string) Face { t.Helper(); return faceOf(t, s.dev, name) }
		for _, name := range []string{"http-api", "ssh-console", "telnet-console", "qrc"} {
			f := face(name)
			c, err := net.DialTimeout("tcp", f.Addr.String(), time.Second)
			if err != nil {
				t.Fatalf("%s %s at %s: %v", s.dev.DeviceID, name, f.Addr, err)
			}
			c.Close()
			awaitHit(t, s, fmt.Sprintf("a connect on %s", name), func(h Hit) bool {
				return h.Protocol == "tcp" && h.Method == "CONNECT" && h.Port == int(f.Addr.Port())
			})
		}

		// The line protocol's session events carry the port it bound.
		qrc := face("qrc")
		c, err := net.DialTimeout("tcp", qrc.Addr.String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		awaitSession(t, s, func(ev SessionEvent) bool { return ev.Kind == "open" && ev.Port == int(qrc.Addr.Port()) })
		c.Close()

		// A telnet command's hit carries the port the face bound.
		telnet := face("telnet-console")
		tc, err := net.DialTimeout("tcp", telnet.Addr.String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = tc.SetDeadline(time.Now().Add(3 * time.Second))
		rd := bufio.NewReader(telnetReader{r: tc})
		for _, step := range []struct{ prompt, send string }{{"login:", "admin"}, {"Password:", "polaris"}, {"ready", "show dsp"}} {
			if err := readUntil(rd, step.prompt); err != nil {
				t.Fatalf("%s telnet at %s, awaiting %q: %v", s.dev.DeviceID, telnet.Addr, step.prompt, err)
			}
			if _, err := tc.Write([]byte(step.send + "\r\n")); err != nil {
				t.Fatal(err)
			}
		}
		awaitHit(t, s, "a telnet command", func(h Hit) bool {
			return h.Protocol == "telnet" && h.Path == "show dsp" && h.Port == int(telnet.Addr.Port())
		})
		tc.Close()

		snmp := face("snmp")
		g := &gosnmp.GoSNMP{Target: ip, Port: snmp.Addr.Port(), Community: "public",
			Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1}
		if err := g.Connect(); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Get([]string{"1.3.6.1.4.1.55540.5.2.0"}); err != nil {
			t.Fatalf("%s snmp at %s: %v", s.dev.DeviceID, snmp.Addr, err)
		}
		g.Conn.Close()
		awaitHit(t, s, "an snmp get", func(h Hit) bool { return h.Protocol == "snmp" && h.Port == int(snmp.Addr.Port()) })

		echo := face("udp-echo")
		u, err := net.Dial("udp", echo.Addr.String())
		if err != nil {
			t.Fatal(err)
		}
		_ = u.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := u.Write([]byte("ping")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 16)
		if n, err := u.Read(buf); err != nil || string(buf[:n]) != "ping" {
			t.Fatalf("%s udp echo at %s = %q, %v", s.dev.DeviceID, echo.Addr, buf[:n], err)
		}
		u.Close()
		awaitHit(t, s, "a udp echo", func(h Hit) bool { return h.Protocol == "udp" && h.Port == int(echo.Addr.Port()) })
	}
}

// awaitHit waits for a hit on one side's engine, and fails if it never comes.
func awaitHit(t *testing.T, s servingSide, what string, match func(Hit) bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case h := <-s.hits:
			if h.DeviceID != s.dev.DeviceID {
				t.Fatalf("%s's engine reported a hit for %s", s.dev.DeviceID, h.DeviceID)
			}
			if match(h) {
				return
			}
		case <-deadline:
			t.Fatalf("%s's engine never reported %s", s.dev.DeviceID, what)
		}
	}
}

// awaitSession waits for a session event on one side's engine.
func awaitSession(t *testing.T, s servingSide, match func(SessionEvent) bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-s.sessions:
			if match(ev) {
				return
			}
		case <-deadline:
			t.Fatalf("%s's engine never reported the session event", s.dev.DeviceID)
		}
	}
}

// faceOf returns the named face a device's engine started, failing the test
// when it has none.
func faceOf(t testing.TB, d *Device, name string) Face {
	t.Helper()
	f, ok := d.Face(name)
	if !ok {
		t.Fatalf("%s serves no %s face; it serves %+v", d.DeviceID, name, d.Faces())
	}
	return f
}

// faceAddr is the host:port the named face bound.
func faceAddr(t testing.TB, d *Device, name string) string {
	t.Helper()
	return faceOf(t, d, name).Addr.String()
}

// facePort is the port the named face bound.
func facePort(t testing.TB, d *Device, name string) int {
	t.Helper()
	return int(faceOf(t, d, name).Addr.Port())
}
