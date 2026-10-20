package processor

import (
	"image"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

// Trim is the file-level TrimImage.
func Trim(inputPath, outputPath string, pad int) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return TrimImage(src, pad)
	})
}

// opaqueBounds returns the minimal rectangle enclosing all pixels with non-zero alpha (in n's coordinates; ok=false if there are none).
func opaqueBounds(n *image.NRGBA) (r image.Rectangle, ok bool) {
	w, h := n.Rect.Dx(), n.Rect.Dy()
	minX, minY, maxX, maxY := w, h, -1, -1
	for y := 0; y < h; y++ {
		row := n.Pix[y*n.Stride : y*n.Stride+w*4]
		for x := 0; x < w; x++ {
			if row[x*4+3] != 0 {
				minX = min(minX, x)
				maxX = max(maxX, x)
				minY = min(minY, y)
				maxY = max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		return image.Rectangle{}, false
	}
	return image.Rect(minX, minY, maxX+1, maxY+1), true
}

// TrimImage crops to the bounding box of non-transparent pixels and then adds pad pixels of transparent margin on every side.
// If the result equals the original, or the image is fully transparent, it returns a Skip error.
func TrimImage(src image.Image, pad int) (image.Image, string, error) {
	if pad < 0 {
		return nil, "", i18n.Errorf("err.trim.pad", pad)
	}
	n := common.ToNRGBA(src)
	w, h := n.Rect.Dx(), n.Rect.Dy()
	box, ok := opaqueBounds(n)
	if !ok {
		return nil, "", common.Skip("skip.trim.transparent", w, h)
	}
	newW, newH := box.Dx()+2*pad, box.Dy()+2*pad
	if newW*newH > maxOutputPixels {
		return nil, "", i18n.Errorf("err.result_too_large", newW, newH)
	}
	// If the bounding box plus padding equals the whole image, nothing changes.
	if box.Min.X-pad == 0 && box.Min.Y-pad == 0 && newW == w && newH == h {
		return nil, "", common.Skip("skip.trim.unchanged", pad, w, h)
	}

	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < box.Dy(); y++ {
		so := (box.Min.Y+y)*n.Stride + box.Min.X*4
		do := (y+pad)*dst.Stride + pad*4
		copy(dst.Pix[do:do+box.Dx()*4], n.Pix[so:so+box.Dx()*4])
	}
	return dst, i18n.T("msg.trim.result", w, h, newW, newH, pad), nil
}
