package common

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"

	"sprite-tools/pkg/i18n"
)

// LoadPNG reads and decodes a PNG file into memory. The file handle is closed immediately, so the same path can be overwritten right away.
func LoadPNG(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, i18n.Errorf("err.png.invalid", err)
	}
	return img, nil
}

// SavePNG writes the image to a temporary file first and then swaps it in, so a failure never corrupts the existing file.
func SavePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return WriteFileAtomic(path, buf.Bytes())
}

// WriteFileAtomic writes to a temporary file in the same directory and then renames it, saving atomically.
func WriteFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sprite-tools-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// ToNRGBA converts an image to an *image.NRGBA whose bounds start at (0,0).
// If it already is such an NRGBA it is returned as is without copying, so modifying the result also modifies the original (treat it as read-only).
// The result always has no padding between rows (Stride == 4*width), so Pix can be walked directly in pixel order.
func ToNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) && n.Stride == 4*n.Rect.Dx() {
		return n
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// PackNRGBA packs 8-bit NRGBA values into a single uint32 usable as a map key.
func PackNRGBA(r, g, b, a uint8) uint32 {
	return uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | uint32(a)
}

// UnpackNRGBA is the inverse of PackNRGBA.
func UnpackNRGBA(k uint32) (r, g, b, a uint8) {
	return uint8(k >> 24), uint8(k >> 16), uint8(k >> 8), uint8(k)
}

// PackedToOKLab converts a packed NRGBA value to OKLab (alpha excluded, same result as ColorToOKLab).
func PackedToOKLab(k uint32) OKLab {
	r, g, b, _ := UnpackNRGBA(k)
	return RGBToOKLab(uint32(r)<<8, uint32(g)<<8, uint32(b)<<8)
}
