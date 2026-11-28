//go:build linux

package sim

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/profile"
)

// The line-protocol half of the golden wire harness (#2835): what a family
// declares beside its HTTP and SNMP faces. The faces it serves (each
// interface's protocol, port, auth, framing, welcome and subscribable subjects),
// the bytes a persistent shell session writes for a script of lines and of state
// changes made while it listens (replies, pushes, the help listing), and the
// datagrams a UDP echo face returns. A family supplies Sessions and Echo in its
// wireFamily; the recorder and the replay run them with the rest of the script.
//
// A session runs the same shell loop the SSH, Telnet and raw-TCP listeners all
// run (runShellSession), over a pipe instead of a socket, so what is recorded is
// the loop's own bytes: the transport's login dialog is the transport's, held by
// its own tests.

// wireFace is one interface a family declares, as the engine reads it. The
// subjects a persistent face may push are not listed here: what a session is
// pushed is recorded by the sessions themselves.
type wireFace struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Port       int    `json:"port"`
	TLS        bool   `json:"tls,omitempty"`
	APISurface string `json:"api_surface,omitempty"`
	Auth       struct {
		Type, User, Password, Header, APIKey, Realm string
		Community, RefusedCommunity                 string
		Enforce                                     bool
		PublicPaths                                 []string
	} `json:"auth"`
	Persistent bool       `json:"persistent,omitempty"`
	LineEnding string     `json:"line_ending,omitempty"`
	FrameStart string     `json:"frame_start,omitempty"`
	FrameEnd   string     `json:"frame_end,omitempty"`
	Welcome    string     `json:"welcome,omitempty"`
	Limits     wireLimits `json:"limits,omitempty"`
	Reachable  string     `json:"reachable_when,omitempty"`
}

type wireLimits struct {
	MaxSubs, MinIntervalMs, MaxSessions, OutBuffer int
}

// wireFacesOf is the interfaces a catalog definition serves.
func wireFacesOf(ifaces []profile.Interface) []wireFace {
	out := make([]wireFace, 0, len(ifaces))
	for _, i := range ifaces {
		f := wireFace{Name: i.Name, Protocol: i.Protocol, Port: i.Port, TLS: i.TLS, APISurface: i.APISurface, Persistent: i.Persistent,
			LineEnding: i.LineEnding, FrameStart: i.FrameStart, FrameEnd: i.FrameEnd, Welcome: i.Welcome, Reachable: i.ReachableWhen}
		f.Auth.Type, f.Auth.User, f.Auth.Password, f.Auth.Header, f.Auth.APIKey, f.Auth.Realm = i.Auth.Type, i.Auth.DefaultUser, i.Auth.DefaultPassword, i.Auth.Header, i.Auth.APIKey, i.Auth.Realm
		f.Auth.Community, f.Auth.RefusedCommunity, f.Auth.Enforce, f.Auth.PublicPaths = i.Auth.Community, i.Auth.RefusedCommunity, i.Auth.Enforce, i.Auth.PublicPaths
		if i.Persistent {
			l := i.Limits.WithDefaults()
			f.Limits = wireLimits{l.MaxSubsPerSession, l.PublishMinIntervalMs, l.MaxSessionsPerDevice, l.OutBuffer}
		}
		out = append(out, f)
	}
	return out
}

// wireSession is a script played over one persistent face.
type wireSession struct {
	Face  string
	Steps []wireLineStep
}

// wireLineStep is one moment of a session: lines the client sends back to back,
// and a request made to the device while the session listens (a state change it
// may push). A step with neither collects what the device wrote on its own (the
// welcome banner).
type wireLineStep struct {
	Name string
	Send []string
	Do   *wireStep
}

// wireSessionStep is what the device wrote during one step.
type wireSessionStep struct {
	Name string   `json:"name"`
	Send []string `json:"send,omitempty"`
	Do   string   `json:"do,omitempty"`
	Out  string   `json:"out"`
}

type wireSessionResult struct {
	Face  string            `json:"face"`
	Steps []wireSessionStep `json:"steps"`
}

// wireEcho is what a UDP echo face answered a datagram with.
type wireEcho struct {
	Face string `json:"face"`
	Sent string `json:"sent"`
	Got  string `json:"got"`
}

// lockedBuf is the bytes a session wrote, readable while it still writes.
type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// take is what was written since the last take.
func (b *lockedBuf) take() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.buf.String()
	b.buf.Reset()
	return s
}

// wireQuiet is how long a session must write nothing before a step is over.
const wireQuiet = 120 * time.Millisecond

