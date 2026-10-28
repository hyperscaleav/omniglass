package migrate

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckHead(t *testing.T) {
	known := []string{"20260627120000", "20261028002934", "20260724120000"}
	cases := []struct {
		name    string
		applied []string
		wantErr bool
	}{
		{"an empty database is behind", nil, false},
		{"a database behind the binary starts", []string{"20260627120000"}, false},
		{"a database at the binary's head starts", []string{"20260627120000", "20260724120000", "20261028002934"}, false},
		// A version collapsed out of the chain (#388) sits below the head; it is
		// history, not a newer schema, so it never refuses.
		{"an applied version below the head the binary no longer embeds starts", []string{"20260601000000", "20261028002934"}, false},
		{"a database migrated by a newer build refuses", []string{"20261028002934", "20261101090000"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkHead(tc.applied, known)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkHead(%v) = %v, want error %v", tc.applied, err, tc.wantErr)
			}
			if err == nil {
				return
			}
			if !errors.Is(err, ErrSchemaAhead) {
				t.Errorf("error %v is not ErrSchemaAhead", err)
			}
			for _, v := range []string{"20261101090000", "20261028002934"} {
				if !strings.Contains(err.Error(), v) {
					t.Errorf("error %q does not name %s", err, v)
				}
			}
		})
	}
}

func TestKnownVersionsReadTheEmbeddedSet(t *testing.T) {
	known, err := knownVersions()
	if err != nil {
		t.Fatalf("knownVersions: %v", err)
	}
	if len(known) == 0 {
		t.Fatal("knownVersions found no embedded migrations")
	}
	for _, v := range known {
		if v == "" || strings.Trim(v, "0123456789") != "" {
			t.Errorf("known version %q is not a dbmate timestamp", v)
		}
	}
}
