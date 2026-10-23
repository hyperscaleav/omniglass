package storage_test

import (
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/fingerprint"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The connection fingerprint's table check (#1056 ruling): every field the
// worklist delivers to a node, and so every field a dialer or probe can read
// to connect, is either an input of the fingerprint or declared not one, with
// why. A field added to a task without a decision here fails, so a new
// transport cannot read an endpoint, secret or driver field the fingerprint
// does not cover.
var taskFieldFingerprint = map[string]string{
	// fingerprinted: the fingerprint.Inputs field that covers it
	"Transport":      "Transport",
	"EndpointParams": "Params",
	"Secrets":        "SecretVersions", // the sealed envelopes' digests, never the values
	"Session":        "Session",
	"Ladder":         "Ladder",
	"HostKey":        "HostKeyPin",
	"SessionRetry":   "Retry",
	// not a connection input
	"ID":           "-", // the task's own content hash
	"Mode":         "-", // poll or listen: what to do, not how to connect
	"EndpointID":   "-", // the key a session and a gate are held under
	"EndpointName": "-", // derived from the transport
	"Spec":         "-", // the function (what to ask and how to read it); its arm rides the session identity
	"Fingerprint":  "-", // the digest itself
	"Rejection":    "-", // the gate the digest is compared against
}

func TestEveryTaskFieldIsAFingerprintInputOrDeclaredNotOne(t *testing.T) {
	inputs := reflect.TypeOf(fingerprint.Inputs{})
	// DriverInputs is fingerprinted from the endpoint row, which the worklist
	// reads but does not deliver.
	for _, typ := range []reflect.Type{reflect.TypeOf(storage.WorklistTask{}), reflect.TypeOf(collection.TaskSpec{})} {
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			cover, ok := taskFieldFingerprint[name]
			if !ok {
				t.Errorf("%s.%s is neither a fingerprint input nor declared not one: decide it here", typ.Name(), name)
				continue
			}
			if cover == "-" {
				continue
			}
			if _, ok := inputs.FieldByName(cover); !ok {
				t.Errorf("%s.%s claims fingerprint.Inputs.%s, which does not exist", typ.Name(), name, cover)
			}
		}
	}
}
