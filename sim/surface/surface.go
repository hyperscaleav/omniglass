// Package surface is what a Go device family declares a protocol surface
// with (#2831): the commands a line protocol answers, the subjects it pushes,
// the OIDs an SNMP agent serves and the routes an HTTP API answers, each a Go
// function of typed state rather than a template string. The sub-packages
// (nvp, snmp, httpapi) are the shared declarations; the simulator's protocol
// servers serve whatever these types describe, through the seams in
// sim/gosurface.go.
//
// A surface owns no state and no I/O. It reads and writes the family's typed
// state through the accessors it is built with, and says what changed in a
// Reply, so the server (not the surface) decides how a change fans out.
package surface

import "encoding/json"

// Action is the persistent-session verb a command carries.
type Action string

const (
	ActionNone        Action = ""
	ActionSubscribe   Action = "subscribe"
	ActionUnsubscribe Action = "unsubscribe"
)

// Change is one state write a command made: the dotted state path a
// subscriber watches, and the value now there.
type Change struct {
	Path  string
	Value any
}

// Reply is a line command's answer: the text to send (the server adds the
// wire envelope), the subscription verb it carries, and the state it changed.
type Reply struct {
	Text       string
	Action     Action
	Subject    string
	Token      string
	IntervalMs int
	Changes    []Change
}

// Commander answers a line protocol's commands. Commands lists every line it
// answers, in the order a help listing shows them; Handle answers one line,
// and reports false for a line that is none of its own.
type Commander interface {
	Commands() []string
	Handle(line string) (Reply, bool)
}

// Subject is a state path a session may subscribe to, with the line it
// pushes when the path changes. Push gets the new value.
type Subject struct {
	Match string // the subject a subscribe command names
	Path  string // the state path it watches
	Token string // the default publish token
	Push  func(value any) string
}

// Raw renders a pushed value bare: a string as itself, anything else as its
// JSON form. It is what a line protocol prints for a value it does not quote.
func Raw(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// Response is an HTTP answer.
type Response struct {
	Status      int
	ContentType string
	Body        []byte
}

// Route is one HTTP endpoint: Serve is a function of the family's state,
// read when the request arrives.
type Route struct {
	Method string
	Path   string
	Serve  func() Response
}

// Kind is the SNMP type an OID answers with.
type Kind int

const (
	KindOctetString Kind = iota
	KindInteger
	KindGauge32
	KindCounter32
	KindCounter64
	KindTimeTicks
)

// OID is one scalar an SNMP agent serves. Get returns the Go type the Kind
// names: string, int, uint, uint, uint64 or uint32 (hundredths of a second).
type OID struct {
	OID  string
	Kind Kind
	Get  func() any
}

// Text is an OctetString OID.
func Text(oid string, get func() string) OID {
	return OID{OID: oid, Kind: KindOctetString, Get: func() any { return get() }}
}

// Integer is an Integer OID.
func Integer(oid string, get func() int) OID {
	return OID{OID: oid, Kind: KindInteger, Get: func() any { return get() }}
}

// Gauge is a Gauge32 OID.
func Gauge(oid string, get func() uint) OID {
	return OID{OID: oid, Kind: KindGauge32, Get: func() any { return get() }}
}

// Counter is a Counter32 OID.
func Counter(oid string, get func() uint) OID {
	return OID{OID: oid, Kind: KindCounter32, Get: func() any { return get() }}
}

// Ticks is a TimeTicks OID, in hundredths of a second.
func Ticks(oid string, get func() uint32) OID {
	return OID{OID: oid, Kind: KindTimeTicks, Get: func() any { return get() }}
}
