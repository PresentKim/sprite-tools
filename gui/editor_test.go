package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sprite-tools/pkg/common"
)

func b64(pix []byte) string { return base64.StdEncoding.EncodeToString(pix) }

func decodeB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadImageRGBAKeepsLowAlpha(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{200, 100, 50, 3})
	img.SetNRGBA(1, 0, color.NRGBA{1, 2, 3, 255})
	writePNG(t, p, img)

	got, err := loadImageRGBA(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 2 || got.Height != 1 || got.Indexed {
		t.Fatalf("metadata differs: %+v", got)
	}
	want := []byte{200, 100, 50, 3, 1, 2, 3, 255}
	if !bytes.Equal(decodeB64(t, got.Pixels), want) {
		t.Errorf("pixels = %v, want %v", decodeB64(t, got.Pixels), want)
	}
}

func TestLoadImageRGBAPaletted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "p.png")
	pal := color.Palette{color.NRGBA{0, 0, 0, 0}, color.NRGBA{10, 20, 30, 255}, color.NRGBA{250, 0, 0, 7}}
	img := image.NewPaletted(image.Rect(0, 0, 3, 1), pal)
	img.Pix = []uint8{1, 0, 2}
	writePNG(t, p, img)

	got, err := loadImageRGBA(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Indexed || got.Colors != 3 {
		t.Fatalf("index info differs: %+v", got)
	}
	want := []byte{10, 20, 30, 255, 0, 0, 0, 0, 250, 0, 0, 7}
	if !bytes.Equal(decodeB64(t, got.Pixels), want) {
		t.Errorf("pixels = %v, want %v", decodeB64(t, got.Pixels), want)
	}
}

func TestSaveImageRGBARGBAAndBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writePNG(t, p, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	old, _ := os.ReadFile(p)

	pix := []byte{9, 8, 7, 5, 255, 0, 0, 255}
	res, err := saveImageRGBA(p, p, 2, 1, b64(pix), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != "rgba" || res.Backup == "" || res.Bytes == 0 {
		t.Fatalf("result differs: %+v", res)
	}
	bak, err := os.ReadFile(res.Backup)
	if err != nil || !bytes.Equal(bak, old) {
		t.Errorf("backup differs from the original: %v", err)
	}
	if filepath.Base(filepath.Dir(filepath.Dir(res.Backup))) != common.BackupDirName {
		t.Errorf("backup location: %s", res.Backup)
	}
	back, err := loadImageRGBA(p)
	if err != nil || !bytes.Equal(decodeB64(t, back.Pixels), pix) {
		t.Errorf("re-read pixels differ: %v %v", err, back)
	}

	// With backup=false no backup is made, and a new file is not a backup target either.
	n := filepath.Join(dir, "new.png")
	r2, err := saveImageRGBA(n, "", 2, 1, b64(pix), true)
	if err != nil || r2.Backup != "" {
		t.Errorf("a new file should have no backup: %+v %v", r2, err)
	}
	r3, err := saveImageRGBA(p, p, 2, 1, b64(pix), false)
	if err != nil || r3.Backup != "" {
		t.Errorf("backed up although backup=false: %+v %v", r3, err)
	}
}

func TestSaveImageRGBAValidation(t *testing.T) {
	dir := t.TempDir()
	if _, err := saveImageRGBA(filepath.Join(dir, "a.txt"), "", 1, 1, b64(make([]byte, 4)), false); err == nil {
		t.Error("a non-PNG extension should be rejected")
	}
	if _, err := saveImageRGBA(filepath.Join(dir, "a.png"), "", 2, 2, b64(make([]byte, 4)), false); err == nil {
		t.Error("a size mismatch should be rejected")
	}
	if _, err := saveImageRGBA(filepath.Join(dir, "a.png"), "", 0, 2, "", false); err == nil {
		t.Error("a zero size should be rejected")
	}
}

// Indexed PNG round trip: keep the original palette order, append new colors at the end, transparency uses the original transparent index.
func TestSaveIndexedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "idx.png")
	pal := color.Palette{
		color.NRGBA{10, 10, 10, 255},
		color.NRGBA{255, 255, 255, 0}, // transparent is entry 1
		color.NRGBA{200, 0, 0, 255},
		color.NRGBA{0, 200, 0, 255},
	}
	img := image.NewPaletted(image.Rect(0, 0, 4, 1), pal)
	img.Pix = []uint8{0, 1, 2, 3}
	writePNG(t, p, img)

	// Change green to blue and leave the transparent pixel as (arbitrary RGB, alpha 0).
	pix := []byte{
		10, 10, 10, 255,
		77, 66, 55, 0,
		200, 0, 0, 255,
		0, 0, 200, 255,
	}
	res, err := saveImageRGBA(p, p, 4, 1, b64(pix), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != "indexed" {
		t.Fatalf("should be saved as indexed: %+v", res)
	}
	data, _ := os.ReadFile(p)
	dec, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := dec.(*image.Paletted)
	if !ok {
		t.Fatalf("not Paletted: %T", dec)
	}
	// Original entries kept (including the unused green) + new blue added
	if len(got.Palette) != 5 {
		t.Fatalf("palette size = %d, want 5", len(got.Palette))
	}
	for i, c := range pal {
		w := color.NRGBAModel.Convert(c).(color.NRGBA)
		g := color.NRGBAModel.Convert(got.Palette[i]).(color.NRGBA)
		if w != g {
			t.Errorf("palette %d = %v, want %v", i, g, w)
		}
	}
	if got.Pix[0] != 0 || got.Pix[1] != 1 || got.Pix[2] != 2 || got.Pix[3] != 4 {
		t.Errorf("indices = %v", got.Pix)
	}

	// Loaded again, the transparent pixel has the color of the original transparent entry and the rest are unchanged.
	back, err := loadImageRGBA(p)
	if err != nil {
		t.Fatal(err)
	}
	bp := decodeB64(t, back.Pixels)
	if bp[7] != 0 || !bytes.Equal(bp[8:], pix[8:]) || !bytes.Equal(bp[:4], pix[:4]) {
		t.Errorf("round-trip result differs: %v", bp)
	}
}

