package node_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/nettest"
	"github.com/hyperscaleav/omniglass/internal/node"
)

// A server URL can carry a user:password, and a claim that fails names the URL
// it was sent to. node run prints that error and sim run logs it on every retry
// while its server is still coming up, so a password in OMNIGLASS_SERVER
// reached the screen and the container logs (#979's review).

// TestAnUnreachableClaimNeverEchoesTheServersPassword: net/http masks the
// password in its own error, but the claim wrapped the raw URL around it.
func TestAnUnreachableClaimNeverEchoesTheServersPassword(t *testing.T) {
	server := "http://operator:hunter2@" + nettest.RefusedAddr(t)
	_, err := node.Claim(context.Background(), server, "edge-hq", "ogp_enroll")
	if err == nil {
		t.Fatal("a claim to a refused address succeeded")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the claim error %q carries the server's password", err)
	}
}

// TestAMalformedClaimURLNeverEchoesItsPassword: a password with a reserved
// character does not parse, and the parser's error quotes the whole URL.
func TestAMalformedClaimURLNeverEchoesItsPassword(t *testing.T) {
	_, err := node.Claim(context.Background(), "https://operator:hunter2@omniglass.example:80a", "edge-hq", "ogp_enroll")
	if err == nil {
		t.Fatal("a claim to an unparseable server URL succeeded")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the claim error %q carries the server's password", err)
	}
}

// TestAMisreadClaimPasswordIsNeverShown: a password the parser misreads
// without failing landed in the dial error whole, and sim run logged it on
// every retry (#979's review).
func TestAMisreadClaimPasswordIsNeverShown(t *testing.T) {
	_, err := node.Claim(context.Background(), "http://operator:/abcSECRET@"+nettest.RefusedAddr(t), "edge-hq", "ogp_enroll")
	if err == nil {
		t.Fatal("a claim to a misread server URL succeeded")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("the claim error %q carries the server's password", err)
	}
}
