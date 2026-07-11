package avatar_test

import (
	"bytes"
	"errors"
	"image"
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/avatar"
)

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestNormalize_PNGProducesSquareJPEG(t *testing.T) {
	out, err := avatar.Normalize(mustRead(t, "red_600x400.png"))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("format = %q, want jpeg", format)
	}
	if cfg.Width != 256 || cfg.Height != 256 {
		t.Errorf("size = %dx%d, want 256x256", cfg.Width, cfg.Height)
	}
}

func TestNormalize_JPEGPassesThrough(t *testing.T) {
	if _, err := avatar.Normalize(mustRead(t, "blue_100x100.jpg")); err != nil {
		t.Fatalf("normalize jpeg: %v", err)
	}
}

func TestNormalize_RejectsNonImage(t *testing.T) {
	if _, err := avatar.Normalize(mustRead(t, "notanimage.txt")); !errors.Is(err, avatar.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestNormalize_RejectsGIF(t *testing.T) {
	if _, err := avatar.Normalize(mustRead(t, "anim.gif")); !errors.Is(err, avatar.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestNormalize_RejectsOversizePayload(t *testing.T) {
	big := make([]byte, 9<<20) // 9 MiB
	if _, err := avatar.Normalize(big); !errors.Is(err, avatar.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}