func TestSaveIndexedAddsTransparentAtZero(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "idx.png")
	pal := color.Palette{color.NRGBA{1, 1, 1, 255}, color.NRGBA{2, 2, 2, 255}}
	img := image.NewPaletted(image.Rect(0, 0, 2, 1), pal)
	img.Pix = []uint8{0, 1}
	writePNG(t, p, img)

	pix := []byte{0, 0, 0, 0, 2, 2, 2, 255}
	res, err := saveImageRGBA(p, p, 2, 1, b64(pix), false)
	if err != nil || res.Format != "indexed" {
		t.Fatalf("%+v %v", res, err)
	}
	data, _ := os.ReadFile(p)
	dec, _ := png.Decode(bytes.NewReader(data))
	got := dec.(*image.Paletted)
	if c := color.NRGBAModel.Convert(got.Palette[0]).(color.NRGBA); c.A != 0 {
		t.Errorf("entry 0 should be transparent: %v", c)
	}
	if got.Pix[0] != 0 || got.Pix[1] != 2 {
		t.Errorf("indices = %v", got.Pix)
	}
}

func TestSaveIndexedFallsBackOver256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "idx.png")
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.NRGBA{1, 1, 1, 255}})
	writePNG(t, p, img)

	const n = 300
	pix := make([]byte, n*4)
	for i := 0; i < n; i++ {
		pix[i*4], pix[i*4+1], pix[i*4+2], pix[i*4+3] = uint8(i), uint8(i>>8), 9, 255
	}
	res, err := saveImageRGBA(p, p, n, 1, b64(pix), false)
	if err != nil || res.Format != "rgba" {
		t.Fatalf("more than 256 colors should be rgba: %+v %v", res, err)
	}
	data, _ := os.ReadFile(p)
	dec, _ := png.Decode(bytes.NewReader(data))
	if _, ok := dec.(*image.Paletted); ok {
		t.Errorf("should not be indexed: %T", dec)
	}
}

func TestSaveIndexedDropsUnusedWhenFull(t *testing.T) {
	// If only some of the original 256 colors are used and new colors are added, unused original entries are dropped to fit within 256.
	dir := t.TempDir()
	p := filepath.Join(dir, "idx.png")
	pal := make(color.Palette, 256)
	for i := range pal {
		pal[i] = color.NRGBA{uint8(i), 0, 0, 255}
	}
	img := image.NewPaletted(image.Rect(0, 0, 256, 1), pal)
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	writePNG(t, p, img)

	pix := []byte{0, 0, 0, 255, 0, 0, 77, 255}
	res, err := saveImageRGBA(p, p, 2, 1, b64(pix), false)
	if err != nil || res.Format != "indexed" || res.Colors != 2 {
		t.Fatalf("%+v %v", res, err)
	}
}

func pixOf(t *testing.T, w, h int, f func(x, y int) color.NRGBA) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, f(x, y))
		}
	}
	return b64(img.Pix)
}

