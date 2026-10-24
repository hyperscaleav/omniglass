//go:build linux

package collection

import (
	"net"

	"golang.org/x/sys/unix"
)

// enableErrorQueue turns on the socket's error queue (IP_RECVERR, or
// IPV6_RECVERR), so an unconnected udp socket hears the host's
// port-unreachable report for a datagram it sent: the next read returns
// ECONNREFUSED rather than the report being dropped.
func enableErrorQueue(c *net.UDPConn, v6 bool) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) {
		if v6 {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_RECVERR, 1)
			return
		}
		serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVERR, 1)
	}); err != nil {
		return err
	}
	return serr
}

// drainErrorQueue reads every queued report off the socket's error queue:
// a report left queued holds receive buffer, and enough of them would crowd
// out the datagrams themselves.
func drainErrorQueue(c *net.UDPConn) {
	rc, err := c.SyscallConn()
	if err != nil {
		return
	}
	buf, oob := make([]byte, 512), make([]byte, 512)
	_ = rc.Read(func(fd uintptr) bool {
		for {
			if _, _, _, _, err := unix.Recvmsg(int(fd), buf, oob, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT); err != nil {
				return true // empty (EAGAIN), or nothing more to read
			}
		}
	})
}
