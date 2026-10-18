package driver_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A binding declares how its command settles; a malformed declaration is
// refused at write.
func TestValidateSettle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		settle *driver.Settle
		want   string // empty: valid
	}{
		{"unset settles on observation", nil, ""},
		{"observe", &driver.Settle{From: "observe"}, ""},
		{"response with a value", &driver.Settle{From: "response", Extract: &driver.Extract{Regex: `^INPUT (\S+)$`}}, ""},
		{"response with a confirmation", &driver.Settle{From: "response", Success: &driver.Match{Regex: `^OK$`}}, ""},
		{"unknown source", &driver.Settle{From: "telepathy"}, "telepathy"},
		{"response with neither", &driver.Settle{From: "response"}, "exactly one"},
		{"response with both", &driver.Settle{From: "response", Extract: &driver.Extract{Regex: `x`}, Success: &driver.Match{Prefix: "OK"}}, "exactly one"},
		{"response extracting by OID", &driver.Settle{From: "response", Extract: &driver.Extract{OID: "1.3.6"}}, "regex"},
		{"uncompilable value regex", &driver.Settle{From: "response", Extract: &driver.Extract{Regex: `(`}}, "regex"},
		{"observe with an extract", &driver.Settle{From: "observe", Extract: &driver.Extract{Regex: `x`}}, "observe"},
	} {
		s := validSpec()
		s.Commands[0].Settle = tc.settle
		err := s.Validate(nil)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: refused: %v", tc.name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: err %v, want one naming %q", tc.name, err, tc.want)
		}
	}
}

// JudgeReply reads a device's reply to a command the way its binding says.
func TestJudgeReply(t *testing.T) {
	value := driver.CommandBinding{Settle: &driver.Settle{From: "response", Extract: &driver.Extract{Regex: `^INPUT (\S+)$`}}}
	if v := value.JudgeReply("INPUT hdmi3"); !v.Judged || !v.Succeeded || !v.HasValue || v.Value != "hdmi3" {
		t.Errorf("value reply = %+v, want judged with hdmi3", v)
	}
	if v := value.JudgeReply("-ERR busy"); !v.Judged || v.Succeeded || v.HasValue {
		t.Errorf("unmatched value reply = %+v, want judged and failed", v)
	}

	ack := driver.CommandBinding{Settle: &driver.Settle{From: "response", Success: &driver.Match{Regex: `^OK$`}}}
	if v := ack.JudgeReply("OK"); !v.Judged || !v.Succeeded || v.HasValue {
		t.Errorf("ack reply = %+v, want judged, succeeded, no value", v)
	}
	if v := ack.JudgeReply("NAK"); !v.Judged || v.Succeeded {
		t.Errorf("nak reply = %+v, want judged and failed", v)
	}

	for _, b := range []driver.CommandBinding{{}, {Settle: &driver.Settle{From: "observe"}}} {
		if v := b.JudgeReply("INPUT hdmi3"); v.Judged {
			t.Errorf("an observe binding judged its reply: %+v", v)
		}
	}
}
