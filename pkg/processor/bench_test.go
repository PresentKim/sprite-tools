package processor

import (
	"image"
	"image/color"
	"testing"
)

// benchImage is a 1024x1024 image of 240 colors scaled up 4x (some pixels are translucent/transparent).
func benchImage() *image.NRGBA {
	const size, scale = 1024, 4
	r := lcg(2024)
	pal := make([]color.NRGBA, 240)
	for i := range pal {
		v := r.next()
		pal[i] = color.NRGBA{uint8(v), uint8(v >> 8), uint8(v >> 16), 255}
		if i%10 == 0 {
			pal[i].A = 128
		}
	}
	cells := size / scale
	grid := make([]int, cells*cells)
	for i := range grid {
		grid[i] = int(r.next() % 250) // 240 and above are transparent
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if g := grid[(y/scale)*cells+x/scale]; g < len(pal) {
				img.SetNRGBA(x, y, pal[g])
			}
		}
	}
	return img
}

func BenchmarkReduce(b *testing.B) {
	img := benchImage()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := ReduceImage(img, 0.03); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDownscale(b *testing.B) {
	img := benchImage()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := DownscaleImage(img, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIndex(b *testing.B) {
	img := benchImage()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := IndexImage(img); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecolor(b *testing.B) {
	img := benchImage()
	cols, labs := goldenPaletteColors()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := RecolorImage(img, cols, labs); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSlice(b *testing.B) {
	img := benchImage()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := SliceImage(img, 32, 32, false); err != nil {
			b.Fatal(err)
		}
	}
}
