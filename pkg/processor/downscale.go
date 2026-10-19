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
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scale := gcd(width, height)
	if scale <= 1 {
		return 1
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		currentLen := 1
		for x := bounds.Min.X + 1; x < bounds.Max.X; x++ {
			if img.At(x, y) == img.At(x-1, y) {
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

	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		currentLen := 1
		for y := bounds.Min.Y + 1; y < bounds.Max.Y; y++ {
			if img.At(x, y) == img.At(x, y-1) {
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

	scale := forceScale
	if scale <= 0 {
		scale = detectScale(src)
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
			dst.Set(x, y, src.At(bounds.Min.X+x*scale, bounds.Min.Y+y*scale))
		}
	}

	return dst, fmt.Sprintf("%d배 축소, %dx%d → %dx%d", scale, bounds.Dx(), bounds.Dy(), newW, newH), nil
}
