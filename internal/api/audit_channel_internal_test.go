package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The channel filter's enum is the storage vocabulary: direct (a row with no
// channel) plus every channel audit_log can hold. A channel added to storage
// without the filter, or offered by the filter without storage, fails here.
func TestAuditChannelEnumIsStorageVocabulary(t *testing.T) {
	t.Parallel()
	f, ok := reflect.TypeOf(auditListInput{}).FieldByName("Channel")
	if !ok {
		t.Fatal("auditListInput has no Channel field")
	}
	got := strings.Split(f.Tag.Get("enum"), ",")
	want := []string{storage.AuditChannelDirect}
	for _, c := range storage.AuditChannels {
		want = append(want, string(c))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("channel enum = %v, want %v", got, want)
	}
}
