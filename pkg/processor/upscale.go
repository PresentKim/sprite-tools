package processor

import (
	"image"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

const (
	MinUpscale = 2
	MaxUpscale = 32

	// maxOutputPixels is the maximum number of pixels of an upscale/pack result (about 1GB of RGBA).
	maxOutputPixels = 1 << 28
)

// Upscale is the file-level UpscaleImage.
func Upscale(inputPath, outputPath string, scale int) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return UpscaleImage(src, scale)
	})
}

// UpscaleImage upscales by an integer factor (2-32) using nearest-neighbor.
func UpscaleImage(src image.Image, scale int) (image.Image, string, error) {
	if scale < MinUpscale || scale > MaxUpscale {
		return nil, "", i18n.Errorf("err.upscale.scale_range", MinUpscale, MaxUpscale, scale)
	}
	n := common.ToNRGBA(src)
	w, h := n.Rect.Dx(), n.Rect.Dy()
	if w == 0 || h == 0 {
		return nil, "", common.Skip("skip.upscale.empty")
	}
	newW, newH := w*scale, h*scale
	if newW*newH > maxOutputPixels {
		return nil, "", i18n.Errorf("err.result_too_large", newW, newH)
	}

	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	rowLen := newW * 4
	for y := 0; y < h; y++ {
		// Expand only the first row pixel by pixel; copy the remaining rows.
		first := dst.Pix[y*scale*rowLen : y*scale*rowLen+rowLen]
		srcRow := n.Pix[y*n.Stride : y*n.Stride+w*4]
		for x := 0; x < w; x++ {
			px := srcRow[x*4 : x*4+4]
			for k := 0; k < scale; k++ {
				copy(first[(x*scale+k)*4:], px)
			}
		}
		for k := 1; k < scale; k++ {
			copy(dst.Pix[(y*scale+k)*rowLen:(y*scale+k+1)*rowLen], first)
		}
	}
	return dst, i18n.T("msg.upscale.result", scale, w, h, newW, newH), nil
}
