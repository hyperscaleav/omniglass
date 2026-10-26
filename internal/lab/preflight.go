package lab

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"strconv"
	"strings"
	"syscall"
)

// PreflightError is a check that failed before the lab created anything: its
// cause, and the fix, in a learner's words.
type PreflightError struct {
	Cause string
	Fix   string
	Err   error
}

func (e *PreflightError) Error() string {
	return e.Cause + "\n  To fix it: " + e.Fix
}

func (e *PreflightError) Unwrap() error { return e.Err }

const dockerInstallURL = "https://docs.docker.com/get-started/get-docker/"

// RemoteDaemon refuses a daemon on another machine: the console would be
// published on that machine's loopback, not this one's.
func RemoteDaemon(d Daemon) *PreflightError {
	return &PreflightError{
		Cause: fmt.Sprintf("Docker here points at a daemon on another machine (%s, from %s), and the lab publishes its console on the daemon's own loopback, which you could not reach from here.", d.Host, d.Source),
		Fix:   "point Docker at this machine's daemon (unset DOCKER_HOST, or `docker context use default`), or run the lab on the machine the daemon runs on.",
	}
}

// ClassifyPing turns a failed ping into the cause and the fix: no Docker
// found, Docker not running, or permission denied on the socket.
func ClassifyPing(d Daemon, err error) *PreflightError {
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || strings.Contains(msg, "permission denied"):
		return &PreflightError{
			Cause: fmt.Sprintf("Docker is installed, but this user may not use it: permission denied on %s.", d.Host),
			Fix:   "on Linux, add yourself to the docker group (`sudo usermod -aG docker $USER`, then sign out and back in), or use rootless Docker. On macOS and Windows, make sure Docker Desktop is running as you.",
			Err:   err,
		}
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOENT) ||
		strings.Contains(msg, "no such file") || strings.Contains(msg, "cannot find the file"):
		if d.Source == "the default socket" || d.Source == "the default named pipe" {
			return &PreflightError{
				Cause: "No Docker found: nothing answers at " + d.Host + ".",
				Fix:   "install Docker Desktop (macOS, Windows) or Docker Engine (Linux), start it, and run the lab again: " + dockerInstallURL,
				Err:   err,
			}
		}
		return &PreflightError{
			Cause: fmt.Sprintf("Docker is not running: %s (from %s) does not exist.", d.Host, d.Source),
			Fix:   "start Docker Desktop, or the Docker service (`sudo systemctl start docker`), and run the lab again.",
			Err:   err,
		}
	default:
		return &PreflightError{
			Cause: fmt.Sprintf("Docker is not running: %s (from %s) did not answer (%v).", d.Host, d.Source, err),
			Fix:   "start Docker Desktop, or the Docker service (`sudo systemctl start docker`), and run the lab again.",
			Err:   err,
		}
	}
}

// PortFree checks that the console's address is free on this machine. Port 0
// asks Docker to pick one, so it is always free.
func PortFree(listen string, port int) error {
	if port == 0 {
		return nil
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(listen, strconv.Itoa(port)))
	if err != nil {
		return &PreflightError{
			Cause: fmt.Sprintf("The console's port %d on %s is taken by another program.", port, listen),
			Fix:   fmt.Sprintf("stop whatever holds port %d, or run the lab on another port: omniglass lab run --port %d", port, port+1),
			Err:   err,
		}
	}
	return ln.Close()
}

// ProcessAlive reports whether a process with the pid runs on this host: the
// reaper's check that a lab's owner is gone.
func ProcessAlive(pid int) bool { return processAlive(pid) }
