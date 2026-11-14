//go:build linux

package lab

import (
	"os"
	"strconv"
	"strings"
)

// processStart is the pid's start time: field 22 of /proc/<pid>/stat, in
// clock ticks since boot.
func processStart(pid int) (string, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	return parseStatStart(string(b))
}

// parseStatStart reads the start time out of a /proc/<pid>/stat line. The
// command name (field 2) may hold spaces and parentheses, so the fields are
// counted from the last ")": the state is field 3, the start time field 22.
func parseStatStart(stat string) (string, bool) {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return "", false
	}
	fields := strings.Fields(stat[i+1:])
	const startIndex = 22 - 3
	if len(fields) <= startIndex {
		return "", false
	}
	return fields[startIndex], true
}
