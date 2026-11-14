//go:build !windows && !linux && !darwin

package lab

// processStart is unknown here: a lab's owner is judged by its pid alone.
func processStart(int) (string, bool) { return "", false }
