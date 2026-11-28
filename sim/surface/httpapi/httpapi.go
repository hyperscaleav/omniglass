// Package httpapi is the HTTP status document the simulated devices repeat,
// in Go (#2831): GET /api/status answers a "device" identity block (model,
// serial, firmware, uptime) and then the family's own sections. Ten
// profiles' status templates open with that block, in one of two key orders.
// The document is built from typed values and encoding/json, laid out as the
// templates lay it out: two-space indent, the identity block and each Block
// section spread over lines, each Inline section compact on its own line.
package httpapi

import (
	"bytes"
	"encoding/json"

	"github.com/hyperscaleav/omniglass/sim/surface"
)

// Order is the key order of the identity block.
type Order int

const (
	// ModelFirst lists model, serial, firmware, uptime.
	ModelFirst Order = iota
	// UptimeFirst lists uptime, model, serial, firmware.
	UptimeFirst
)

// Identity is the "device" block.
type Identity struct {
	Model    string
	Serial   string
	Firmware string
	Uptime   int64
}

type modelFirst struct {
	Model    string `json:"model"`
	Serial   string `json:"serial"`
	Firmware string `json:"firmware"`
	Uptime   int64  `json:"uptime"`
}

type uptimeFirst struct {
	Uptime   int64  `json:"uptime"`
	Model    string `json:"model"`
	Serial   string `json:"serial"`
	Firmware string `json:"firmware"`
}

func (i Identity) value(o Order) any {
	if o == UptimeFirst {
		return uptimeFirst{i.Uptime, i.Model, i.Serial, i.Firmware}
	}
	return modelFirst{i.Model, i.Serial, i.Firmware, i.Uptime}
}

// Section is one top-level key after the identity block.
type Section struct {
	Key    string
	Value  any
	Inline bool
}

// Block is a section spread over lines, one key per line. Its Value is a
// struct, whose field order is the key order.
func Block(key string, v any) Section { return Section{Key: key, Value: v} }

// Inline is a section compact on one line. Its Value is a map or a struct;
// a map's keys come out sorted.
func Inline(key string, v any) Section { return Section{Key: key, Value: v, Inline: true} }

// Status renders the document, ending in a newline.
func Status(id Identity, order Order, sections ...Section) []byte {
	var b bytes.Buffer
	b.WriteString("{\n  \"device\": ")
	writeBlock(&b, id.value(order))
	for _, s := range sections {
		b.WriteString(",\n  ")
		b.Write(encode(s.Key, "", ""))
		b.WriteString(": ")
		if s.Inline {
			b.Write(encode(s.Value, "", ""))
		} else {
			writeBlock(&b, s.Value)
		}
	}
	b.WriteString("\n}\n")
	return b.Bytes()
}

func writeBlock(b *bytes.Buffer, v any) {
	b.Write(encode(v, "  ", "  "))
}

// encode is json.Marshal (or MarshalIndent, with an indent) without the
// HTML escaping, so a "&" in a value reaches the wire as the templates
// write it.
func encode(v any, prefix, indent string) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent(prefix, indent)
	}
	if err := enc.Encode(v); err != nil {
		return []byte("{}")
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// StatusRoute is GET /api/status over a family's typed state: read runs when
// the request arrives.
func StatusRoute(order Order, read func() (Identity, []Section)) surface.Route {
	return surface.Route{Method: "GET", Path: "/api/status", Serve: func() surface.Response {
		id, sections := read()
		return surface.Response{Status: 200, ContentType: "application/json", Body: Status(id, order, sections...)}
	}}
}
