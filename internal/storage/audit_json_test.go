package storage

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
)

// imageOf runs auditJSON and decodes the image it would store.
func imageOf(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := auditJSON(v)
	if err != nil {
		t.Fatalf("auditJSON: %v", err)
	}
	var image map[string]any
	if err := json.Unmarshal(b.([]byte), &image); err != nil {
		t.Fatalf("decode image %s: %v", b, err)
	}
	return image
}

// An endpoint's params and inputs are jsonb read back raw, so the image
// carries them as the JSON they are, never as encoding/json's base64 of the
// bytes (#1088).
func TestAnAuditImageCarriesJSONBytesAsJSON(t *testing.T) {
	ep := &Endpoint{
		ID: "ep-1", Name: "ssh", Transport: "ssh",
		Params: []byte(`{"target":"127.60.0.19:22","timeout":"5s"}`),
		Inputs: []byte(`{"host":"127.60.0.19","login":"panel-login"}`),
	}
	image := imageOf(t, ep)
	if got, want := image["Params"], map[string]any{"target": "127.60.0.19:22", "timeout": "5s"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Params = %#v, want %#v", got, want)
	}
	if got, want := image["Inputs"], map[string]any{"host": "127.60.0.19", "login": "panel-login"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Inputs = %#v, want %#v", got, want)
	}
	if image["Name"] != "ssh" || image["Transport"] != "ssh" {
		t.Fatalf("the other fields moved: %#v", image)
	}
}

// The retry's image embeds the endpoint beside the pin (RetryEndpoint's
// pinned): the promoted fields are inlined too, and a nil embedded endpoint
// leaves only the pin.
func TestAnAuditImageInlinesThroughAnEmbeddedEndpoint(t *testing.T) {
	type pinned struct {
		*Endpoint
		HostKey string `json:"host_key,omitempty"`
	}
	image := imageOf(t, pinned{Endpoint: &Endpoint{Params: []byte(`{"target":"h:22"}`)}, HostKey: "SHA256:abc"})
	if got, want := image["Params"], map[string]any{"target": "h:22"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Params = %#v, want %#v", got, want)
	}
	if image["host_key"] != "SHA256:abc" {
		t.Fatalf("host_key = %#v", image["host_key"])
	}
	if image := imageOf(t, pinned{HostKey: "SHA256:abc"}); len(image) != 1 || image["host_key"] != "SHA256:abc" {
		t.Fatalf("nil embedded endpoint image = %#v, want the pin alone", image)
	}
}

// Only bytes that hold JSON are inlined, under the key encoding/json gives
// the field; anything else marshals exactly as before.
func TestAnAuditImageInlinesOnlyJSONUnderItsKey(t *testing.T) {
	type named struct {
		Spec   []byte `json:"spec"`
		Hidden []byte `json:"-"`
		Opt    []byte `json:"opt,omitempty"`
		Blob   []byte
		Null   []byte
		Raw    json.RawMessage
	}
	blob := []byte{0xff, 0x00, 0x7b}
	image := imageOf(t, named{
		Spec:   []byte(`{"version":1,"polls":[]}`),
		Hidden: []byte(`{"x":1}`),
		Blob:   blob,
		Raw:    json.RawMessage(`[1,2]`),
	})
	if got, want := image["spec"], map[string]any{"version": float64(1), "polls": []any{}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("spec = %#v, want %#v", got, want)
	}
	if _, ok := image["Hidden"]; ok {
		t.Fatal("a json:\"-\" field reached the image")
	}
	if _, ok := image["opt"]; ok {
		t.Fatal("an empty omitempty field reached the image")
	}
	if got, want := image["Blob"], base64.StdEncoding.EncodeToString(blob); got != want {
		t.Fatalf("Blob = %#v, want the base64 %q encoding/json gives non-JSON bytes", got, want)
	}
	if image["Null"] != nil {
		t.Fatalf("Null = %#v, want null", image["Null"])
	}
	if got, want := image["Raw"], []any{float64(1), float64(2)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Raw = %#v, want %#v", got, want)
	}
}

// A struct embedded under a json name is a nested object, not promoted: its
// bytes are not looked for at the top, where a same-named field would be
// clobbered.
func TestAnAuditImageLeavesANamedEmbedAlone(t *testing.T) {
	type Inner struct{ Params []byte }
	type outer struct {
		Inner `json:"inner"`
		Other string `json:"Params"`
	}
	image := imageOf(t, outer{Inner: Inner{Params: []byte(`{"a":1}`)}, Other: "top"})
	if image["Params"] != "top" {
		t.Fatalf("top-level Params = %#v, want \"top\"", image["Params"])
	}
}

// An image that is not a struct (a map, as the secret and principal writes
// pass) and a missing image marshal as they always did.
func TestAnAuditImageOfAMapOrNothingIsUnchanged(t *testing.T) {
	image := imageOf(t, map[string]any{"name": "core-snmp", "owner_id": nil})
	if !reflect.DeepEqual(image, map[string]any{"name": "core-snmp", "owner_id": nil}) {
		t.Fatalf("map image = %#v", image)
	}
	if b, err := auditJSON(nil); err != nil || b != nil {
		t.Fatalf("auditJSON(nil) = %v, %v, want nil, nil", b, err)
	}
}
