package mail_test

import (
	"bytes"
	"io"
	"mime"
	"mime/quotedprintable"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/mail"
)

func TestBuildIsAPlainTextMessageAReaderParses(t *testing.T) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	m := mail.Message{ID: "01a1-id", From: "Omniglass <omniglass@example.com>", To: []string{"alice@example.com", "ops@example.com"},
		Subject: "[High] display-1: no vidéo", Text: "Line one\nLine two, a long line " + strings.Repeat("x", 90), Date: at}
	raw, err := mail.Build(m)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("\r\n\r\n")) || bytes.Contains(bytes.ReplaceAll(raw, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatalf("the message is not CRLF-delimited:\n%s", raw)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mimeDecoder)
	if subj := dec.header(t, msg.Header.Get("Subject")); subj != m.Subject {
		t.Fatalf("Subject = %q, want %q", subj, m.Subject)
	}
	if got := msg.Header.Get("Message-Id"); got != "<01a1-id@omniglass>" {
		t.Fatalf("Message-ID = %q", got)
	}
	if got, _ := msg.Header.Date(); !got.Equal(at) {
		t.Fatalf("Date = %v, want the row's own time %v", got, at)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if strings.TrimSuffix(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") != m.Text {
		t.Fatalf("body = %q, want %q", body, m.Text)
	}
	// The same row builds the same bytes: a retry sends what the first
	// attempt sent.
	again, _ := mail.Build(m)
	if !bytes.Equal(raw, again) {
		t.Fatal("two builds of one message differ")
	}
}

func TestBuildRefusesAHeaderInjection(t *testing.T) {
	for _, m := range []mail.Message{
		{ID: "x", From: "a@example.com", To: []string{"b@example.com"}, Subject: "one\r\nBcc: evil@example.com"},
		{ID: "x", From: "a@example.com\r\nBcc: evil@example.com", To: []string{"b@example.com"}},
		{ID: "x", From: "a@example.com", To: []string{"b@example.com\nBcc: evil@example.com"}},
	} {
		raw, err := mail.Build(m)
		if err == nil && bytes.Contains(raw, []byte("\nBcc:")) {
			t.Fatalf("a header was injected:\n%s", raw)
		}
	}
}

type mimeDecoder struct{}

func (mimeDecoder) header(t *testing.T, v string) string {
	t.Helper()
	out, err := new(mimeWordDecoder).DecodeHeader(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type mimeWordDecoder = mime.WordDecoder
