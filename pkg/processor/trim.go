package processor

import (
	"fmt"
	"image"

	"sprite-tools/pkg/common"
)

// Trim: 파일 단위 TrimImage.
func Trim(inputPath, outputPath string, pad int) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return TrimImage(src, pad)
	})
}

// opaqueBounds: 알파가 0이 아닌 픽셀을 모두 감싸는 최소 사각형을 반환합니다 (n 의 좌표계, 없으면 ok=false).
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

// TrimImage: 투명하지 않은 픽셀의 경계 상자로 자른 뒤 사방에 pad 픽셀의 투명 여백을 붙입니다.
// 결과가 원본과 같으면, 또는 완전히 투명한 이미지면 Skip 에러를 반환합니다.
func TrimImage(src image.Image, pad int) (image.Image, string, error) {
	if pad < 0 {
		return nil, "", fmt.Errorf("여백은 0 이상이어야 합니다: %d", pad)
	}
	n := common.ToNRGBA(src)
	w, h := n.Rect.Dx(), n.Rect.Dy()
	box, ok := opaqueBounds(n)
	if !ok {
		return nil, "", common.Skip("완전히 투명한 이미지라 자를 기준이 없음, %dx%d", w, h)
	}
	newW, newH := box.Dx()+2*pad, box.Dy()+2*pad
	if newW*newH > maxOutputPixels {
		return nil, "", fmt.Errorf("결과가 너무 큽니다: %dx%d", newW, newH)
	}
	// 경계 상자에 여백을 더한 영역이 이미지 전체와 같으면 달라지는 것이 없습니다.
	if box.Min.X-pad == 0 && box.Min.Y-pad == 0 && newW == w && newH == h {
		return nil, "", common.Skip("이미 여백 %d 로 맞음, %dx%d", pad, w, h)
	}

	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < box.Dy(); y++ {
		so := (box.Min.Y+y)*n.Stride + box.Min.X*4
		do := (y+pad)*dst.Stride + pad*4
		copy(dst.Pix[do:do+box.Dx()*4], n.Pix[so:so+box.Dx()*4])
	}
	return dst, fmt.Sprintf("%dx%d → %dx%d (여백 %d)", w, h, newW, newH, pad), nil
}
