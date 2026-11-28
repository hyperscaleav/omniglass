package sim

import (
	"io"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/surface"
)

// A Go route that leaves its status unset answers 200, as a handler that
// writes a body does; net/http panics on a zero status.
func TestGoRouteWithNoStatusAnswersOK(t *testing.T) {
	dev := NewDevice("g", "g", net.ParseIP("127.0.0.1"), "p", 1, nil)
	dev.AttachRoute(surface.Route{Method: "GET", Path: "/x", Serve: func() surface.Response {
		return surface.Response{Body: []byte("hi")}
	}})
	rec := httptest.NewRecorder()
	dev.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	if rec.Code != 200 || string(body) != "hi" {
		t.Fatalf("got %d %q", rec.Code, body)
	}
}
