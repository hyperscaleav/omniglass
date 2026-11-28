//go:build linux

package sim

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The helpers a family's wireFamily.Extra hook records with (#2836): a
// conversation on a line-protocol face, an event stream, and the webhook posts
// a device sends. The generic script covers HTTP requests and SNMP reads; these
// cover the rest of a device's wire, recorded in the same transcript and held to
// the same replay. Nothing here knows a family.

// wireTalk is one send on a line-protocol face and what came back.
type wireTalk struct {
	// Send is the bytes written (Go string escapes spelled out), Set the state
	// an operator's hand writes to the device before the read (a path to its
	// new value), and Name says what the step is for.
	Name string
	Send string
	Set  map[string]any
}

// wireHeard is what one wireTalk read: the bytes as hex (a binary frame is no
// text) and as a quoted string, the reading of a human.
type wireHeard struct {
	Name string `json:"name"`
	Sent string `json:"sent"`
	Hex  string `json:"hex"`
	Text string `json:"text"`
}

// wireConverse opens one connection to the named face and plays the talks on
// it in order, reading after each until the face has been quiet for a moment,
// and returns what it said. The first element is what the face says on connect
// (its welcome banner).
func wireConverse(t *testing.T, d *Device, face string, talks []wireTalk) []wireHeard {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", facePort(t, d, face)), 2*time.Second)
	if err != nil {
		t.Fatalf("dialing the %s face: %v", face, err)
	}
	defer conn.Close()
	read := func() string {
		var buf []byte
		chunk := make([]byte, 4096)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
			n, err := conn.Read(chunk)
			buf = append(buf, chunk[:n]...)
			if err != nil {
				return string(buf)
			}
		}
	}
	heard := func(name, sent, got string) wireHeard {
		return wireHeard{Name: name, Sent: strconv.Quote(sent), Hex: hex.EncodeToString([]byte(got)), Text: strconv.Quote(got)}
	}
	out := []wireHeard{heard("on connect", "", read())}
	for _, tk := range talks {
		if len(tk.Set) > 0 {
			// Written as an outside hand writes state: subscribed sessions hear it.
			if err := d.applyExternalDiffs(tk.Set, d.Version+1); err != nil {
				t.Fatal(err)
			}
		}
		if tk.Send != "" {
			if _, err := conn.Write([]byte(tk.Send)); err != nil {
				t.Fatalf("writing %q: %v", tk.Send, err)
			}
		}
		out = append(out, heard(tk.Name, tk.Send, read()))
	}
	return out
}

// syncBuffer is a response writer a stream can be read from while it is still
// being written.
type syncBuffer struct {
	mu     sync.Mutex
	header http.Header
	body   strings.Builder
	status int
}

func (b *syncBuffer) Header() http.Header {
	if b.header == nil {
		b.header = http.Header{}
	}
	return b.header
}
func (b *syncBuffer) WriteHeader(code int) { b.mu.Lock(); b.status = code; b.mu.Unlock() }
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body.Write(p)
}
func (b *syncBuffer) Flush() {}
func (b *syncBuffer) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body.String()
}

// wireStream is what a device's event stream sent while during ran: it opens the
// stream as the console would, waits for the stream's opening lines, runs
// during, waits until the stream has been quiet, and closes it.
func wireStream(t *testing.T, d *Device, path string, during func()) map[string]any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := asConsole(httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
	req.RemoteAddr = "console"
	if d.HTTPAuth.Mode == HTTPAuthHeader {
		req.Header.Set(d.HTTPAuth.HeaderName, d.HTTPAuth.APIKey)
	}
	w := &syncBuffer{}
	done := make(chan struct{})
	go func() { defer close(done); d.ServeHTTP(w, req) }()
	deadline := time.Now().Add(2 * time.Second)
	for w.text() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	opening := w.text()
	during()
	last, quiet := -1, 0
	for quiet < 6 && time.Now().Before(deadline.Add(3*time.Second)) {
		time.Sleep(25 * time.Millisecond)
		if n := len(w.text()); n == last {
			quiet++
		} else {
			last, quiet = n, 0
		}
	}
	cancel()
	<-done
	w.mu.Lock()
	defer w.mu.Unlock()
	return map[string]any{"status": w.status, "content_type": w.Header().Get("Content-Type"), "opening": opening, "stream": w.body.String()}
}