func TestProcessPixelsReduceMaxColors(t *testing.T) {
	a := NewApp()
	// Reduce 3 similar reds + 1 blue to 2 colors
	src := pixOf(t, 4, 1, func(x, y int) color.NRGBA {
		return [...]color.NRGBA{{250, 0, 0, 255}, {252, 2, 0, 255}, {255, 0, 2, 255}, {0, 0, 255, 255}}[x]
	})
	res, err := a.ProcessPixels(JobRequest{Command: "reduce", MaxColors: 2}, 4, 1, src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.Width != 4 || res.Height != 1 {
		t.Fatalf("result differs: %+v", res)
	}
	got := decodeB64(t, res.Pixels)
	seen := map[[4]byte]bool{}
	for i := 0; i < len(got); i += 4 {
		seen[[4]byte{got[i], got[i+1], got[i+2], got[i+3]}] = true
	}
	if len(seen) != 2 {
		t.Errorf("color count = %d, want 2 (%v)", len(seen), got)
	}
}

func TestProcessPixelsUpscaleChangesSize(t *testing.T) {
	a := NewApp()
	src := pixOf(t, 2, 1, func(x, y int) color.NRGBA { return color.NRGBA{uint8(10 + x), 0, 0, 255} })
	res, err := a.ProcessPixels(JobRequest{Command: "upscale", Scale: 3}, 2, 1, src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 6 || res.Height != 3 {
		t.Fatalf("size = %dx%d, want 6x3", res.Width, res.Height)
	}
	got := decodeB64(t, res.Pixels)
	if len(got) != 6*3*4 || got[0] != 10 || got[3*4] != 11 || got[(2*6+5)*4] != 11 {
		t.Errorf("upscaled pixels differ")
	}
}

func TestProcessPixelsSkipAndErrors(t *testing.T) {
	a := NewApp()
	// trim on an image that already has no margin is skipped
	full := pixOf(t, 2, 2, func(x, y int) color.NRGBA { return color.NRGBA{1, 2, 3, 255} })
	res, err := a.ProcessPixels(JobRequest{Command: "trim"}, 2, 2, full)
	if err != nil || !res.Skipped || res.Message == "" || res.Pixels != "" {
		t.Errorf("Skip handling differs: %+v, %v", res, err)
	}
	// A fully transparent image is skipped too
	empty := b64(make([]byte, 2*2*4))
	if res, err := a.ProcessPixels(JobRequest{Command: "trim"}, 2, 2, empty); err != nil || !res.Skipped {
		t.Errorf("transparent image: %+v, %v", res, err)
	}
	// Trimming the margin
	pad := pixOf(t, 4, 4, func(x, y int) color.NRGBA {
		if x == 1 && y == 2 {
			return color.NRGBA{9, 9, 9, 255}
		}
		return color.NRGBA{}
	})
	res, err = a.ProcessPixels(JobRequest{Command: "trim"}, 4, 4, pad)
	if err != nil || res.Skipped || res.Width != 1 || res.Height != 1 {
		t.Errorf("trim: %+v, %v", res, err)
	}

	for name, call := range map[string]func() error{
		"disallowed command":    func() error { _, e := a.ProcessPixels(JobRequest{Command: "slice"}, 2, 2, full); return e },
		"unknown command":       func() error { _, e := a.ProcessPixels(JobRequest{Command: "nope"}, 2, 2, full); return e },
		"invalid size":          func() error { _, e := a.ProcessPixels(JobRequest{Command: "trim"}, 0, 2, full); return e },
		"pixel length mismatch": func() error { _, e := a.ProcessPixels(JobRequest{Command: "trim"}, 3, 2, full); return e },
		"invalid base64":        func() error { _, e := a.ProcessPixels(JobRequest{Command: "trim"}, 2, 2, "!!"); return e },
		"invalid scale":         func() error { _, e := a.ProcessPixels(JobRequest{Command: "upscale", Scale: 1}, 2, 2, full); return e },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: should be an error", name)
		}
	}
}

func TestProcessPixelsRecolorWithPaletteFile(t *testing.T) {
	a := NewApp()
	dir := t.TempDir()
	pal := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	pal.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	pal.SetNRGBA(1, 0, color.NRGBA{0, 0, 255, 255})
	p := filepath.Join(dir, "pal.png")
	writePNG(t, p, pal)

	src := pixOf(t, 2, 1, func(x, y int) color.NRGBA {
		return [...]color.NRGBA{{240, 20, 20, 255}, {20, 20, 240, 128}}[x]
	})
	res, err := a.ProcessPixels(JobRequest{Command: "recolor", Palette: p}, 2, 1, src)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeB64(t, res.Pixels)
	want := []byte{255, 0, 0, 255, 0, 0, 255, 128}
	if !bytes.Equal(got, want) {
		t.Errorf("recolor = %v, want %v", got, want)
	}
}

func TestPaletteStrip(t *testing.T) {
	img, err := paletteStrip([]string{"#ff0000", "00ff00", " #0000FF "})
	if err != nil {
		t.Fatal(err)
	}
	n := common.ToNRGBA(img)
	if n.Rect.Dx() != 3 || n.Rect.Dy() != 1 {
		t.Fatalf("size = %v", n.Rect)
	}
	want := []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255}
	if !bytes.Equal(n.Pix, want) {
		t.Errorf("pixels = %v, want %v", n.Pix, want)
	}
	if _, err := paletteStrip(nil); err == nil {
		t.Error("an empty list should be an error")
	}
	if _, err := paletteStrip([]string{"#12"}); err == nil {
		t.Error("an invalid color should be an error")
	}
	if _, err := paletteStrip(make([]string, 257)); err == nil {
		t.Error("257 colors should be an error")
	}
}

func TestSavePaletteColorsRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	for _, k := range []string{"APPDATA", "XDG_CONFIG_HOME", "HOME"} {
		t.Setenv(k, dir)
	}
	a := NewApp()
	cols := []string{"#102030", "#405060"}
	if ok, err := a.PaletteExists("my-pal"); err != nil || ok {
		t.Fatalf("should not exist at first: %v %v", ok, err)
	}
	info, err := a.SavePaletteColors("My-Pal", cols, false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "my-pal" || len(info.Colors) != 2 || info.Colors[0] != "#102030" {
		t.Errorf("save result = %+v", info)
	}
	if ok, _ := a.PaletteExists("my-pal"); !ok {
		t.Error("should exist after saving")
	}
	if _, err := a.SavePaletteColors("my-pal", cols[:1], false); err == nil {
		t.Error("the same name should be rejected")
	}
	info, err = a.SavePaletteColors("my-pal", cols[:1], true)
	if err != nil || len(info.Colors) != 1 {
		t.Errorf("overwrite = %+v, %v", info, err)
	}
	if _, err := a.SavePaletteColors("Bad Name", cols, false); err == nil {
		t.Error("an invalid name should be rejected")
	}
}

func TestProcessPixelsDespeckle(t *testing.T) {
	a := NewApp()
	src := pixOf(t, 6, 4, func(x, y int) color.NRGBA {
		if x < 3 && y < 3 || (x == 5 && y == 3) { // a 3x3 blob + 1 isolated pixel
			return color.NRGBA{9, 9, 9, 255}
		}
		return color.NRGBA{}
	})
	res, err := a.ProcessPixels(JobRequest{Command: "despeckle", MinSize: 3, Diagonal: true}, 6, 4, src)
	if err != nil || res.Skipped || res.Width != 6 || res.Height != 4 {
		t.Fatalf("despeckle: %+v, %v", res, err)
	}
	got := decodeB64(t, res.Pixels)
	if got[(3*6+5)*4+3] != 0 || got[(1*6+1)*4+3] != 255 {
		t.Error("only the isolated pixel should be removed")
	}
	if res, err := a.ProcessPixels(JobRequest{Command: "despeckle", MinSize: 3, Diagonal: true}, 6, 4, res.Pixels); err != nil || !res.Skipped {
		t.Errorf("with nothing to remove it should be skipped: %+v, %v", res, err)
	}
	if _, err := a.ProcessPixels(JobRequest{Command: "despeckle", MinSize: 0}, 6, 4, src); err == nil {
		t.Error("MinSize 0 should be an error")
	}
}

func TestEditorDirtyCount(t *testing.T) {
	a := NewApp()
	a.SetEditorDirtyCount(3)
	if a.editorDirty.Load() != 3 {
		t.Error("document count was not stored")
	}
	a.SetEditorDirty(false)
	if a.editorDirty.Load() != 0 {
		t.Error("SetEditorDirty(false) should be 0")
	}
	a.SetEditorDirty(true)
	if a.editorDirty.Load() != 1 {
		t.Error("SetEditorDirty(true) should be 1")
	}
	if !strings.Contains(dirtyMessage(3), "3개") {
		t.Errorf("message: %q", dirtyMessage(3))
	}
}
