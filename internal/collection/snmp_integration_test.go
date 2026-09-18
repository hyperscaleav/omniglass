package collection_test

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The real SNMP getter against a real agent (the capability carve-out). The
// test agent speaks v2c GET from the BER spec up, independent of the client
// library, so a green run is a genuine cross-implementation exchange over a
// genuine UDP socket: request-id correlated, community adjudicated, values
// typed (an OctetString sysDescr, a TimeTicks sysUpTime).

// --- a minimal BER v2c agent ------------------------------------------------

type tlvReader struct {
	b []byte
}

func (r *tlvReader) read(t *testing.T) (tag byte, val []byte) {
	t.Helper()
	if len(r.b) < 2 {
		t.Fatalf("agent: truncated TLV")
	}
	tag = r.b[0]
	n, rest := berLen(t, r.b[1:])
	if len(rest) < n {
		t.Fatalf("agent: TLV length %d overruns buffer", n)
	}
	val, r.b = rest[:n], rest[n:]
	return tag, val
}

func berLen(t *testing.T, b []byte) (int, []byte) {
	t.Helper()
	if b[0] < 0x80 {
		return int(b[0]), b[1:]
	}
	n := int(b[0] & 0x7f)
	if n > 2 || len(b) < 1+n {
		t.Fatalf("agent: unsupported BER length")
	}
	v := 0
	for i := 0; i < n; i++ {
		v = v<<8 | int(b[1+i])
	}
	return v, b[1+n:]
}

func berInt(t *testing.T, val []byte) int64 {
	t.Helper()
	var v int64
	for _, b := range val {
		v = v<<8 | int64(b)
	}
	return v
}

func tlv(tag byte, val []byte) []byte {
	if len(val) < 0x80 {
		return append([]byte{tag, byte(len(val))}, val...)
	}
	if len(val) <= 0xff {
		return append([]byte{tag, 0x81, byte(len(val))}, val...)
	}
	out := []byte{tag, 0x82, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(val)))
	return append(out, val...)
}

func encInt(tag byte, v int64) []byte {
	var b []byte
	for {
		b = append([]byte{byte(v & 0xff)}, b...)
		v >>= 8
		if v == 0 || v == -1 {
			break
		}
	}
	if len(b) > 0 && b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return tlv(tag, b)
}

func encOID(t *testing.T, oid string) []byte {
	t.Helper()
	parts := strings.Split(oid, ".")
	if len(parts) < 2 {
		t.Fatalf("agent: bad oid %q", oid)
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		var v uint64
		for _, c := range p {
			v = v*10 + uint64(c-'0')
		}
		nums[i] = v
	}
	body := []byte{byte(nums[0]*40 + nums[1])}
	for _, v := range nums[2:] {
		var chunk []byte
		chunk = append(chunk, byte(v&0x7f))
		v >>= 7
		for v > 0 {
			chunk = append([]byte{byte(v&0x7f | 0x80)}, chunk...)
			v >>= 7
		}
		body = append(body, chunk...)
	}
	return tlv(0x06, body)
}

func decOID(t *testing.T, val []byte) string {
	t.Helper()
	if len(val) == 0 {
		return ""
	}
	var sb strings.Builder
	first := int(val[0])
	sb.WriteString(itoa(first / 40))
	sb.WriteByte('.')
	sb.WriteString(itoa(first % 40))
	var acc uint64
	for _, b := range val[1:] {
		acc = acc<<7 | uint64(b&0x7f)
		if b&0x80 == 0 {
			sb.WriteByte('.')
			sb.WriteString(itoa(int(acc)))
			acc = 0
		}
	}
	return sb.String()
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// startSNMPAgent serves v2c GET on a random UDP port: community "lab-public",
// sysDescr as an OctetString, sysUpTime as TimeTicks (8123456 hundredths).
// A wrong community is silence, exactly as real agents behave; an unknown OID
// answers noSuchObject (0x80).
func startSNMPAgent(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("agent: listen: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	values := map[string][]byte{
		"1.3.6.1.2.1.1.1.0": tlv(0x04, []byte("Boreal AirWall 3")), // sysDescr OctetString
		"1.3.6.1.2.1.1.3.0": encInt(0x43, 8123456),                 // sysUpTime TimeTicks
	}

	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			msg := &tlvReader{b: append([]byte(nil), buf[:n]...)}
			tag, body := msg.read(t)
			if tag != 0x30 {
				continue
			}
			seq := &tlvReader{b: body}
			vTag, vVal := seq.read(t)
			cTag, cVal := seq.read(t)
			pTag, pVal := seq.read(t)
			if vTag != 0x02 || berInt(t, vVal) != 1 || cTag != 0x04 || pTag != 0xa0 {
				continue // not a v2c GetRequest
			}
			if string(cVal) != "lab-public" {
				continue // wrong community: agents answer with silence
			}
			pdu := &tlvReader{b: pVal}
			_, ridVal := pdu.read(t)
			pdu.read(t) // error-status
			pdu.read(t) // error-index
			_, vbl := pdu.read(t)

			// Answer each requested OID.
			var vbs []byte
			vbList := &tlvReader{b: vbl}
			for len(vbList.b) > 0 {
				_, vb := vbList.read(t)
				one := &tlvReader{b: vb}
				_, oidVal := one.read(t)
				oid := decOID(t, oidVal)
				val, ok := values[oid]
				if !ok {
					val = tlv(0x80, nil) // noSuchObject
				}
				vbs = append(vbs, tlv(0x30, append(encOID(t, oid), val...))...)
			}
			resp := tlv(0x30, append(append(
				encInt(0x02, 1),
				tlv(0x04, []byte("lab-public"))...),
				tlv(0xa2, append(append(append(
					tlv(0x02, ridVal),
					encInt(0x02, 0)...),
					encInt(0x02, 0)...),
					tlv(0x30, vbs)...))...))
			_, _ = pc.WriteTo(resp, addr)
		}
	}()
	return pc.LocalAddr().String()
}

func TestSNMPGetterReal(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	g := collection.NewSNMPGetter()
	ctx := context.Background()
	addr := startSNMPAgent(t)

	t.Run("v2c scalars come back typed and correlated", func(t *testing.T) {
		got, err := g.Get(ctx, addr, "lab-public", []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0"}, 3*time.Second)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got["1.3.6.1.2.1.1.1.0"] != "Boreal AirWall 3" {
			t.Fatalf("sysDescr = %q", got["1.3.6.1.2.1.1.1.0"])
		}
		if got["1.3.6.1.2.1.1.3.0"] != "8123456" {
			t.Fatalf("sysUpTime = %q, want the timeticks in decimal", got["1.3.6.1.2.1.1.3.0"])
		}
	})

	t.Run("an unknown OID is absent, not a placeholder", func(t *testing.T) {
		got, err := g.Get(ctx, addr, "lab-public", []string{"1.3.6.1.2.1.1.5.0"}, 3*time.Second)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if _, ok := got["1.3.6.1.2.1.1.5.0"]; ok {
			t.Fatalf("noSuchObject rendered a value: %v", got)
		}
	})

	t.Run("a wrong community is an error, not silence read as data", func(t *testing.T) {
		if _, err := g.Get(ctx, addr, "wrong-horse", []string{"1.3.6.1.2.1.1.1.0"}, 700*time.Millisecond); err == nil {
			t.Fatal("wrong community answered")
		}
	})
}
