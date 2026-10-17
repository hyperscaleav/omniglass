//go:build !linux

package sim

import "fmt"

func addLoopbackIP(ip string) error {
	return fmt.Errorf("loopback IP management not implemented on this platform")
}
