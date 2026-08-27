package collection

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHProber is the layer-7 SSH probe boundary (#812), faked in unit tests. The
// two ladder facts only a real handshake observes: an accepting port whose
// daemon never completes the SSH key exchange is reached-but-not-responsive,
// and a completed exchange that refuses the credentials is
// responded-but-not-authenticated, each distinct from port-closed. An outcome
// is DATA; err is reserved for a target that could not be attempted at all.
type SSHProber interface {
	Probe(ctx context.Context, target string, cred SSHCredential, timeout time.Duration) (SSHResult, error)
}

// SSHCredential is the optional identity a probe authenticates with, read off
// the endpoint's params (the driver spec's secret references arrive with
// #813). With no password set the probe still proves responsiveness: the
// exchange completes and the server refuses the empty auth attempt, which IS
// the protocol answering.
type SSHCredential struct {
	Username string
	Password string
}

// SSHResult is one SSH probe observation. Dialed is the L4 rung; Responded
// says the SSH protocol answered (the key exchange reached authentication);
// Authenticated says the credential was accepted, and AuthAttempted
// distinguishes "refused" from "never tried with a real credential".
// HandshakeMS is set only when Responded. Reason classifies the failing rung.
type SSHResult struct {
	Dialed        bool
	Responded     bool
	AuthAttempted bool
	Authenticated bool
	DialMS        float64
	HandshakeMS   float64
	Reason        Reachability
}

// NewSSHProber returns the real SSH probe. Host keys are deliberately not
// verified: the probe measures whether the daemon answers and whether a
// credential is accepted, and pinning arrives with the driver spec's inputs.
func NewSSHProber() SSHProber { return sshProber{} }

type sshProber struct{}

func (sshProber) Probe(ctx context.Context, target string, cred SSHCredential, timeout time.Duration) (SSHResult, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var res SSHResult
	user := cred.Username
	if user == "" {
		user = "omniglass-probe"
	}
	var auth []ssh.AuthMethod
	if cred.Password != "" {
		auth = append(auth, ssh.Password(cred.Password))
		res.AuthAttempted = true
	}
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	dialer := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		if reason, ok := Classify(err); ok {
			res.Reason = reason
			return res, nil
		}
		return SSHResult{}, err // resolve/setup failure: inconclusive
	}
	res.Dialed = true
	res.DialMS = float64(time.Since(start)) / float64(time.Millisecond)
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, target, config)
	elapsed := float64(time.Since(start)) / float64(time.Millisecond)
	if err != nil {
		_ = conn.Close()
		// The exchange reached authentication and the server said no: the
		// protocol answered, so the responds rung is up and the auth rung is
		// down. Anything else on a dialed socket is the daemon not speaking
		// SSH to us in time: reached-but-not-responsive.
		if strings.Contains(err.Error(), "unable to authenticate") {
			res.Responded = true
			res.HandshakeMS = elapsed
			res.Reason = Responded
			return res, nil
		}
		res.Reason = Timedout
		if reason, ok := Classify(err); ok {
			res.Reason = reason
		}
		return res, nil
	}
	client := ssh.NewClient(c, chans, reqs)
	_ = client.Close()
	res.Responded = true
	res.Authenticated = true
	res.HandshakeMS = elapsed
	res.Reason = Responded
	if !res.AuthAttempted {
		// A server that accepts the empty auth attempt authenticated nothing
		// worth claiming; the rung stays unattempted.
		res.Authenticated = false
	}
	return res, nil
}

// String implements fmt.Stringer for test failure legibility.
func (r SSHResult) String() string {
	return fmt.Sprintf("dialed=%v responded=%v authAttempted=%v authenticated=%v reason=%s", r.Dialed, r.Responded, r.AuthAttempted, r.Authenticated, r.Reason)
}