func (b *lockedBuf) settle() {
	last, since := -1, time.Now()
	for time.Since(since) < wireQuiet {
		if n := b.len(); n != last {
			last, since = n, time.Now()
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// registryOf is the session registry of the interface with this name.
func registryOf(t *testing.T, d *Device, iface profile.Interface) *SessionRegistry {
	t.Helper()
	switch iface.Protocol {
	case "ssh":
		return d.SSHSession()
	case "telnet":
		return d.TelnetSession()
	case "tcp":
		for _, r := range d.TCPSessions() {
			if r.cfg.Port == iface.Port {
				return r
			}
		}
	}
	t.Fatalf("%s: no session registry for the %s face", d.DeviceID, iface.Name)
	return nil
}

// playWireSession runs a script over one persistent face and returns what the
// device wrote at each step.
func playWireSession(t *testing.T, d *Device, ifaces []profile.Interface, s wireSession) wireSessionResult {
	t.Helper()
	var iface profile.Interface
	found := false
	for _, i := range ifaces {
		if i.Name == s.Face {
			iface, found = i, true
		}
	}
	if !found {
		t.Fatalf("%s declares no %s face", d.DeviceID, s.Face)
	}
	reg := registryOf(t, d, iface)
	sess, err := reg.Open("wire")
	if err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	out := &lockedBuf{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runShellSession(d, sess, bufio.NewReader(pr), out, iface.Port, iface.Protocol)
	}()
	defer func() {
		_ = pw.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("%s: the %s session never ended", d.DeviceID, s.Face)
		}
		reg.Close(sess)
	}()

	res := wireSessionResult{Face: s.Face}
	out.settle()
	res.Steps = append(res.Steps, wireSessionStep{Name: "connect", Out: out.take()})
	end := reg.cfg.LineEnding
	for _, st := range s.Steps {
		rec := wireSessionStep{Name: st.Name, Send: st.Send}
		if st.Do != nil {
			wireRequest(t, d, *st.Do)
			rec.Do = st.Do.Name
		}
		for _, line := range st.Send {
			if _, err := io.WriteString(pw, line+end); err != nil {
				break
			}
		}
		out.settle()
		rec.Out = out.take()
		res.Steps = append(res.Steps, rec)
	}
	return res
}

// playWireEcho sends one datagram to a UDP echo face and returns the answer.
func playWireEcho(t *testing.T, d *Device, face string) wireEcho {
	t.Helper()
	const sent = "wire-echo"
	c, err := net.Dial("udp", net.JoinHostPort(d.LoopbackIP.String(), strconv.Itoa(facePort(t, d, face))))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte(sent)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("%s: the %s face did not echo: %v", d.DeviceID, face, err)
	}
	return wireEcho{Face: face, Sent: sent, Got: string(buf[:n])}
}

// collectWireLines gathers the line-protocol half of a transcript: the faces,
// each session and each echo.
func collectWireLines(t *testing.T, fam wireFamily, product string, tr *wireTranscript) {
	t.Helper()
	if len(fam.Sessions) == 0 && len(fam.Echo) == 0 && !fam.Faces {
		return
	}
	e := fam.Engine(t)
	r, ok := e.cat.ForProduct(product)
	if !ok {
		t.Fatalf("the catalog simulates no %s", product)
	}
	ifaces := r.Definition.Surfaces()
	if fam.Faces {
		tr.Faces = wireFacesOf(ifaces)
	}
	for _, s := range fam.Sessions {
		// A session starts from a device at rest, as an HTTP script does.
		d := wireDevice(t, fam, product)
		tr.Sessions = append(tr.Sessions, playWireSession(t, d, ifaces, s))
	}
	if len(fam.Echo) > 0 {
		d := wireDevice(t, fam, product)
		for _, face := range fam.Echo {
			tr.Echo = append(tr.Echo, playWireEcho(t, d, face))
		}
	}
}

// diffWireLines is what a replayed line-protocol half differs in, one line each.
func diffWireLines(product string, got, want wireTranscript) []string {
	var out []string
	if !reflect.DeepEqual(got.Faces, want.Faces) {
		out = append(out, "faces differ:\n got "+jsonOf(got.Faces)+"\nwant "+jsonOf(want.Faces))
	}
	if len(got.Sessions) != len(want.Sessions) {
		return append(out, product+": "+strconv.Itoa(len(got.Sessions))+" sessions replayed, "+strconv.Itoa(len(want.Sessions))+" recorded")
	}
	for i, w := range want.Sessions {
		g := got.Sessions[i]
		if g.Face != w.Face || len(g.Steps) != len(w.Steps) {
			out = append(out, product+": the "+w.Face+" session has a different shape")
			continue
		}
		for j, ws := range w.Steps {
			if g.Steps[j].Out != ws.Out {
				out = append(out, product+": "+w.Face+": "+ws.Name+": the device wrote\n got "+strconv.Quote(g.Steps[j].Out)+"\nwant "+strconv.Quote(ws.Out))
			}
		}
	}
	if !reflect.DeepEqual(got.Echo, want.Echo) {
		out = append(out, product+": the echo faces answered "+jsonOf(got.Echo)+", recorded "+jsonOf(want.Echo))
	}
	return out
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
