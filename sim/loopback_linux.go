//go:build linux

package sim

import (
	"fmt"
	"os/exec"
)

func addLoopbackIP(ip string) error {
	cmd := exec.Command("ip", "addr", "add", ip+"/32", "dev", "lo")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, out)
	}
	return nil
}
