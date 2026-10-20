package processor

import (
	"image"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// detectScale estimates the pixel upscale factor as the greatest common divisor of the lengths of runs of the same color.
func detectScale(img image.Image) int {
	return detectScaleNRGBA(common.ToNRGBA(img))
}

// detectScaleNRGBA is the NRGBA-only implementation of detectScale (bounds start at 0,0). It compares Pix directly.
func detectScaleNRGBA(img *image.NRGBA) int {
	width, height := img.Rect.Dx(), img.Rect.Dy()
	scale := gcd(width, height)
	if scale <= 1 {
		return 1
	}
	pix, stride := img.Pix, img.Stride

	// Horizontal: length of same-color runs in each row
	for y := 0; y < height; y++ {
		row := pix[y*stride : y*stride+width*4]
		currentLen := 1
		for x := 1; x < width; x++ {
			if [4]byte(row[x*4:x*4+4]) == [4]byte(row[x*4-4:x*4]) {
				currentLen++
			} else {
				scale = gcd(scale, currentLen)
				currentLen = 1
			}
		}
		scale = gcd(scale, currentLen)
		if scale == 1 {
			return 1
		}
	}

	// Vertical: scan row by row and count the run lengths per column (memory access is sequential).
	runs := make([]int, width)
	for x := range runs {
		runs[x] = 1
	}
	for y := 1; y < height; y++ {
		cur := pix[y*stride : y*stride+width*4]
		prev := pix[(y-1)*stride : (y-1)*stride+width*4]
		for x := 0; x < width; x++ {
			if [4]byte(cur[x*4:x*4+4]) == [4]byte(prev[x*4:x*4+4]) {
				runs[x]++
			} else {
				scale = gcd(scale, runs[x])
				runs[x] = 1
			}
		}
		if scale == 1 {
			return 1
		}
	}
	for _, r := range runs {
		scale = gcd(scale, r)
	}
	if scale <= 1 {
		return 1
	}
	return scale
}

// Downscale is the file-level DownscaleImage.
func Downscale(inputPath, outputPath string, forceScale int) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return DownscaleImage(src, forceScale)
	})
}

// DownscaleImage shrinks upscaled pixel art back to 1:1 size. If forceScale > 0, that factor is used instead of auto-detection.
func DownscaleImage(src image.Image, forceScale int) (image.Image, string, error) {
	bounds := src.Bounds()
	n := common.ToNRGBA(src)

	scale := forceScale
	if scale <= 0 {
		scale = detectScaleNRGBA(n)
		if scale <= 1 {
			return nil, "", common.Skip("skip.downscale.none", bounds.Dx(), bounds.Dy())
		}
	} else if bounds.Dx()%scale != 0 || bounds.Dy()%scale != 0 {
		return nil, "", i18n.Errorf("err.downscale.not_divisible", bounds.Dx(), bounds.Dy(), scale)
	}

	newW, newH := bounds.Dx()/scale, bounds.Dy()/scale
	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			copy(dst.Pix[(y*newW+x)*4:], n.Pix[(y*scale*n.Stride)+x*scale*4:][:4])
		}
	}

	return dst, i18n.T("msg.downscale.result", scale, bounds.Dx(), bounds.Dy(), newW, newH), nil
}
