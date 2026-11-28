package surface

import "testing"

func TestRawPrintsAStringBareAndAnythingElseAsJSON(t *testing.T) {
	for in, want := range map[any]string{"hdmi1": "hdmi1", "": "", 7: "7", true: "true", nil: "null", 1.5: "1.5"} {
		if got := Raw(in); got != want {
			t.Errorf("Raw(%#v) = %q, want %q", in, got, want)
		}
	}
}

func TestOIDConstructorsReturnTheirKindsGoType(t *testing.T) {
	if o := Text("1", func() string { return "a" }); o.Kind != KindOctetString || o.Get() != "a" {
		t.Error("text")
	}
	if o := Integer("1", func() int { return 3 }); o.Kind != KindInteger || o.Get() != 3 {
		t.Error("integer")
	}
	if o := Gauge("1", func() uint { return 3 }); o.Kind != KindGauge32 || o.Get() != uint(3) {
		t.Error("gauge")
	}
	if o := Counter("1", func() uint { return 3 }); o.Kind != KindCounter32 || o.Get() != uint(3) {
		t.Error("counter")
	}
	if o := Ticks("1", func() uint32 { return 3 }); o.Kind != KindTimeTicks || o.Get() != uint32(3) {
		t.Error("ticks")
	}
}

func TestEncodedRendersAValueAsItsJSONForm(t *testing.T) {
	for in, want := range map[any]string{"decreased": `"decreased"`, 4: "4", true: "true", nil: "null", 2.5: "2.5"} {
		if got := Encoded(in); got != want {
			t.Errorf("Encoded(%v) = %s, want %s", in, got, want)
		}
	}
}
