package processor

import (
	"fmt"
	"image"

	"sprite-tools/pkg/common"
)

const (
	MinUpscale = 2
	MaxUpscale = 32

	// maxOutputPixels: 확대/합치기 결과의 최대 픽셀 수 (RGBA 약 1GB).
	maxOutputPixels = 1 << 28
)

// Upscale: 파일 단위 UpscaleImage.
func Upscale(inputPath, outputPath string, scale int) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return UpscaleImage(src, scale)
	})
}

// UpscaleImage: 최근접 이웃 방식으로 정수 배율(2~32)만큼 확대합니다.
func UpscaleImage(src image.Image, scale int) (image.Image, string, error) {
	if scale < MinUpscale || scale > MaxUpscale {
		return nil, "", fmt.Errorf("확대 배율은 %d~%d 사이여야 합니다: %d", MinUpscale, MaxUpscale, scale)
	}
	n := common.ToNRGBA(src)
	w, h := n.Rect.Dx(), n.Rect.Dy()
	if w == 0 || h == 0 {
		return nil, "", common.Skip("빈 이미지")
	}
	newW, newH := w*scale, h*scale
	if newW*newH > maxOutputPixels {
		return nil, "", fmt.Errorf("결과가 너무 큽니다: %dx%d", newW, newH)
	}

	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	rowLen := newW * 4
	for y := 0; y < h; y++ {
		// 첫 줄만 픽셀 단위로 늘리고, 나머지 줄은 복사합니다.
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
	return dst, fmt.Sprintf("%d배 확대, %dx%d → %dx%d", scale, w, h, newW, newH), nil
}
