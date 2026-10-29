package mailsim_test

import (
	"bytes"
	"encoding/json"
	"net/smtp"
	"strings"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/mailsim"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// The sim keeps what it accepts, writes it as a JSON line, and refuses with a
// temporary error while told it is down.
func TestTheSimKeepsWhatItIsSentAndCanBeDown(t *testing.T) {
	out := &syncBuf{}
	down := false
	var mu sync.Mutex
	sim, err := mailsim.Start("127.0.0.1:0", mailsim.Options{Out: out, Failing: func() bool { mu.Lock(); defer mu.Unlock(); return down }})
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	msg := "Subject: hello\r\nMessage-ID: <m-1@omniglass>\r\n\r\nthe body\r\n"
	if err := smtp.SendMail(sim.Addr(), nil, "a@example.com", []string{"b@example.com"}, []byte(msg)); err != nil {
		t.Fatal(err)
	}
	var line mailsim.Received
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &line); err != nil {
		t.Fatalf("the out line %q: %v", out.String(), err)
	}
	if line.Subject != "hello" || line.Text != "the body" || line.To[0] != "b@example.com" || line.MessageID != "<m-1@omniglass>" {
		t.Fatalf("the sim kept %+v", line)
	}
	mu.Lock()
	down = true
	mu.Unlock()
	err = smtp.SendMail(sim.Addr(), nil, "a@example.com", []string{"b@example.com"}, []byte(msg))
	if err == nil || !strings.Contains(err.Error(), "451") {
		t.Fatalf("a send while down = %v, want a 451", err)
	}
	if n := len(sim.Messages()); n != 1 {
		t.Fatalf("the sim kept %d messages, want 1", n)
	}
}
