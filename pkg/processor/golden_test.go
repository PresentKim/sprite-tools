package processor

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

// This file is a regression test that checks results stay byte-for-byte identical before and after performance work (refactoring).
// The expected values are SHA-256 hashes recorded with the pre-refactoring implementation.

// lcg is a deterministic random number generator for tests.
type lcg uint32

func (l *lcg) next() uint32 {
	*l = *l*1664525 + 1013904223
	return uint32(*l >> 8)
}

// goldenPalette is the default color list, mixing similar colors (differing by 1-3).
func goldenPalette() []color.NRGBA {
	base := []color.NRGBA{
		{200, 30, 30, 255}, {30, 200, 30, 255}, {30, 30, 200, 255}, {240, 240, 60, 255},
		{20, 20, 20, 255}, {250, 250, 250, 255}, {120, 80, 40, 255}, {90, 90, 100, 255},
	}
	var out []color.NRGBA
	for i, c := range base {
		out = append(out, c)
		out = append(out, color.NRGBA{c.R + 2, c.G + 1, c.B, 255})
		out = append(out, color.NRGBA{c.R, c.G + 3, c.B - 2, 255})
		if i%2 == 0 {
			out = append(out, color.NRGBA{c.R, c.G, c.B, 128}) // translucent
		}
	}
	return out
}

func goldenNRGBA(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	pal := goldenPalette()
	r := lcg(12345)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Some of the 16x16 blocks are cleared entirely to make empty frames.
			if (x/16+y/16)%3 == 1 {
				continue
			}
			v := r.next()
			switch v % 7 {
			case 0:
				// Transparent pixels whose RGB is not 0
				img.SetNRGBA(x, y, color.NRGBA{9, 8, 7, 0})
			case 1:
				// Fully transparent (0,0,0,0) is left as is.
			default:
				img.SetNRGBA(x, y, pal[int(v>>3)%len(pal)])
			}
		}
	}
	return img
}

func goldenPaletted(w, h int) *image.Paletted {
	pal := color.Palette{
		color.NRGBA{0, 0, 0, 0}, color.NRGBA{255, 0, 0, 255}, color.NRGBA{254, 1, 0, 255},
		color.NRGBA{0, 255, 0, 255}, color.NRGBA{0, 0, 255, 255}, color.NRGBA{0, 0, 253, 255},
		color.NRGBA{255, 255, 0, 128}, color.NRGBA{10, 10, 10, 255}, color.NRGBA{12, 11, 10, 255},
		color.NRGBA{200, 200, 200, 255},
	}
	img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	r := lcg(777)
	for i := range img.Pix {
		img.Pix[i] = uint8(r.next() % uint32(len(pal)))
	}
	return img
}

func goldenUpscaled() *image.NRGBA {
	small := image.NewNRGBA(image.Rect(0, 0, 12, 9))
	pal := goldenPalette()
	r := lcg(99)
	for y := 0; y < 9; y++ {
		for x := 0; x < 12; x++ {
			if r.next()%5 == 0 {
				continue
			}
			small.SetNRGBA(x, y, pal[int(r.next())%6])
		}
	}
	const s = 4
	big := image.NewNRGBA(image.Rect(0, 0, 12*s, 9*s))
	for y := 0; y < 9*s; y++ {
		for x := 0; x < 12*s; x++ {
			big.SetNRGBA(x, y, small.NRGBAAt(x/s, y/s))
		}
	}
	return big
}

func goldenInputs() []struct {
	name string
	img  image.Image
} {
	sub := goldenNRGBA(80, 64).SubImage(image.Rect(7, 5, 71, 53))
	return []struct {
		name string
		img  image.Image
	}{
		{"nrgba", goldenNRGBA(64, 48)},
		{"sub", sub},
		{"paletted", goldenPaletted(40, 30)},
		{"up4", goldenUpscaled()},
		{"transparent", image.NewNRGBA(image.Rect(0, 0, 16, 16))},
	}
}

func goldenPaletteColors() ([]color.NRGBA, []common.OKLab) {
	cols := []color.NRGBA{
		{0, 0, 0, 255}, {255, 255, 255, 255}, {200, 40, 40, 255},
		{40, 200, 40, 255}, {40, 40, 200, 255}, {230, 230, 70, 255},
	}
	labs := make([]common.OKLab, len(cols))
	for i, c := range cols {
		labs[i] = common.ColorToOKLab(c)
	}
	return cols, labs
}

// hashImage hashes the result image's bounds and NRGBA pixels (plus the index and palette for a paletted image).
func hashImage(img image.Image) string {
	h := sha256.New()
	b := img.Bounds()
	fmt.Fprintf(h, "%v;", b)
	if p, ok := img.(*image.Paletted); ok {
		h.Write(p.Pix)
		for _, c := range p.Palette {
			r, g, bl, a := c.RGBA()
			binary.Write(h, binary.LittleEndian, []uint32{r, g, bl, a})
		}
	}
	n := image.NewNRGBA(b)
	draw.Draw(n, b, img, b.Min, draw.Src)
	h.Write(n.Pix)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func describe(img image.Image, msg string, err error) string {
	if err != nil {
		return "ERR:" + err.Error()
	}
	return hashImage(img) + "|" + msg
}

func goldenResults() map[string]string {
	got := map[string]string{}
	cols, labs := goldenPaletteColors()
	for _, in := range goldenInputs() {
		for _, t := range []float64{0.004, 0.02, 0.08} {
			img, msg, err := ReduceImage(in.img, t)
			got[fmt.Sprintf("%s/reduce-%v", in.name, t)] = describe(img, msg, err)
		}
		for _, s := range []int{0, 2, 4} {
			img, msg, err := DownscaleImage(in.img, s)
			got[fmt.Sprintf("%s/downscale-%d", in.name, s)] = describe(img, msg, err)
		}
		{
			img, msg, err := IndexImage(in.img)
			got[in.name+"/index"] = describe(img, msg, err)
		}
		{
			img, msg, err := RecolorImage(in.img, cols, labs)
			got[in.name+"/recolor"] = describe(img, msg, err)
		}
		for _, keep := range []bool{false, true} {
			for _, fs := range [][2]int{{16, 16}, {10, 12}} {
				frames, msg, err := SliceImage(in.img, fs[0], fs[1], keep)
				key := fmt.Sprintf("%s/slice-%dx%d-%v", in.name, fs[0], fs[1], keep)
				if err != nil {
					got[key] = "ERR:" + err.Error()
					continue
				}
				h := sha256.New()
				for _, f := range frames {
					fmt.Fprintf(h, "%d,%d;", f.Col, f.Row)
					h.Write([]byte(hashImage(f.Image)))
				}
				got[key] = hex.EncodeToString(h.Sum(nil))[:16] + "|" + msg
			}
		}
	}
	return got
}

// TestGolden compares, in Korean, the expected values recorded with both the pixel hash and the message.
// The expected values are exactly as recorded before i18n was introduced; the test passes only if the Korean catalog reproduces the old wording exactly.
func TestGolden(t *testing.T) {
	if err := i18n.SetLanguage("ko"); err != nil {
		t.Fatal(err)
	}
	got := goldenResults()
	for k, v := range got {
		want, ok := goldenWant[k]
		if !ok {
			t.Errorf("no expected value: %q: %q,", k, v)
		} else if want != v {
			t.Errorf("%s mismatch\n got: %s\nwant: %s", k, v, want)
		}
	}
	for k := range goldenWant {
		if _, ok := got[k]; !ok {
			t.Errorf("no result: %s", k)
		}
	}
}
