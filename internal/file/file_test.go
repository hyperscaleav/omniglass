package file_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/file"
)

func TestValidateAcceptsAWellFormedUpload(t *testing.T) {
	if err := file.Validate("firmware-v2.1.bin", "application/octet-stream", []byte("bytes")); err != nil {
		t.Fatalf("valid upload rejected: %v", err)
	}
}

func TestValidateRejectsEmptyName(t *testing.T) {
	err := file.Validate("  ", "text/plain", []byte("x"))
	if !errors.Is(err, file.ErrNameInvalid) {
		t.Fatalf("blank name: got %v, want ErrNameInvalid", err)
	}
}

func TestValidateRejectsPathSeparatorsInName(t *testing.T) {
	for _, name := range []string{"../etc/passwd", "a/b.txt", `a\b.txt`} {
		if err := file.Validate(name, "text/plain", []byte("x")); !errors.Is(err, file.ErrNameInvalid) {
			t.Fatalf("name %q: got %v, want ErrNameInvalid", name, err)
		}
	}
}

func TestValidateRejectsEmptyContentType(t *testing.T) {
	err := file.Validate("a.txt", "", []byte("x"))
	if !errors.Is(err, file.ErrContentTypeInvalid) {
		t.Fatalf("blank content type: got %v, want ErrContentTypeInvalid", err)
	}
}

func TestValidateRejectsEmptyBytes(t *testing.T) {
	err := file.Validate("a.txt", "text/plain", nil)
	if !errors.Is(err, file.ErrEmpty) {
		t.Fatalf("empty bytes: got %v, want ErrEmpty", err)
	}
}

func TestValidateRejectsOversizeName(t *testing.T) {
	err := file.Validate(strings.Repeat("a", 256), "text/plain", []byte("x"))
	if !errors.Is(err, file.ErrNameInvalid) {
		t.Fatalf("oversize name: got %v, want ErrNameInvalid", err)
	}
}
