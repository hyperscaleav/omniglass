//go:build !linux

package collection

import "net"

// enableErrorQueue is Linux's alone: elsewhere an unconnected udp socket
// does not hear the host's port-unreachable report, so a closed port reads
// as a timeout.
func enableErrorQueue(*net.UDPConn, bool) error { return nil }

func drainErrorQueue(*net.UDPConn) {}
