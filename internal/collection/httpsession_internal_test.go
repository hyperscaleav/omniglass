package collection

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A component cannot make an endpoint's jar hold more than maxCookies
// cookies, or one past maxCookieValue.
func TestRecordingJarIsBounded(t *testing.T) {
	j := newRecordingJar()
	u, _ := url.Parse("http://10.0.0.5/")
	var many []*http.Cookie
	for i := range 200 {
		many = append(many, &http.Cookie{Name: fmt.Sprintf("c%d", i), Value: fmt.Sprintf("value-%d", i)})
	}
	j.SetCookies(u, many)
	if got := len(j.Cookies(u)); got != maxCookies {
		t.Fatalf("the jar holds %d cookies, want %d", got, maxCookies)
	}
	// A known cookie still updates past the cap.
	j.SetCookies(u, []*http.Cookie{{Name: "c0", Value: "value-updated"}})
	for _, c := range j.Cookies(u) {
		if c.Name == "c0" && c.Value != "value-updated" {
			t.Fatalf("c0 = %q, want the update", c.Value)
		}
	}
	big := newRecordingJar()
	big.SetCookies(u, []*http.Cookie{{Name: "big", Value: strings.Repeat("x", maxCookieValue)}})
	if len(big.Cookies(u)) != 0 || len(big.values()) != 0 {
		t.Fatal("a cookie past the value cap was kept")
	}
}
