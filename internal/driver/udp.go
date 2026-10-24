package driver

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// ListenPortInput is a udp driver's input naming the local port its
// endpoint binds (#1037, decision 5 of #1076): each endpoint binds its own,
// and its requests share it, so a reply and a push arrive on one socket. The
// operator configures the component to send there (and opens the node's
// firewall). Absent, the node binds a port of its own choosing, which suits a
// component that answers to a request's source. It is derived into the
// endpoint's params as host and port are, so it is part of the connection
// fingerprint.
const ListenPortInput = "listen-port"

// validateUDPInputs refuses a listen-port input the engine could not read:
// one off udp, one that is not a number, or a default that is not a port.
func (s *Spec) validateUDPInputs() error {
	for _, in := range s.Inputs {
		if in.Name != ListenPortInput {
			continue
		}
		if s.Transport != "udp" {
			return fmt.Errorf("driver: input %s names a udp endpoint's local port, and %q has none", ListenPortInput, s.Transport)
		}
		if in.Kind != "number" {
			return fmt.Errorf("driver: input %s is a number (a port, 1 to 65535)", ListenPortInput)
		}
		if in.Default != "" {
			if err := checkPort(in.Default); err != nil {
				return fmt.Errorf("driver: input %s default: %v", ListenPortInput, err)
			}
		}
	}
	if s.Transport == "udp" {
		// An open udp port cannot be detected: the transport rung is the
		// driver's own request answered, so a udp driver names it.
		l := ResolveLadder(s.Transport, s.Ladder, s.Session)
		if l.Transport == RungRequest && (l.Request == nil || l.Request.Empty()) {
			return fmt.Errorf("driver: a udp driver names its request rung (ladder.transport.request): an open udp port cannot be detected, so the rung is a request answered")
		}
	}
	return nil
}

func checkPort(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%q is not a port (1 to 65535)", v)
	}
	return nil
}

// ParseListenPort reads a listen port off its params value: a JSON number or
// a numeral, 1 to 65535. Absent, null and "" are unset and read 0. It is the
// one reading the write path and the node share, so a value the write
// accepts is one the node binds.
func ParseListenPort(raw json.RawMessage) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	refused := fmt.Errorf("%s %s is not a port (1 to 65535)", ListenPortInput, raw)
	var n float64
	var s string
	switch {
	case json.Unmarshal(raw, &s) == nil:
		if s == "" {
			return 0, nil
		}
		i, err := strconv.Atoi(s)
		if err != nil {
			return 0, refused
		}
		n = float64(i)
	case json.Unmarshal(raw, &n) == nil:
	default:
		return 0, refused
	}
	if n != math.Trunc(n) || n < 1 || n > 65535 {
		return 0, refused
	}
	return int(n), nil
}
