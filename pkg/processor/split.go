package processor

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

func isFrameEmpty(img image.Image, rect image.Rectangle) bool {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 0 {
				return false
			}
		}
	}
	return true
}

func isAlreadySplit(img image.Image, size int) bool {
	bounds := img.Bounds()
	return bounds.Dx() == size && bounds.Dy() == size
}

func SplitFile(path string, size int) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	srcImg, err := png.Decode(file)
	if err != nil {
		return err
	}

	if isAlreadySplit(srcImg, size) {
		fmt.Printf("건너뜀 (이미 분리된 이미지): %s\n", path)
		return nil
	}

	bounds := srcImg.Bounds()
	baseName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	dir := filepath.Dir(path)
	count := 1

	for y := bounds.Min.Y; y < bounds.Max.Y; y += size {
		for x := bounds.Min.X; x < bounds.Max.X; x += size {
			currRect := image.Rect(x, y, x+size, y+size)

			if isFrameEmpty(srcImg, currRect) {
				continue
			}

			dstImg := image.NewRGBA(image.Rect(0, 0, size, size))
			draw.Draw(dstImg, dstImg.Bounds(), srcImg, currRect.Min, draw.Src)

			outFileName := fmt.Sprintf("%s-%04d.png", baseName, count)
			outPath := filepath.Join(dir, outFileName)

			outFile, err := os.Create(outPath)
			if err != nil {
				return err
			}

			if err := png.Encode(outFile, dstImg); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()

			fmt.Printf("✓ 생성됨: %s\n", outPath)
			count++
		}
	}

	return nil
}

func SplitAll(pattern string, size int) {
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		matched, _ := filepath.Match(pattern, filepath.Base(path))
		if !matched {
			return nil
		}

		if err := SplitFile(path, size); err != nil {
			fmt.Printf("오류 발생 (%s): %v\n", path, err)
		}
		return nil
	})
}