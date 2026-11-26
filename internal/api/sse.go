package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Stream hygiene every Server-Sent Events route keeps (#2731). A proxy that
// buffers a response (nginx does, unless told otherwise) holds a stream's
// events until the buffer fills, so each route says X-Accel-Buffering: no. A
// client that stops reading would park its stream's goroutine on a write
// forever, so each send carries a write deadline, and a send that misses it
// ends the stream. A resumable stream tells an EventSource how long to wait
// before it reconnects (the retry field).
const (
	sseWriteDeadline = 10 * time.Second
	sseRetry         = 3 * time.Second
)

// startSSE sends a stream's status and headers. The returned func clears the
// write deadline the stream's sends set, so the response's own end is not
// cut by a deadline a long idle passed.
func startSSE(hctx huma.Context, cacheControl string) func() {
	hctx.SetHeader("Content-Type", "text/event-stream")
	hctx.SetHeader("Cache-Control", cacheControl)
	hctx.SetHeader("X-Accel-Buffering", "no")
	hctx.SetStatus(http.StatusOK)
	w := hctx.BodyWriter()
	return func() {
		if rw, ok := w.(http.ResponseWriter); ok {
			_ = http.NewResponseController(rw).SetWriteDeadline(time.Time{})
		}
	}
}

// sseWriter frames Server-Sent Events and flushes each one, every write
// under a fresh deadline. It is an io.Writer and an http.Flusher too, for a
// route that frames its own events (the AG-UI run).
type sseWriter struct {
	w io.Writer
	// deadline bounds each write; zero is sseWriteDeadline.
	deadline time.Duration
}

func newSSEWriter(w io.Writer) *sseWriter { return &sseWriter{w: w} }

func (s *sseWriter) Write(p []byte) (int, error) {
	if rw, ok := s.w.(http.ResponseWriter); ok {
		d := s.deadline
		if d <= 0 {
			d = sseWriteDeadline
		}
		if err := http.NewResponseController(rw).SetWriteDeadline(time.Now().Add(d)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return 0, err
		}
	}
	return s.w.Write(p)
}

// Flush pushes what was written to the client.
func (s *sseWriter) Flush() { _ = s.flush() }

func (s *sseWriter) flush() error {
	if rw, ok := s.w.(http.ResponseWriter); ok {
		if err := http.NewResponseController(rw).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		return nil
	}
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// retry sends the SSE retry field: how long an EventSource waits before it
// reconnects.
func (s *sseWriter) retry(d time.Duration) error {
	if _, err := s.Write([]byte("retry: " + strconv.FormatInt(d.Milliseconds(), 10) + "\n\n")); err != nil {
		return err
	}
	return s.flush()
}

// send writes one event, with id when it has one, and flushes it.
func (s *sseWriter) send(id, event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var buf strings.Builder
	if id != "" {
		buf.WriteString("id: " + id + "\n")
	}
	buf.WriteString("event: " + event + "\ndata: ")
	buf.Write(b)
	buf.WriteString("\n\n")
	if _, err := s.Write([]byte(buf.String())); err != nil {
		return err
	}
	return s.flush()
}

// StreamShutdown tells the server's open streams it is shutting down, so
// each ends with its position (a final mark) rather than being cut: an
// http.Server's Shutdown waits for idle connections, and a stream is never
// idle. The server registers Close with RegisterOnShutdown.
type StreamShutdown struct {
	once sync.Once
	done chan struct{}
}

// NewStreamShutdown returns an open shutdown signal.
func NewStreamShutdown() *StreamShutdown { return &StreamShutdown{done: make(chan struct{})} }

// Close signals every stream to end; a second close is a no-op.
func (s *StreamShutdown) Close() {
	if s != nil {
		s.once.Do(func() { close(s.done) })
	}
}

// Done is closed once the server is shutting down; a nil signal never closes.
func (s *StreamShutdown) Done() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.done
}
