// Package avatar normalizes an uploaded image into a fixed-size square JPEG for
// use as a principal's profile picture. It is pure: decode, guard, crop, resize,
// re-encode, with no I/O. The guards (payload cap, dimension cap) make it safe to
// run on untrusted uploads.
package avatar

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"

	// Decoders registered for image.Decode / image.DecodeConfig. GIF is
	// deliberately NOT registered, so a GIF (animated or not) fails to decode and
	// is rejected as unsupported.
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxBytes = 8 << 20 // 8 MiB decoded-payload cap (decompression-bomb guard on input)
	maxDim   = 8000    // reject any source dimension above this (bomb guard on pixels)
	outSize  = 256     // output is outSize x outSize
	quality  = 82
)

var (
	ErrTooLarge    = errors.New("avatar: image too large")
	ErrUnsupported = errors.New("avatar: unsupported image")
)

// Normalize decodes raw (JPEG, PNG, or WebP), center-crops it to a square,
// resizes to 256x256, and re-encodes as JPEG. Oversize payloads and dimensions
// are rejected with ErrTooLarge; anything that is not a supported image is
// ErrUnsupported.
func Normalize(raw []byte) ([]byte, error) {
	if len(raw) > maxBytes {
		return nil, ErrTooLarge
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnsupported
	}
	if cfg.Width > maxDim || cfg.Height > maxDim {
		return nil, ErrTooLarge
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnsupported
	}

	// Center-crop to the largest centered square.
	b := src.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	ox := b.Min.X + (b.Dx()-side)/2
	oy := b.Min.Y + (b.Dy()-side)/2
	crop := image.Rect(ox, oy, ox+side, oy+side)

	dst := image.NewRGBA(image.Rect(0, 0, outSize, outSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("avatar: encode: %w", err)
	}
	return out.Bytes(), nil
}
