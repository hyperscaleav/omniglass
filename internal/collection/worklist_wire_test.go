package collection

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// worklistOfSize is an encoded worklist of about n bytes.
func worklistOfSize(t *testing.T, n int) (WorklistReply, []byte) {
	t.Helper()
	reply := WorklistReply{ConfigGeneration: 7, ReportSeconds: 60}
	for i := 0; ; i++ {
		raw, err := json.Marshal(reply)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) >= n {
			return reply, raw
		}
		reply.Tasks = append(reply.Tasks, TaskSpec{ID: "task-" + strings.Repeat("x", i%7), Mode: "poll", EndpointName: "tcp", Transport: "tcp"})
	}
}

// A small reply goes as it is, with no header.
func TestASmallWorklistIsSentUncompressed(t *testing.T) {
	_, raw := worklistOfSize(t, 1000)
	body, hdr, err := EncodeWorklistBody(raw)
	if err != nil {
		t.Fatal(err)
	}
	if hdr != nil || !bytes.Equal(body, raw) {
		t.Fatalf("a %d-byte reply was encoded: header %v, %d bytes", len(raw), hdr, len(body))
	}
}

// A reply past 64 KB is gzipped and says so, and the node reads back the
// worklist it was built from.
func TestALargeWorklistIsGzippedAndReadsBack(t *testing.T) {
	want, raw := worklistOfSize(t, WorklistCompressAbove+1)
	body, hdr, err := EncodeWorklistBody(raw)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Get(ContentEncodingHeader) != "gzip" {
		t.Fatalf("header %v, want Content-Encoding: gzip", hdr)
	}
	if len(body) >= len(raw) {
		t.Fatalf("gzipped %d bytes to %d", len(raw), len(body))
	}
	got, err := DecodeWorklistReply(hdr, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tasks) != len(want.Tasks) || got.ConfigGeneration != 7 || got.ReportSeconds != 60 || got.Tasks[3].ID != want.Tasks[3].ID {
		t.Fatalf("read back %d tasks (generation %d), want %d", len(got.Tasks), got.ConfigGeneration, len(want.Tasks))
	}
}

// A reply with no header is plain JSON.
func TestAnUncompressedWorklistReadsBack(t *testing.T) {
	_, raw := worklistOfSize(t, 500)
	got, err := DecodeWorklistReply(nil, raw)
	if err != nil || got.ConfigGeneration != 7 {
		t.Fatalf("read %+v, %v", got, err)
	}
}

// The server's refusal reaches the node as an error naming the reason, never
// as an empty worklist that would tear down its sessions.
func TestARefusedWorklistIsAnError(t *testing.T) {
	_, err := DecodeWorklistReply(WorklistErrorReply("worklist read failed"), nil)
	if !errors.Is(err, ErrWorklistRefused) || !strings.Contains(err.Error(), "worklist read failed") {
		t.Fatalf("err = %v, want the refusal and its reason", err)
	}
}

// An encoding the node does not know, or a body that does not inflate, is an
// error.
func TestAnUnreadableWorklistIsAnError(t *testing.T) {
	if _, err := DecodeWorklistReply(nats.Header{ContentEncodingHeader: []string{"br"}}, []byte("{}")); err == nil {
		t.Error("an unknown encoding decoded")
	}
	if _, err := DecodeWorklistReply(nats.Header{ContentEncodingHeader: []string{"gzip"}}, []byte("{}")); err == nil {
		t.Error("a body that is not gzip inflated")
	}
}

// A body that inflates past the limit is refused rather than read whole.
func TestAWorklistThatInflatesPastTheLimitIsRefused(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	chunk := make([]byte, 1<<20)
	for range worklistInflateLimit/len(chunk) + 1 {
		if _, err := zw.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := DecodeWorklistReply(nats.Header{ContentEncodingHeader: []string{"gzip"}}, buf.Bytes())
	if err == nil || !strings.Contains(err.Error(), "over") {
		t.Fatalf("err = %v, want over the limit", err)
	}
}

// The request's budget parses as a positive duration; anything else states
// none.
func TestAWorklistRequestStatesItsBudget(t *testing.T) {
	for _, c := range []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{"5s", 5 * time.Second, true},
		{"1500ms", 1500 * time.Millisecond, true},
		{"", 0, false},
		{"soon", 0, false},
		{"0s", 0, false},
		{"-1s", 0, false},
	} {
		hdr := nats.Header{}
		if c.value != "" {
			hdr.Set(WorklistTimeoutHeader, c.value)
		}
		got, ok := WorklistRequestTimeout(hdr)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %v %v, want %v %v", c.value, got, ok, c.want, c.ok)
		}
	}
}
