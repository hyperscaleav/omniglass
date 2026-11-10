package collection

import (
	"net"
	"testing"
	"time"
)

// One request per connection is what lets the lab reconcile the node's
// connections against the sim's accepts: a pooled connection would carry
// several requests on one accept. Both HTTP transports keep it.
func TestBothHTTPTransportsKeepDisableKeepAlives(t *testing.T) {
	if !probeTransport(nil).DisableKeepAlives {
		t.Fatal("the http probe's transport pools connections")
	}
	if !fetchTransport(&net.Dialer{}, HTTPExchange{Timeout: time.Second}).DisableKeepAlives {
		t.Fatal("the http fetcher's transport pools connections")
	}
}
