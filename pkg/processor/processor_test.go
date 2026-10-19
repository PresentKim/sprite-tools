package processor

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"

	"sprite-tools/pkg/common"
)

func checker(w, h, scale int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w*scale, h*scale))
	for y := 0; y < h*scale; y++ {
		for x := 0; x < w*scale; x++ {
			if (x/scale+y/scale)%2 == 0 {
				img.Set(x, y, color.NRGBA{255, 0, 0, 255})
			} else {
				img.Set(x, y, color.NRGBA{0, 0, 255, 255})
			}
		}
	}
	return img
}

func TestDetectScale(t *testing.T) {
	for _, s := range []int{1, 2, 3, 4} {
		if got := detectScale(checker(5, 3, s)); got != s {
			t.Errorf("scale %d: got %d", s, got)
		}
	}
}

func TestDownscale(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	if err := common.SavePNG(in, checker(4, 2, 3)); err != nil {
		t.Fatal(err)
	}
	if _, err := Downscale(in, out, 0); err != nil {
		t.Fatal(err)
	}
	img, _ := common.LoadPNG(out)
	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 2 {
		t.Fatalf("unexpected size %v", img.Bounds())
	}
	if _, err := Downscale(out, out, 0); !common.IsSkip(err) {
		t.Fatalf("1:1 image should be skipped, got %v", err)
	}
}

func TestIndexTooManyColors(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "a.png")
	img := image.NewNRGBA(image.Rect(0, 0, 300, 1))
	for x := 0; x < 300; x++ {
		img.Set(x, 0, color.NRGBA{uint8(x), uint8(x >> 8), 0, 255})
	}
	common.SavePNG(in, img)
	if _, err := Index(in, filepath.Join(dir, "b.png")); err == nil {
		t.Fatal("expected error for >255 colors")
	}
}

func TestIndexTransparentIndexZero(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	img := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	img.Set(0, 0, color.NRGBA{10, 20, 30, 255})
	img.Set(1, 0, color.NRGBA{0, 0, 0, 0})
	img.Set(2, 0, color.NRGBA{9, 9, 9, 0})
	common.SavePNG(in, img)
	if _, err := Index(in, out); err != nil {
		t.Fatal(err)
	}
	res, _ := common.LoadPNG(out)
	p, ok := res.(*image.Paletted)
	if !ok {
		t.Fatalf("expected paletted output, got %T", res)
	}
	if len(p.Palette) != 2 || p.ColorIndexAt(1, 0) != 0 || p.ColorIndexAt(2, 0) != 0 {
		t.Fatalf("unexpected palette %v / indices", p.Palette)
	}
}

func TestReduceKeepsDominantColor(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	rare := color.NRGBA{200, 0, 0, 255}
	common_ := color.NRGBA{202, 1, 1, 255}
	img := image.NewNRGBA(image.Rect(0, 0, 5, 1))
	img.Set(0, 0, rare)
	for x := 1; x < 5; x++ {
		img.Set(x, 0, common_)
	}
	common.SavePNG(in, img)
	if _, err := Reduce(in, out, 0.05); err != nil {
		t.Fatal(err)
	}
	res, _ := common.LoadPNG(out)
	if got := color.NRGBAModel.Convert(res.At(0, 0)); got != common_ {
		t.Fatalf("rare color should merge into dominant one, got %v", got)
	}
}

func TestSlice(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "sheet.png")
	img := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	img.Set(0, 0, color.NRGBA{1, 2, 3, 255}) // 첫 프레임만 내용 있음
	common.SavePNG(in, img)
	msg, err := SliceFile(in, 4, 4, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := common.LoadPNG(filepath.Join(dir, "sheet-0001.png")); err != nil {
		t.Fatalf("frame missing: %v (%s)", err, msg)
	}
	if _, err := common.LoadPNG(filepath.Join(dir, "sheet-0002.png")); err == nil {
		t.Fatal("empty frame should be skipped")
	}
}
