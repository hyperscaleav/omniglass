package sim

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/profile"
)

// A Go-declared subject builds its push line from the subject that changed,
// the token the subscription carries and the new value (#2835): one wildcard
// subject covers many paths, and a command may name its own token.
func TestAGoSubjectPushesTheSubjectAndTokenItWasSubscribedWith(t *testing.T) {
	dev := NewDevice("d", "d", net.ParseIP("127.0.0.1"), "p", 1, map[string]any{
		"status": map[string]any{"channels": []any{map[string]any{"gain": -6}, map[string]any{"gain": -3}}},
	})
	cfg, err := CompileSessionConfig(&profile.Interface{
		Name: "shell", Protocol: "tcp", Persistent: true, LineEnding: "\n",
		Subjects: []profile.Subject{{
			Match: "status.channels.*.gain", PublishToken: "gain",
			PushLine: func(subject, token string, value any) string {
				return fmt.Sprintf("%s|%s|%v", subject, token, value)
			},
		}},
	})
	if err != nil {
		t.Fatalf("a subject with a Go push line did not compile: %v", err)
	}
	reg := NewSessionRegistry(dev, cfg, nil)
	sess, err := reg.Open("test")
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close(sess)
	if err := sess.Subscribe("status.channels.0.gain", "", 0); err != nil {
		t.Fatal(err)
	}
	if err := sess.Subscribe("status.channels.1.gain", "mine", 0); err != nil {
		t.Fatal(err)
	}
	reg.Fanout(map[string]any{"status.channels.0.gain": -9, "status.channels.1.gain": 4})
	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case line := <-sess.Out:
			got[string(line)] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("heard %v, want two pushes", got)
		}
	}
	for _, want := range []string{"status.channels.0.gain|gain|-9\n", "status.channels.1.gain|mine|4\n"} {
		if !got[want] {
			t.Errorf("pushes = %v, missing %q", got, want)
		}
	}
}
