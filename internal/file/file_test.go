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

// TestValidateRejectsANameThatReadsAsAnotherReference pins #1133's security
// review, finding 4 and 6: a file is addressed by its uuid or its filename, the
// uuid tried first, so a filename shaped like a uuid would reach a DIFFERENT
// file when typed back, and a filename of `.` or `..` is a dot segment a path
// normalizer rewrites into its parent route. Neither is a label anybody needs.
func TestValidateRejectsANameThatReadsAsAnotherReference(t *testing.T) {
	for _, name := range []string{
		"01a03c92-5cd8-757a-8b3c-82623d8ff57e",
		"01A03C92-5CD8-757A-8B3C-82623D8FF57E",
		".", "..",
	} {
		if err := file.Validate(name, "text/plain", []byte("x")); !errors.Is(err, file.ErrNameInvalid) {
			t.Errorf("Validate(%q) = %v, want ErrNameInvalid", name, err)
		}
	}
	for _, name := range []string{"01a03c92-5cd8-757a-8b3c-82623d8ff57e.txt", "...", ".env", "notes:v2.txt"} {
		if err := file.Validate(name, "text/plain", []byte("x")); err != nil {
			t.Errorf("Validate(%q) = %v, want it accepted", name, err)
		}
	}
}
