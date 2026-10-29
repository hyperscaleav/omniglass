package bus

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// reportWindowStore is a store whose worklist carries a report window.
type reportWindowStore struct {
	fakeStore
	seconds int
}

func (s reportWindowStore) NodeWorklist(context.Context, string) (storage.Worklist, error) {
	return storage.Worklist{ReportSeconds: s.seconds}, nil
}

// The wire reply carries the worklist's collection report window (#1369), the
// one the node reports its collection telemetry over.
func TestTheWorklistReplyCarriesTheReportWindow(t *testing.T) {
	s := &Server{store: reportWindowStore{seconds: 45}}
	reply, err := s.buildWorklistReply("edge-1")
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReportSeconds != 45 || reply.ReportEvery().Seconds() != 45 {
		t.Fatalf("reply carries %d seconds (%v), want 45", reply.ReportSeconds, reply.ReportEvery())
	}
}
