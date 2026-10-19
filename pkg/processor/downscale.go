package processor

import (
	"fmt"
	"image"
	"image/png"
	"os"
)

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func detectScale(img image.Image) int {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scale := width

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

	scale = gcd(scale, width)
	scale = gcd(scale, height)

	return scale
}

func Downscale(inputPath, outputPath string) error {
	file, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	src, err := png.Decode(file)
	if err != nil {
		return err
	}

	scale := detectScale(src)
	if scale <= 1 {
		return fmt.Errorf("already 1:1 scale or unable to detect")
	}

	bounds := src.Bounds()
	newW, newH := bounds.Dx()/scale, bounds.Dy()/scale
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))

	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			dst.Set(x, y, src.At(bounds.Min.X+x*scale, bounds.Min.Y+y*scale))
		}
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if err := png.Encode(out, dst); err != nil {
		return err
	}

	fi, _ := os.Stat(outputPath)
	fmt.Printf("[DOWNSCALED] %d bytes (Detected: %dx, Target: %dx%d): %s -> %s\n", fi.Size(), scale, newW, newH, inputPath, outputPath)
	return nil
}