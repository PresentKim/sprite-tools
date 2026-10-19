package processor

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"sprite-tools/pkg/common"
)

func Simplify(inputPath, outputPath string, threshold float64) error {
	file, err := os.Open(inputPath)
	if err != nil {
		fmt.Printf("[ERROR] Failed to open %s: %v\n", inputPath, err)
		return err
	}
	defer file.Close()

	src, err := png.Decode(file)
	if err != nil {
		fmt.Printf("[ERROR] Failed to decode %s: %v\n", inputPath, err)
		return err
	}

	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)

	originalColors := make(map[color.Color]bool)
	colorCache := make(map[color.Color]color.Color)

	type repColor struct {
		oklab common.OKLab
		raw   color.Color
	}
	var representatives []repColor

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := src.At(x, y)
			originalColors[c] = true

			_, _, _, a := c.RGBA()
			if a == 0 {
				dst.Set(x, y, color.Transparent)
				continue
			}

			if mappedColor, ok := colorCache[c]; ok {
				dst.Set(x, y, mappedColor)
				continue
			}

			currOKLab := common.ColorToOKLab(c)
			found := false

			for _, rep := range representatives {
				if common.DeltaOKLabWeighted(currOKLab, rep.oklab) < threshold {
					dst.Set(x, y, rep.raw)
					colorCache[c] = rep.raw
					found = true
					break
				}
			}

			if !found {
				representatives = append(representatives, repColor{currOKLab, c})
				dst.Set(x, y, c)
				colorCache[c] = c
			}
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

	origCount := len(originalColors)
	finalCount := len(representatives)

	hasTransparent := false
	for c := range originalColors {
		_, _, _, a := c.RGBA()
		if a == 0 {
			hasTransparent = true
			break
		}
	}
	if hasTransparent {
		finalCount++
	}

	if origCount == finalCount {
		fmt.Printf("[SKIP] No color count change for %s (Colors: %d)\n", filepath.Base(inputPath), origCount)
	} else {
		fmt.Printf("[DONE] %s: %d -> %d colors\n", filepath.Base(inputPath), origCount, finalCount)
	}

	return nil
}