package collection_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The real SSH probe against a real in-process daemon (the capability
// carve-out): a genuine key exchange over a genuine socket, with password auth
// the server actually adjudicates, is the only tier that can prove
// responded-but-not-authenticated is distinct from reached-but-not-responsive.

// startSSHServer runs a minimal password-auth SSH daemon on a random port and
// returns its address. It accepts exactly probe/right-horse and refuses
// everything else, which is all the ladder needs adjudicated.
func startSSHServer(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if meta.User() == "probe" && string(pass) == "right-horse" {
				return nil, nil
			}
			return nil, &ssh.ServerAuthError{}
		},
	}
	config.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				sc, chans, reqs, err := ssh.NewServerConn(c, config)
				if err != nil {
					return // an auth refusal lands here; the client saw it
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					_ = ch.Reject(ssh.UnknownChannelType, "probe only")
				}
				_ = sc.Close()
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestSSHProberReal(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	p := collection.NewSSHProber()
	ctx := context.Background()

	addr := startSSHServer(t)

	t.Run("no credential: the exchange answers, nothing is authenticated", func(t *testing.T) {
		res, err := p.Probe(ctx, addr, collection.SSHCredential{}, 3*time.Second)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if !res.Dialed || !res.Responded {
			t.Fatalf("want dialed+responded, got %s", res)
		}
		if res.AuthAttempted || res.Authenticated {
			t.Fatalf("no credential was supplied, got %s", res)
		}
		if res.HandshakeMS <= 0 {
			t.Fatalf("want a positive handshake time, got %+v", res)
		}
	})

	t.Run("wrong credential: responded but not authenticated", func(t *testing.T) {
		res, err := p.Probe(ctx, addr, collection.SSHCredential{Username: "probe", Password: "wrong-horse"}, 3*time.Second)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if !res.Responded || !res.AuthAttempted || res.Authenticated {
			t.Fatalf("want responded+attempted+refused, got %s", res)
		}
	})

	t.Run("right credential: authenticated", func(t *testing.T) {
		res, err := p.Probe(ctx, addr, collection.SSHCredential{Username: "probe", Password: "right-horse"}, 3*time.Second)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if !res.Responded || !res.AuthAttempted || !res.Authenticated {
			t.Fatalf("want authenticated, got %s", res)
		}
	})

	t.Run("reached but not responsive: an accepting port that never speaks SSH", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer func() { _ = ln.Close() }()
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
			}
		}()
		res, err := p.Probe(ctx, ln.Addr().String(), collection.SSHCredential{}, 700*time.Millisecond)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if !res.Dialed || res.Responded {
			t.Fatalf("want dialed and NOT responded, got %s", res)
		}
	})
}
