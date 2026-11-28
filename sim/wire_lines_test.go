//go:build linux

package sim

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Line protocols in the golden wire transcripts (#2833). A device that speaks a
// line protocol (NVP over TCP, a CLI over SSH) is held to the lines it
// answered: each step is one line sent, the reply it drew, the lines pushed to a
// subscriber after it, and whether and how the device's state moved. A step is
// either played on the device's raw TCP face (the bytes a driver sees, CRLF and
// pushes included) or run direct against the device's command table (a shell's
// verb, which carries no framing of its own). The device's own list of
// commands, the order a shell's help names them in, is recorded beside.

// wireLine is one step of a family's line script.
type wireLine struct {
	Name string
	// Line is sent on the TCP face; Direct runs it through the device's command
	// table instead, as a shell session would.
	Line   string
	Direct bool
	// Then is an HTTP request played after the line is sent and before the
	// pushes are read: how a subscriber hears of a change made elsewhere.
	Then *wireStep
	// NoReply says the line draws no reply (a blank line), so none is waited
	// for.
	NoReply bool
	// Hand is state set on the device before the line, an operator's hand.
	Hand map[string]any
	// Snapshot records the whole state after the step.
	Snapshot bool
}

// wireLineResult is what one line step put on the wire.
type wireLineResult struct {
	Name        string          `json:"name"`
	Line        string          `json:"line"`
	Direct      bool            `json:"direct,omitempty"`
	Reply       string          `json:"reply"`
	Pushed      []string        `json:"pushed,omitempty"`
	Action      string          `json:"action,omitempty"`
	Subject     string          `json:"subject,omitempty"`
	Token       string          `json:"token,omitempty"`
	Failed      string          `json:"failed,omitempty"`
	Unknown     bool            `json:"unknown,omitempty"`
	StateChange bool            `json:"state_change"`
	State       json.RawMessage `json:"state,omitempty"`
}

// lineQuiet is how long a connection is silent before the pushes of a step are
// taken as all in: loopback, so a push follows its reply within microseconds.
const lineQuiet = 80 * time.Millisecond

// runWireLines plays a family's line script on a started device and returns the
// results and the device's command list.
func runWireLines(t *testing.T, d *Device, face string, steps []wireLine) []wireLineResult {
	t.Helper()
	if face == "" {
		face = "nvp"
	}
	var (
		conn net.Conn
		rd   *bufio.Reader
	)
	for _, st := range steps {
		if !st.Direct && conn == nil {
			c, err := net.DialTimeout("tcp", faceAddr(t, d, face), 2*time.Second)
			if err != nil {
				t.Fatalf("dial the %s face: %v", face, err)
			}
			t.Cleanup(func() { _ = c.Close() })
			conn, rd = c, bufio.NewReader(c)
		}
	}
	out := make([]wireLineResult, 0, len(steps))
	for _, st := range steps {
		if st.Hand != nil {
			handSet(t, d, st.Hand)
		}
		before := stateJSON(d)
		res := wireLineResult{Name: st.Name, Line: st.Line, Direct: st.Direct}
		if st.Direct {
			r, ok, err := d.RunCommand(st.Line)
			res.Reply, res.Action, res.Subject, res.Token = r.Response, string(r.Action), r.Subject, r.Token
			res.Unknown = !ok
			if err != nil {
				res.Failed = err.Error()
			}
			if st.Then != nil {
				wireRequest(t, d, *st.Then)
			}
		} else {
			if st.Line != "" {
				if _, err := conn.Write([]byte(st.Line + "\r\n")); err != nil {
					t.Fatalf("%s: %v", st.Name, err)
				}
				if !st.NoReply {
					// The reply is read before anything else is done, so a push
					// a later step draws cannot overtake it.
					res.Reply = readReply(conn, rd)
				}
			}
			if st.Then != nil {
				wireRequest(t, d, *st.Then)
			}
			res.Pushed = readLines(conn, rd)
		}
		after := stateJSON(d)
		res.StateChange = string(before) != string(after)
		if st.Snapshot {
			res.State = after
		}
		out = append(out, res)
	}
	return out
}

// replyWait is how long a reply is waited for.
const replyWait = 500 * time.Millisecond

// readReply reads the one line a request draws, "" when none arrives.
func readReply(conn net.Conn, rd *bufio.Reader) string {
	_ = conn.SetReadDeadline(time.Now().Add(replyWait))
	s, _ := rd.ReadString('\n')
	return strings.TrimRight(s, "\r\n")
}

// readLines reads every line the connection delivers until it falls quiet.
func readLines(conn net.Conn, rd *bufio.Reader) []string {
	var out []string
	for {
		_ = conn.SetReadDeadline(time.Now().Add(lineQuiet))
		s, err := rd.ReadString('\n')
		if s != "" {
			out = append(out, strings.TrimRight(s, "\r\n"))
		}
		if err != nil {
			return out
		}
	}
}

// handSet sets state paths on the device as an operator would, past any
// surface.
func handSet(t *testing.T, d *Device, set map[string]any) {
	t.Helper()
	s := d.State()
	for path, v := range set {
		if err := SetPath(s, path, v); err != nil {
			t.Fatal(err)
		}
	}
	d.SetState(s, d.Version+1)
}

// diffWireLines is what a replayed line script differs in from the golden.
func diffWireLines(t *testing.T, product string, got, want []wireLineResult) []string {
	t.Helper()
	var out []string
	if len(got) != len(want) {
		return []string{fmt.Sprintf("%s: %d line steps replayed, %d recorded", product, len(got), len(want))}
	}
	for i, w := range want {
		g := got[i]
		g.State, w.State = canonRaw(t, g.State), canonRaw(t, w.State)
		if !reflect.DeepEqual(g, w) {
			out = append(out, fmt.Sprintf("%s: line %q:\n got %s\nwant %s", product, w.Name, lineSummary(g), lineSummary(w)))
		}
	}
	return out
}

// canonRaw is a recorded state in canonical form, nil when none was recorded.
func canonRaw(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.RawMessage(canon(t, raw))
}

// handPath sets one state path on a device as an operator's hand would, past
// any surface: what a room day's operator does mid-walk.
func handPath(d *Device, path string, v any) {
	s := d.State()
	if err := SetPath(s, path, v); err != nil {
		panic(err)
	}
	d.SetState(s, d.Version+1)
}

// wireLine1 is a line sent on the TCP face.
func wireLine1(name, line string) wireLine { return wireLine{Name: name, Line: line} }

// wireLineSnap is a line sent on the TCP face whose state after is recorded.
func wireLineSnap(name, line string) wireLine {
	return wireLine{Name: name, Line: line, Snapshot: true}
}

// wireCmd is a verb run direct against the command table, state recorded.
func wireCmd(name, line string) wireLine {
	return wireLine{Name: name, Line: line, Direct: true, Snapshot: true}
}

// lineSummary is a line step's result as one readable line, its state as text.
func lineSummary(r wireLineResult) string {
	state := string(r.State)
	r.State = nil
	b, _ := json.Marshal(r)
	return string(b) + " state " + state
}
