package collection_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The ssh dialer's credential and channel paths over a real ssh server (the
// capability carve-out, #1032): a private key, keyboard-interactive answered
// by the login dialog, a question nothing declared (a one-time code) refused
// as terminal auth rather than retried, and the pty and exec channels the
// driver may declare. The simulated panel proves the password path.

type sshServer struct {
	addr string
	mu   sync.Mutex
	reqs []string // channel request types, in order
}

func (s *sshServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

// newSSHServer serves a line shell answering GET INPUT, authenticating by
// cfg (whose host key it adds).
func newSSHServer(t *testing.T, cfg *ssh.ServerConfig) *sshServer {
	t.Helper()
	_, hk, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(hk)
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := &sshServer{addr: ln.Addr().String()}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					ch, creqs, err := nc.Accept()
					if err != nil {
						continue
					}
					go func() {
						for r := range creqs {
							srv.mu.Lock()
							srv.reqs = append(srv.reqs, r.Type)
							srv.mu.Unlock()
							_ = r.Reply(true, nil)
						}
					}()
					go func() {
						defer ch.Close()
						rd := bufio.NewReader(ch)
						for {
							l, err := rd.ReadString('\n')
							if err != nil {
								return
							}
							if strings.TrimRight(l, "\r\n") == "GET INPUT" {
								_, _ = io.WriteString(ch, "INPUT hdmi1\r\n")
							}
						}
					}()
				}
			}(c)
		}
	}()
	return srv
}

func sshSessionTo(t *testing.T, addr string, auth *collection.SessionAuth, ch collection.ChannelOptions) (*collection.Session, chan collection.SessionTransition) {
	t.Helper()
	states := make(chan collection.SessionTransition, 32)
	s := collection.StartSession(collection.SessionConfig{
		Target: addr, Timeout: 2 * time.Second, Dialer: collection.NewSSHSessionDialer(),
		Auth: auth, Channel: ch,
		OnTransition: func(tr collection.SessionTransition) { states <- tr },
	})
	t.Cleanup(s.Close)
	return s, states
}

func TestSSHSessionPrivateKey(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire ssh test")
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := newSSHServer(t, &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() == "admin" && string(k.Marshal()) == string(sshPub.Marshal()) {
			return nil, nil
		}
		return nil, fmt.Errorf("no")
	}})
	s, states := sshSessionTo(t, srv.addr, &collection.SessionAuth{SSH: &collection.SSHAuth{
		Username: "admin", PrivateKey: string(pem.EncodeToMemory(block)), Pin: collection.NewHostKeyPin(""),
	}}, collection.ChannelOptions{PTY: true})
	awaitTransition(t, states, collection.SessionUp)
	if got, err := s.Exchange(context.Background(), "GET INPUT", &driver.Reply{Prefix: "INPUT "}, 2*time.Second); err != nil || got != "INPUT hdmi1" {
		t.Fatalf("poll over a key-authenticated pty shell = %q, %v", got, err)
	}
	if r := strings.Join(srv.requests(), ","); r != "pty-req,shell" {
		t.Fatalf("channel requests = %s, want pty-req,shell", r)
	}
}

func kiServer(t *testing.T, question, answer string) *sshServer {
	return newSSHServer(t, &ssh.ServerConfig{KeyboardInteractiveCallback: func(c ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		got, err := client("", "", []string{question}, []bool{false})
		if err != nil {
			return nil, err
		}
		if len(got) == 1 && got[0] == answer {
			return nil, nil
		}
		return nil, fmt.Errorf("no")
	}})
}

func TestSSHSessionKeyboardInteractiveThroughTheLoginDialog(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire ssh test")
	}
	srv := kiServer(t, "Password: ", "newtron")
	login := &collection.LoginDialog{Steps: []collection.LoginPrompt{{Expect: driver.Match{Regex: `^Password: ?$`}, Send: "newtron"}}}
	s, states := sshSessionTo(t, srv.addr, &collection.SessionAuth{Login: login, SSH: &collection.SSHAuth{Username: "admin", Pin: collection.NewHostKeyPin("")}},
		collection.ChannelOptions{Exec: "nvp"})
	awaitTransition(t, states, collection.SessionUp)
	if got, err := s.Exchange(context.Background(), "GET INPUT", &driver.Reply{Prefix: "INPUT "}, 2*time.Second); err != nil || got != "INPUT hdmi1" {
		t.Fatalf("poll over a keyboard-interactive exec channel = %q, %v", got, err)
	}
	if r := strings.Join(srv.requests(), ","); r != "exec" {
		t.Fatalf("channel requests = %s, want exec", r)
	}
}

// Review of #1032, finding 2: a question no login step answers (a one-time
// code) is a refusal retrying cannot fix: rejected as auth, never a retry
// loop against an account that may lock.
func TestSSHSessionUnansweredQuestionIsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire ssh test")
	}
	srv := kiServer(t, "Verification code: ", "123456")
	_, states := sshSessionTo(t, srv.addr, &collection.SessionAuth{SSH: &collection.SSHAuth{
		Username: "admin", Password: "newtron", Pin: collection.NewHostKeyPin(""),
	}}, collection.ChannelOptions{})
	tr := awaitTransition(t, states, collection.SessionRejected)
	if !strings.HasPrefix(tr.Reason, "auth: ") || !strings.Contains(tr.Reason, "Verification code") || strings.Contains(tr.Reason, "newtron") {
		t.Fatalf("rejected reason = %q", tr.Reason)
	}
}
