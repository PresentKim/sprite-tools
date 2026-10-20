package processor

import (
	"fmt"
	"image"

	"sprite-tools/pkg/common"
)

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// detectScale: 같은 색이 연속되는 구간 길이들의 최대공약수로 픽셀 확대 배율을 추정합니다.
func detectScale(img image.Image) int {
	return detectScaleNRGBA(common.ToNRGBA(img))
}

// detectScaleNRGBA: detectScale 의 NRGBA(경계가 0,0 시작) 전용 구현입니다. Pix 를 직접 비교합니다.
func detectScaleNRGBA(img *image.NRGBA) int {
	width, height := img.Rect.Dx(), img.Rect.Dy()
	scale := gcd(width, height)
	if scale <= 1 {
		return 1
	}
	pix, stride := img.Pix, img.Stride

	// 가로: 각 행에서 같은 색이 이어지는 길이
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

	// 세로: 행 단위로 훑으며 열마다 이어지는 길이를 센다(메모리 접근이 연속적).
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

// Downscale: 파일 단위 DownscaleImage.
func Downscale(inputPath, outputPath string, forceScale int) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return DownscaleImage(src, forceScale)
	})
}

// DownscaleImage: 확대된 픽셀 아트를 1:1 크기로 줄입니다. forceScale > 0 이면 자동 감지 대신 해당 배율을 사용합니다.
func DownscaleImage(src image.Image, forceScale int) (image.Image, string, error) {
	bounds := src.Bounds()
	n := common.ToNRGBA(src)

	scale := forceScale
	if scale <= 0 {
		scale = detectScaleNRGBA(n)
		if scale <= 1 {
			return nil, "", common.Skip("이미 1:1 크기이거나 배율을 감지할 수 없음, %dx%d", bounds.Dx(), bounds.Dy())
		}
	} else if bounds.Dx()%scale != 0 || bounds.Dy()%scale != 0 {
		return nil, "", fmt.Errorf("이미지 크기 %dx%d 가 배율 %d 로 나누어떨어지지 않습니다", bounds.Dx(), bounds.Dy(), scale)
	}

	newW, newH := bounds.Dx()/scale, bounds.Dy()/scale
	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			copy(dst.Pix[(y*newW+x)*4:], n.Pix[(y*scale*n.Stride)+x*scale*4:][:4])
		}
	}

	return dst, fmt.Sprintf("%d배 축소, %dx%d → %dx%d", scale, bounds.Dx(), bounds.Dy(), newW, newH), nil
}