// wireRecv is a webhook receiver a device is pointed at: it keeps every post it
// is sent.
type wireRecv struct {
	srv   *httptest.Server
	mu    sync.Mutex
	posts []wireCaught
	// Secret is the key the posts are signed with, which the receiver checks
	// them against, as a server does.
	Secret string
	// Signature, Timestamp and Delivery are the header names a post carries
	// those under.
	Signature, Prefix, Timestamp, Delivery string
}

// wireCaught is one post as received. The signature, timestamp and delivery id
// vary with the clock and a draw, so what is recorded is whether the signature
// verifies, not its bytes.
type wireCaught struct {
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
	Signature string            `json:"signature"`
	// AgeMinutes is how old the signed timestamp was when the post arrived,
	// and Redelivered says another post of the same step carried the same
	// delivery id: what a stale or redelivered post differs by.
	AgeMinutes  int  `json:"age_minutes"`
	Redelivered bool `json:"redelivered"`
	delivery    string
}

func newWireRecv(t *testing.T) *wireRecv {
	r := &wireRecv{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		post := wireCaught{Body: string(b), Headers: map[string]string{}}
		for k, v := range req.Header {
			if k == "User-Agent" || k == "Accept-Encoding" || k == "Content-Length" {
				continue
			}
			val := strings.Join(v, ",")
			if k == http.CanonicalHeaderKey(r.Signature) || k == http.CanonicalHeaderKey(r.Timestamp) || k == http.CanonicalHeaderKey(r.Delivery) {
				val = "<set>"
			}
			post.Headers[k] = val
		}
		post.Signature = r.verify(req, b)
		if r.Timestamp != "" {
			if ts, err := strconv.ParseInt(req.Header.Get(r.Timestamp), 10, 64); err == nil {
				post.AgeMinutes = int(time.Since(time.Unix(ts, 0)).Minutes() + 0.5)
			}
		}
		if r.Delivery != "" {
			post.delivery = req.Header.Get(r.Delivery)
		}
		r.mu.Lock()
		r.posts = append(r.posts, post)
		r.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// verify says whether the post's signature is the one its secret makes.
func (r *wireRecv) verify(req *http.Request, body []byte) string {
	if r.Signature == "" {
		return "unsigned"
	}
	got := req.Header.Get(r.Signature)
	if got == "" {
		return "absent"
	}
	content := body
	if r.Timestamp != "" {
		content = append([]byte(req.Header.Get(r.Timestamp)+"."), body...)
	}
	m := hmac.New(sha256.New, []byte(r.Secret))
	m.Write(content)
	if got == r.Prefix+hex.EncodeToString(m.Sum(nil)) {
		return "valid"
	}
	return "invalid"
}

// take waits for n posts and hands over everything received since the last take,
// in the order the receiver saw them; sorted when the posts of one step may
// arrive in either order.
func (r *wireRecv) take(t *testing.T, n int, sorted bool) []wireCaught {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		got := len(r.posts)
		r.mu.Unlock()
		if got >= n {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond) // a post that was not expected would be here by now
	r.mu.Lock()
	out := r.posts
	r.posts = nil
	r.mu.Unlock()
	if len(out) != n {
		t.Errorf("received %d posts, expected %d: %+v", len(out), n, out)
	}
	for i := range out {
		for j := range out {
			if i != j && out[i].delivery != "" && out[i].delivery == out[j].delivery {
				out[i].Redelivered = true
			}
		}
	}
	if sorted {
		sort.Slice(out, func(i, j int) bool { return out[i].Body < out[j].Body })
	}
	return out
}
