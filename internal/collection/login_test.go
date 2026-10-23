package collection_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The login dialog (#1049) over a real byte stream: net.Pipe stands in for
// the wire, and a scripted device on the other end writes its prompts
// without a line terminator, as a real login does.

func panelDialog() *collection.LoginDialog {
	return &collection.LoginDialog{
		Steps: []collection.LoginPrompt{
			{Expect: driver.Match{Regex: `login: ?$`}, Send: "admin"},
			{Expect: driver.Match{Regex: `Password: ?$`}, Send: "newtron"},
		},
		Ready: driver.Match{Prefix: "READY"},
	}
}

func rejectClassifier(line string) (string, bool) {
	if strings.HasPrefix(line, "auth rejected") {
		return driver.ClassAuth, true
	}
	return "", false
}

// scriptedLogin plays a device's login: banner, "login: ", "Password: ",
// then ready or the refusal, reporting what it was sent.
func scriptedLogin(t *testing.T, dev net.Conn, user, pass string, sent chan<- []string) {
	t.Helper()
	go func() {
		defer dev.Close()
		rd := bufio.NewReader(dev)
		var got []string
		_, _ = io.WriteString(dev, "panel\r\nlogin: ")
		u, _ := rd.ReadString('\n')
		got = append(got, strings.TrimRight(u, "\r\n"))
		_, _ = io.WriteString(dev, "Password: ")
		p, _ := rd.ReadString('\n')
		got = append(got, strings.TrimRight(p, "\r\n"))
		sent <- got
		if got[0] == user && got[1] == pass {
			_, _ = io.WriteString(dev, "\r\nREADY\r\n")
			_, _ = io.ReadAll(rd)
			return
		}
		_, _ = io.WriteString(dev, "\r\nauth rejected\r\n")
	}()
}

func TestLoginDialogAnswersPromptsThatEndNoLine(t *testing.T) {
	client, dev := net.Pipe()
	defer client.Close()
	sent := make(chan []string, 1)
	scriptedLogin(t, dev, "admin", "newtron", sent)
	if err := collection.RunLogin(context.Background(), client, panelDialog(), rejectClassifier, 2*time.Second); err != nil {
		t.Fatalf("login: %v", err)
	}
	if got := <-sent; got[0] != "admin" || got[1] != "newtron" {
		t.Fatalf("the device was sent %q", got)
	}
}

func TestLoginDialogFailureIsTerminalWithTheDeviceText(t *testing.T) {
	client, dev := net.Pipe()
	defer client.Close()
	sent := make(chan []string, 1)
	scriptedLogin(t, dev, "admin", "newtron", sent)
	d := panelDialog()
	d.Steps[1].Send = "wrong-password"
	err := collection.RunLogin(context.Background(), client, d, rejectClassifier, 2*time.Second)
	terr, ok := collection.AsTerminal(err)
	if !ok {
		t.Fatalf("a refused login = %v, want a TerminalError", err)
	}
	if terr.Class != driver.ClassAuth || terr.Reason != "auth rejected" {
		t.Fatalf("terminal = %+v, want auth: auth rejected", terr)
	}
	if strings.Contains(err.Error(), "wrong-password") {
		t.Fatalf("the refusal leaks the credential: %v", err)
	}
}

// A device that asks for the login again after the password refused it,
// even when the driver declared no terminal rule for its text.
func TestLoginDialogRepromptIsAnAuthRefusal(t *testing.T) {
	client, dev := net.Pipe()
	defer client.Close()
	go func() {
		defer dev.Close()
		rd := bufio.NewReader(dev)
		_, _ = io.WriteString(dev, "login: ")
		_, _ = rd.ReadString('\n')
		_, _ = io.WriteString(dev, "Password: ")
		_, _ = rd.ReadString('\n')
		_, _ = io.WriteString(dev, "\r\nLogin incorrect\r\nlogin: ")
		_, _ = io.ReadAll(rd)
	}()
	err := collection.RunLogin(context.Background(), client, panelDialog(), nil, 2*time.Second)
	terr, ok := collection.AsTerminal(err)
	if !ok || terr.Class != driver.ClassAuth {
		t.Fatalf("a re-prompted login = %v, want auth terminal", err)
	}
}

// A device that never prompts is a transient failure (it may be booting),
// never a terminal one.
func TestLoginDialogSilenceIsRetryable(t *testing.T) {
	client, dev := net.Pipe()
	defer client.Close()
	defer dev.Close()
	go func() { _, _ = io.ReadAll(dev) }()
	err := collection.RunLogin(context.Background(), client, panelDialog(), rejectClassifier, 50*time.Millisecond)
	if err == nil {
		t.Fatal("a silent device logged in")
	}
	if _, ok := collection.AsTerminal(err); ok {
		t.Fatalf("silence classified terminal: %v", err)
	}
}

// A connection that drops mid-login is retryable too.
func TestLoginDialogDropIsRetryable(t *testing.T) {
	client, dev := net.Pipe()
	defer client.Close()
	go func() {
		_, _ = io.WriteString(dev, "login: ")
		_ = dev.Close()
	}()
	err := collection.RunLogin(context.Background(), client, panelDialog(), rejectClassifier, time.Second)
	if err == nil {
		t.Fatal("a dropped login passed")
	}
	if _, ok := collection.AsTerminal(err); ok {
		t.Fatalf("a drop classified terminal: %v", err)
	}
}

// The same dialog answers ssh keyboard-interactive questions: each question
// is answered by the step whose prompt it matches.
func TestLoginDialogAnswersChallenges(t *testing.T) {
	d := panelDialog()
	answers, err := d.Answer([]string{"Password: "})
	if err != nil || len(answers) != 1 || answers[0] != "newtron" {
		t.Fatalf("Answer(Password) = %q, %v", answers, err)
	}
	if _, err := d.Answer([]string{"Token code: "}); err == nil {
		t.Fatal("a question no step expects was answered")
	}
}

func TestLoginDialogCancelled(t *testing.T) {
	client, dev := net.Pipe()
	defer client.Close()
	defer dev.Close()
	go func() { _, _ = io.ReadAll(dev) }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := collection.RunLogin(ctx, client, panelDialog(), nil, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled login = %v", err)
	}
}
