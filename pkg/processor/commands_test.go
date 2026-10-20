package processor

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sprite-tools/pkg/common"
)

// solid creates a w x h NRGBA image filled with a single color.
func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestUpscaleImage(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	cs := []color.NRGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 128}, {0, 0, 0, 0}}
	for i, c := range cs {
		src.SetNRGBA(i%2, i/2, c)
	}
	out, msg, err := UpscaleImage(src, 3)
	if err != nil {
		t.Fatal(err)
	}
	n := out.(*image.NRGBA)
	if n.Rect.Dx() != 6 || n.Rect.Dy() != 6 {
		t.Fatalf("size %v", n.Rect)
	}
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			if got, want := n.NRGBAAt(x, y), cs[(y/3)*2+x/3]; got != want {
				t.Fatalf("(%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
	if !strings.Contains(msg, "3배 확대") || !strings.Contains(msg, "2x2 → 6x6") {
		t.Errorf("msg = %q", msg)
	}
	// Scaling back down by the detected factor after upscaling must equal the original.
	back, _, err := DownscaleImage(out, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !equalNRGBA(back.(*image.NRGBA), src) {
		t.Error("upscale -> downscale result differs from the original")
	}
	for _, bad := range []int{0, 1, 33, -2} {
		if _, _, err := UpscaleImage(src, bad); err == nil {
			t.Errorf("scale %d should be an error", bad)
		}
	}
}

func equalNRGBA(a, b *image.NRGBA) bool {
	return a.Rect == b.Rect && string(a.Pix) == string(b.Pix)
}

func TestTrimImage(t *testing.T) {
	red := color.NRGBA{255, 0, 0, 255}
	src := image.NewNRGBA(image.Rect(0, 0, 10, 8))
	src.SetNRGBA(3, 2, red)
	src.SetNRGBA(5, 4, color.NRGBA{0, 0, 255, 1}) // alpha 1 also counts as content

	out, msg, err := TrimImage(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := out.(*image.NRGBA)
	if n.Rect.Dx() != 3 || n.Rect.Dy() != 3 {
		t.Fatalf("size %v", n.Rect)
	}
	if n.NRGBAAt(0, 0) != red || n.NRGBAAt(2, 2).A != 1 {
		t.Error("content was not copied correctly")
	}
	if !strings.Contains(msg, "10x8 → 3x3 (여백 0)") {
		t.Errorf("msg = %q", msg)
	}

	out, _, err = TrimImage(src, 2)
	if err != nil {
		t.Fatal(err)
	}
	n = out.(*image.NRGBA)
	if n.Rect.Dx() != 7 || n.Rect.Dy() != 7 || n.NRGBAAt(2, 2) != red || n.NRGBAAt(0, 0).A != 0 {
		t.Errorf("padding result is wrong: %v", n.Rect)
	}

	// An already full image is skipped
	if _, _, err := TrimImage(solid(4, 4, red), 0); !common.IsSkip(err) {
		t.Errorf("full image: %v", err)
	}
	// Skipped if the padding already equals pad
	padded := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	padded.SetNRGBA(2, 2, red)
	if _, _, err := TrimImage(padded, 2); !common.IsSkip(err) {
		t.Errorf("image that already has the padding: %v", err)
	}
	// Fully transparent
	if _, _, err := TrimImage(image.NewNRGBA(image.Rect(0, 0, 4, 4)), 1); !common.IsSkip(err) {
		t.Errorf("transparent image: %v", err)
	}
	if _, _, err := TrimImage(src, -1); err == nil {
		t.Error("negative padding should be an error")
	}
}

func TestExtractPaletteImage(t *testing.T) {
	a, b, c := color.NRGBA{10, 20, 30, 255}, color.NRGBA{200, 0, 0, 255}, color.NRGBA{0, 200, 0, 255}
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	// Order of appearance c, a, b, a ... frequency: a=3, b=2, c=2 (b appears later than c)
	seq := []color.NRGBA{c, a, b, a, a, b, c, {255, 255, 255, 0}}
	for i, col := range seq {
		src.SetNRGBA(i%4, i/4, col)
	}
	// A translucent pixel with the same RGB as a must be merged into a.
	src.SetNRGBA(3, 1, color.NRGBA{10, 20, 30, 100})

	out, msg, err := ExtractPaletteImage(src)
	if err != nil {
		t.Fatal(err)
	}
	n := out.(*image.NRGBA)
	if n.Rect.Dx() != 3 || n.Rect.Dy() != 1 {
		t.Fatalf("size %v", n.Rect)
	}
	want := []color.NRGBA{a, c, b} // a=4, c=2 (appears first), b=2
	for i, w := range want {
		if got := n.NRGBAAt(i, 0); got != w {
			t.Errorf("color %d = %v, want %v", i, got, w)
		}
	}
	if !strings.Contains(msg, "3개") {
		t.Errorf("msg = %q", msg)
	}

	if _, _, err := ExtractPaletteImage(image.NewNRGBA(image.Rect(0, 0, 3, 3))); !common.IsSkip(err) {
		t.Errorf("transparent image: %v", err)
	}

	many := image.NewNRGBA(image.Rect(0, 0, 300, 1))
	for x := 0; x < 300; x++ {
		many.SetNRGBA(x, 0, color.NRGBA{uint8(x), uint8(x >> 8), 1, 255})
	}
	_, _, err = ExtractPaletteImage(many)
	if err == nil || common.IsSkip(err) || !strings.Contains(err.Error(), "reduce") {
		t.Errorf("more than 256 colors: %v", err)
	}
}

func TestNormalizePaletteName(t *testing.T) {
	if got, err := NormalizePaletteName("  My-Pal2 "); err != nil || got != "my-pal2" {
		t.Errorf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "-x", "a b", "a_b", "../x", "한글"} {
		if _, err := NormalizePaletteName(bad); err == nil {
			t.Errorf("%q should be an error", bad)
		}
	}
}

func TestPackImages(t *testing.T) {
	r, g, b := color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 255, 0, 255}, color.NRGBA{0, 0, 255, 255}
	imgs := []image.Image{solid(4, 4, r), solid(2, 3, g), solid(4, 2, b)}

	sheet, msg, err := PackImages(imgs, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 3 frames -> 2 columns x 2 rows, 4x4 cells
	if sheet.Rect.Dx() != 8 || sheet.Rect.Dy() != 8 {
		t.Fatalf("size %v", sheet.Rect)
	}
	if !strings.Contains(msg, "3프레임 → 2x2 시트, 8x8") {
		t.Errorf("msg = %q", msg)
	}
	if sheet.NRGBAAt(3, 3) != r || sheet.NRGBAAt(4, 0) != g || sheet.NRGBAAt(6, 0).A != 0 || sheet.NRGBAAt(4, 3).A != 0 {
		t.Error("top-left aligned placement is wrong")
	}
	if sheet.NRGBAAt(0, 4) != b || sheet.NRGBAAt(0, 6).A != 0 {
		t.Error("second row placement is wrong")
	}

	// Gap and center alignment
	sheet, _, err = PackSheet(imgs, 3, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if sheet.Rect.Dx() != 4*3+2*2 || sheet.Rect.Dy() != 4 {
		t.Fatalf("size %v", sheet.Rect)
	}
	// Second cell starts at x = 6; g (2x3) spans (7,0)-(8,2)
	if sheet.NRGBAAt(7, 0) != g || sheet.NRGBAAt(7, 2) != g || sheet.NRGBAAt(7, 3).A != 0 || sheet.NRGBAAt(6, 0).A != 0 {
		t.Error("center-aligned placement is wrong")
	}
	if sheet.NRGBAAt(4, 0).A != 0 {
		t.Error("the gap should be transparent")
	}

	if _, _, err := PackImages(nil, 0, 0); err == nil {
		t.Error("an empty list should be an error")
	}
	// A column count larger than the frame count is capped to it
	sheet, _, _ = PackImages(imgs, 10, 0)
	if sheet.Rect.Dx() != 12 || sheet.Rect.Dy() != 4 {
		t.Errorf("size %v", sheet.Rect)
	}
}

func writeTestPNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := common.SavePNG(path, img); err != nil {
		t.Fatal(err)
	}
}

// pack -> slice round trip: packing frames of equal size and slicing them again must give back the original frames.
func TestPackSliceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	var frames []*image.NRGBA
	var paths []string
	for i := 1; i <= 7; i++ {
		f := solid(6, 5, color.NRGBA{uint8(i * 30), uint8(255 - i*10), uint8(i), 255})
		f.SetNRGBA(i%6, i%5, color.NRGBA{1, 2, 3, 77})
		frames = append(frames, f)
		p := filepath.Join(dir, "in", "frame-"+string(rune('0'+i))+".png")
		if i >= 5 { // mix in two-digit names to exercise natural sort
			p = filepath.Join(dir, "in", "frame-1"+string(rune('0'+i))+".png")
		}
		writeTestPNG(t, p, f)
		paths = append(paths, p)
	}
	// Natural sort must apply even if the inputs are passed in shuffled order (frame-1..4, frame-15..17).
	shuffled := []string{paths[6], paths[2], paths[0], paths[5], paths[1], paths[4], paths[3]}

	sheetPath := filepath.Join(dir, "sheet.png")
	if _, err := PackFiles(shuffled, sheetPath, 4, 0, false, false); err != nil {
		t.Fatal(err)
	}
	// An existing output is rejected without force
	if _, err := PackFiles(shuffled, sheetPath, 4, 0, false, false); err == nil {
		t.Error("overwriting an existing file should be rejected without force")
	}
	if _, err := PackFiles(shuffled, sheetPath, 4, 0, false, true); err != nil {
		t.Errorf("force should overwrite: %v", err)
	}
	// An input file cannot be overwritten even with force
	if _, err := PackFiles(shuffled, paths[0], 4, 0, false, true); err == nil {
		t.Error("overwriting an input file should always be rejected")
	}

	src, err := common.LoadPNG(sheetPath)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := SliceImage(src, 6, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 { // 2 rows x 4 columns, the last cell is an empty frame
		t.Fatalf("%d frames", len(got))
	}
	for i, want := range frames {
		if !equalNRGBA(got[i].Image, want) {
			t.Errorf("frame %d differs from the original", i+1)
		}
	}
}

func TestSavePaletteRefusesExisting(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir()) // UserConfigDir on Windows
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	pal := solid(2, 1, color.NRGBA{1, 2, 3, 255})
	path, err := SavePalette("Test-Pal", pal, false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "test-pal.png" {
		t.Errorf("path = %s", path)
	}
	if _, err := SavePalette("test-pal", pal, false); err == nil {
		t.Error("the same name should be rejected without force")
	}
	if _, err := SavePalette("test-pal", pal, true); err != nil {
		t.Errorf("force: %v", err)
	}
	if _, err := SavePalette("bad name", pal, true); err == nil {
		t.Error("an invalid name should be rejected")
	}
	raw, lab, err := LoadPaletteColors(path)
	if err != nil || len(raw) != 1 || len(lab) != 1 {
		t.Errorf("the saved palette should be readable: %v %v", raw, err)
	}
}

func TestDespeckleImage(t *testing.T) {
	red := color.NRGBA{255, 0, 0, 255}
	blue := color.NRGBA{0, 0, 255, 128}
	src := image.NewNRGBA(image.Rect(0, 0, 10, 8))
	// Large blob: 3x3 (9 pixels)
	for y := 1; y <= 3; y++ {
		for x := 1; x <= 3; x++ {
			src.SetNRGBA(x, y, red)
		}
	}
	src.SetNRGBA(8, 1, blue)                    // isolated single pixel
	src.SetNRGBA(7, 5, blue)                    // two pixels connected only diagonally
	src.SetNRGBA(8, 6, blue)                    //
	src.SetNRGBA(0, 7, color.NRGBA{9, 9, 9, 1}) // alpha 1 also counts as content

	// 8-way: the two diagonal pixels form one blob (size 2), plus 2 isolated pixels (size 1) -> with min size 2, the 2 isolated ones are removed
	out, msg, err := DespeckleImage(src, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	n := out.(*image.NRGBA)
	if n.NRGBAAt(8, 1).A != 0 || n.NRGBAAt(0, 7) != (color.NRGBA{}) {
		t.Error("isolated pixels were not removed")
	}
	if n.NRGBAAt(7, 5) != blue || n.NRGBAAt(8, 6) != blue || n.NRGBAAt(2, 2) != red {
		t.Error("pixels that should stay were changed")
	}
	if msg != "조각 2개 제거 (픽셀 2개)" {
		t.Errorf("msg = %q", msg)
	}
	if src.NRGBAAt(8, 1) != blue {
		t.Error("the input image was changed")
	}

	// 4-way: the two diagonal pixels each become a 1-pixel piece and are removed (4 pieces)
	out, msg, err = DespeckleImage(src, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if n = out.(*image.NRGBA); n.NRGBAAt(7, 5).A != 0 || n.NRGBAAt(8, 6).A != 0 || msg != "조각 4개 제거 (픽셀 4개)" {
		t.Errorf("4-way result differs: %q", msg)
	}

	// Min size 10: even the 3x3 (9) is removed, so everything disappears
	out, msg, err = DespeckleImage(src, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if n = out.(*image.NRGBA); n.NRGBAAt(2, 2).A != 0 || msg != "조각 4개 제거 (픽셀 13개)" {
		t.Errorf("min 10: %q", msg)
	}

	// Skip when there is nothing to remove, error for invalid values
	if _, _, err := DespeckleImage(src, 1, true); !common.IsSkip(err) {
		t.Errorf("minSize 1: %v", err)
	}
	if _, _, err := DespeckleImage(solid(4, 4, red), 4, true); !common.IsSkip(err) {
		t.Errorf("no pieces: %v", err)
	}
	if _, _, err := DespeckleImage(image.NewNRGBA(image.Rect(0, 0, 4, 4)), 3, true); !common.IsSkip(err) {
		t.Errorf("fully transparent: %v", err)
	}
	if _, _, err := DespeckleImage(src, 0, true); err == nil || common.IsSkip(err) {
		t.Errorf("minSize 0 should be an error: %v", err)
	}
}
