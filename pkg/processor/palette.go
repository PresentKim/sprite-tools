package processor

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
)

func FixPalette(inputPath, outputPath string) error {
	file, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	src, err := png.Decode(file)
	if err != nil {
		return err
	}

	bounds := src.Bounds()

	colorMap := make(map[color.Color]bool)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			colorMap[src.At(x, y)] = true
		}
	}

	newPalette := color.Palette{color.RGBA{255, 255, 255, 0}}

	for c := range colorMap {
		if len(newPalette) >= 256 {
			break
		}
		r, g, b, a := c.RGBA()
		if !(r == 0xffff && g == 0xffff && b == 0xffff && a == 0) {
			newPalette = append(newPalette, c)
		}
	}

	dst := image.NewPaletted(bounds, newPalette)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := encoder.Encode(out, dst); err != nil {
		return err
	}

	fmt.Printf("✓ Palette Adjusted: %s -> %s\n", inputPath, outputPath)
	return nil
}