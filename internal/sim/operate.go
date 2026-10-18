package sim

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// methodToken is an HTTP method an operator may fire: letters only.
var methodToken = regexp.MustCompile(`^[A-Z]+$`)

// operateBodyCap bounds an operation's answer: enough for any profile's
// status document, small enough to cross the bus and render in a drawer.
const operateBodyCap = 64 << 10

// Operation is one request an operator fires at a simulated device from the
// console: an HTTP call, a command line, or an SNMP get.
type Operation struct {
	Kind     string `json:"kind"` // http, command, snmp
	Method   string `json:"method,omitempty"`
	Path     string `json:"path,omitempty"`
	Body     string `json:"body,omitempty"`
	Command  string `json:"command,omitempty"`
	Protocol string `json:"protocol,omitempty"` // the command's interface protocol, for the hit
	OID      string `json:"oid,omitempty"`
}

// OperationResult is the device's answer.
type OperationResult struct {
	Status    int    `json:"status,omitempty"` // HTTP only
	Body      string `json:"body"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
	TookMs    int64  `json:"took_ms"`
}

// Operate answers op from the device itself, in process: the same routes,
// commands and OID map its listeners serve, against its live state. A
// command's mutations apply, so firing one changes the device the collector
// polls. It runs beside the listeners rather than through them, so an
// impairment on the device's address does not stop it: the console can
// inspect a device that is unplugged. Each operation counts as a request to
// the device, so the console's fire blinks it.
func (d *Device) Operate(op Operation) OperationResult {
	start := time.Now()
	var res OperationResult
	switch op.Kind {
	case "http":
		res = d.operateHTTP(op)
	case "command":
		res = d.operateCommand(op)
	case "snmp":
		res = d.operateSNMP(op)
	default:
		res = OperationResult{Error: fmt.Sprintf("unknown operation kind %q", op.Kind)}
	}
	if len(res.Body) > operateBodyCap {
		res.Body, res.Truncated = res.Body[:operateBodyCap], true
	}
	res.TookMs = time.Since(start).Milliseconds()
	return res
}

func (d *Device) operateHTTP(op Operation) OperationResult {
	method := strings.ToUpper(op.Method)
	if method == "" {
		method = http.MethodGet
	}
	if !methodToken.MatchString(method) {
		return OperationResult{Error: fmt.Sprintf("%q is not an HTTP method", op.Method)}
	}
	// httptest.NewRequest panics on what it cannot parse, so the path is
	// checked first: an operator's typo is an error, never a crash.
	u, err := url.ParseRequestURI(op.Path)
	if err != nil || !strings.HasPrefix(op.Path, "/") || strings.ContainsAny(op.Path, " \t\r\n") {
		return OperationResult{Error: fmt.Sprintf("%q is not a request path", op.Path)}
	}
	req := httptest.NewRequest(method, u.RequestURI(), strings.NewReader(op.Body))
	req.RemoteAddr = "console"
	switch d.HTTPAuth.Mode {
	case HTTPAuthBasic:
		req.SetBasicAuth(d.HTTPAuth.User, d.HTTPAuth.Password)
	case HTTPAuthHeader:
		h := d.HTTPAuth.HeaderName
		if h == "" {
			h = defaultAPIKeyHeader
		}
		req.Header.Set(h, d.HTTPAuth.APIKey)
	}
	if op.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	d.ServeHTTP(w, req)
	return OperationResult{Status: w.Code, Body: w.Body.String()}
}

func (d *Device) operateCommand(op Operation) OperationResult {
	line := strings.TrimSpace(op.Command)
	// A command matched by its bytes is listed, and fired, as "hex" and its
	// pairs.
	if pairs, ok := strings.CutPrefix(line, "hex "); ok {
		b, err := hex.DecodeString(strings.Join(strings.Fields(pairs), ""))
		if err != nil {
			return OperationResult{Error: fmt.Sprintf("%q is not hex pairs", pairs)}
		}
		line = string(b)
	}
	r, ok, err := d.RunCommand(line)
	status := 200
	out := OperationResult{Body: r.Response}
	switch {
	case err != nil:
		out, status = OperationResult{Error: err.Error()}, 500
	case !ok:
		out, status = OperationResult{Error: fmt.Sprintf("the device has no command %q", line)}, 404
	}
	proto := op.Protocol
	if proto == "" {
		proto = "tcp"
	}
	d.publishOperation(proto, "EXEC", line, status)
	return out
}

func (d *Device) operateSNMP(op Operation) OperationResult {
	oid := strings.TrimPrefix(strings.TrimSpace(op.OID), ".")
	path, ok := d.OIDMap[oid]
	if !ok {
		d.publishOperation("snmp", "GET", oid, 404)
		return OperationResult{Error: fmt.Sprintf("noSuchObject: the device has no OID %s", oid)}
	}
	v, err := d.snmpValue(oid, path, snmpTypeFor(path))
	if err != nil {
		d.publishOperation("snmp", "GET", oid, 500)
		return OperationResult{Error: err.Error()}
	}
	d.publishOperation("snmp", "GET", oid, 200)
	return OperationResult{Body: fmt.Sprint(v)}
}

func (d *Device) publishOperation(proto, method, path string, status int) {
	if d.RequestPublisher == nil {
		return
	}
	d.RequestPublisher(RequestEvent{DeviceID: d.DeviceID, Hostname: d.Hostname, Protocol: proto,
		Method: method, Path: path, Status: status, Remote: "console"})
}
