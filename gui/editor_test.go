package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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
		t.Fatalf("메타 정보가 다릅니다: %+v", got)
	}
	want := []byte{200, 100, 50, 3, 1, 2, 3, 255}
	if !bytes.Equal(decodeB64(t, got.Pixels), want) {
		t.Errorf("픽셀 = %v, want %v", decodeB64(t, got.Pixels), want)
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
		t.Fatalf("인덱스 정보가 다릅니다: %+v", got)
	}
	want := []byte{10, 20, 30, 255, 0, 0, 0, 0, 250, 0, 0, 7}
	if !bytes.Equal(decodeB64(t, got.Pixels), want) {
		t.Errorf("픽셀 = %v, want %v", decodeB64(t, got.Pixels), want)
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
		t.Fatalf("결과가 다릅니다: %+v", res)
	}
	bak, err := os.ReadFile(res.Backup)
	if err != nil || !bytes.Equal(bak, old) {
		t.Errorf("백업이 원본과 다릅니다: %v", err)
	}
	if filepath.Base(filepath.Dir(filepath.Dir(res.Backup))) != common.BackupDirName {
		t.Errorf("백업 위치: %s", res.Backup)
	}
	back, err := loadImageRGBA(p)
	if err != nil || !bytes.Equal(decodeB64(t, back.Pixels), pix) {
		t.Errorf("다시 읽은 픽셀이 다릅니다: %v %v", err, back)
	}

	// backup=false 이면 백업을 만들지 않고, 새 파일도 백업 대상이 아닙니다.
	n := filepath.Join(dir, "new.png")
	r2, err := saveImageRGBA(n, "", 2, 1, b64(pix), true)
	if err != nil || r2.Backup != "" {
		t.Errorf("새 파일은 백업이 없어야 합니다: %+v %v", r2, err)
	}
	r3, err := saveImageRGBA(p, p, 2, 1, b64(pix), false)
	if err != nil || r3.Backup != "" {
		t.Errorf("backup=false 인데 백업됨: %+v %v", r3, err)
	}
}

func TestSaveImageRGBAValidation(t *testing.T) {
	dir := t.TempDir()
	if _, err := saveImageRGBA(filepath.Join(dir, "a.txt"), "", 1, 1, b64(make([]byte, 4)), false); err == nil {
		t.Error("PNG 가 아닌 확장자는 거부해야 합니다")
	}
	if _, err := saveImageRGBA(filepath.Join(dir, "a.png"), "", 2, 2, b64(make([]byte, 4)), false); err == nil {
		t.Error("크기가 맞지 않으면 거부해야 합니다")
	}
	if _, err := saveImageRGBA(filepath.Join(dir, "a.png"), "", 0, 2, "", false); err == nil {
		t.Error("0 크기는 거부해야 합니다")
	}
}

// 인덱스 PNG 왕복: 원본 팔레트 순서 유지, 새 색은 뒤에 추가, 투명은 원본의 투명 인덱스.
func TestSaveIndexedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "idx.png")
	pal := color.Palette{
		color.NRGBA{10, 10, 10, 255},
		color.NRGBA{255, 255, 255, 0}, // 투명이 1번
		color.NRGBA{200, 0, 0, 255},
		color.NRGBA{0, 200, 0, 255},
	}
	img := image.NewPaletted(image.Rect(0, 0, 4, 1), pal)
	img.Pix = []uint8{0, 1, 2, 3}
	writePNG(t, p, img)

	// 초록을 파랑으로 바꾸고 투명 픽셀은 (임의 RGB, 알파 0) 으로 둡니다.
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
		t.Fatalf("인덱스로 저장되어야 합니다: %+v", res)
	}
	data, _ := os.ReadFile(p)
	dec, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := dec.(*image.Paletted)
	if !ok {
		t.Fatalf("Paletted 가 아닙니다: %T", dec)
	}
	// 원본 항목 유지(미사용인 초록 포함) + 새 파랑 추가
	if len(got.Palette) != 5 {
		t.Fatalf("팔레트 수 = %d, want 5", len(got.Palette))
	}
	for i, c := range pal {
		w := color.NRGBAModel.Convert(c).(color.NRGBA)
		g := color.NRGBAModel.Convert(got.Palette[i]).(color.NRGBA)
		if w != g {
			t.Errorf("팔레트 %d = %v, want %v", i, g, w)
		}
	}
	if got.Pix[0] != 0 || got.Pix[1] != 1 || got.Pix[2] != 2 || got.Pix[3] != 4 {
		t.Errorf("인덱스 = %v", got.Pix)
	}

	// 다시 불러오면 투명 픽셀은 원본의 투명 항목 색, 나머지는 그대로입니다.
	back, err := loadImageRGBA(p)
	if err != nil {
		t.Fatal(err)
	}
	bp := decodeB64(t, back.Pixels)
	if bp[7] != 0 || !bytes.Equal(bp[8:], pix[8:]) || !bytes.Equal(bp[:4], pix[:4]) {
		t.Errorf("왕복 결과가 다릅니다: %v", bp)
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
		t.Errorf("0번이 투명이어야 합니다: %v", c)
	}
	if got.Pix[0] != 0 || got.Pix[1] != 2 {
		t.Errorf("인덱스 = %v", got.Pix)
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
		t.Fatalf("256색 초과는 rgba 여야 합니다: %+v %v", res, err)
	}
	data, _ := os.ReadFile(p)
	dec, _ := png.Decode(bytes.NewReader(data))
	if _, ok := dec.(*image.Paletted); ok {
		t.Errorf("인덱스가 아니어야 합니다: %T", dec)
	}
}

func TestSaveIndexedDropsUnusedWhenFull(t *testing.T) {
	// 원본 256색 중 일부만 쓰고 새 색을 더하면, 쓰이지 않는 원본 항목을 버려 256개 이내로 맞춥니다.
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
	// 비슷한 빨강 3가지 + 파랑 1가지 → 2색으로 줄이기
	src := pixOf(t, 4, 1, func(x, y int) color.NRGBA {
		return [...]color.NRGBA{{250, 0, 0, 255}, {252, 2, 0, 255}, {255, 0, 2, 255}, {0, 0, 255, 255}}[x]
	})
	res, err := a.ProcessPixels(JobRequest{Command: "reduce", MaxColors: 2}, 4, 1, src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.Width != 4 || res.Height != 1 {
		t.Fatalf("결과가 다릅니다: %+v", res)
	}
	got := decodeB64(t, res.Pixels)
	seen := map[[4]byte]bool{}
	for i := 0; i < len(got); i += 4 {
		seen[[4]byte{got[i], got[i+1], got[i+2], got[i+3]}] = true
	}
	if len(seen) != 2 {
		t.Errorf("색상 수 = %d, want 2 (%v)", len(seen), got)
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
		t.Fatalf("크기 = %dx%d, want 6x3", res.Width, res.Height)
	}
	got := decodeB64(t, res.Pixels)
	if len(got) != 6*3*4 || got[0] != 10 || got[3*4] != 11 || got[(2*6+5)*4] != 11 {
		t.Errorf("확대 픽셀이 다릅니다")
	}
}

func TestProcessPixelsSkipAndErrors(t *testing.T) {
	a := NewApp()
	// 이미 여백이 없는 이미지의 trim 은 건너뜀
	full := pixOf(t, 2, 2, func(x, y int) color.NRGBA { return color.NRGBA{1, 2, 3, 255} })
	res, err := a.ProcessPixels(JobRequest{Command: "trim"}, 2, 2, full)
	if err != nil || !res.Skipped || res.Message == "" || res.Pixels != "" {
		t.Errorf("Skip 처리가 다릅니다: %+v, %v", res, err)
	}
	// 완전히 투명한 이미지도 건너뜀
	empty := b64(make([]byte, 2*2*4))
	if res, err := a.ProcessPixels(JobRequest{Command: "trim"}, 2, 2, empty); err != nil || !res.Skipped {
		t.Errorf("투명 이미지: %+v, %v", res, err)
	}
	// 여백 자르기
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
		"허용되지 않는 명령": func() error { _, e := a.ProcessPixels(JobRequest{Command: "slice"}, 2, 2, full); return e },
		"알 수 없는 명령":  func() error { _, e := a.ProcessPixels(JobRequest{Command: "nope"}, 2, 2, full); return e },
		"잘못된 크기":     func() error { _, e := a.ProcessPixels(JobRequest{Command: "trim"}, 0, 2, full); return e },
		"픽셀 길이 불일치":  func() error { _, e := a.ProcessPixels(JobRequest{Command: "trim"}, 3, 2, full); return e },
		"잘못된 base64": func() error { _, e := a.ProcessPixels(JobRequest{Command: "trim"}, 2, 2, "!!"); return e },
		"잘못된 배율":     func() error { _, e := a.ProcessPixels(JobRequest{Command: "upscale", Scale: 1}, 2, 2, full); return e },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: 오류가 나야 합니다", name)
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
		t.Fatalf("크기 = %v", n.Rect)
	}
	want := []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255}
	if !bytes.Equal(n.Pix, want) {
		t.Errorf("픽셀 = %v, want %v", n.Pix, want)
	}
	if _, err := paletteStrip(nil); err == nil {
		t.Error("빈 목록은 오류여야 합니다")
	}
	if _, err := paletteStrip([]string{"#12"}); err == nil {
		t.Error("잘못된 색은 오류여야 합니다")
	}
	if _, err := paletteStrip(make([]string, 257)); err == nil {
		t.Error("257색은 오류여야 합니다")
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
		t.Fatalf("처음에는 없어야 합니다: %v %v", ok, err)
	}
	info, err := a.SavePaletteColors("My-Pal", cols, false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "my-pal" || len(info.Colors) != 2 || info.Colors[0] != "#102030" {
		t.Errorf("저장 결과 = %+v", info)
	}
	if ok, _ := a.PaletteExists("my-pal"); !ok {
		t.Error("저장 후에는 있어야 합니다")
	}
	if _, err := a.SavePaletteColors("my-pal", cols[:1], false); err == nil {
		t.Error("같은 이름은 거부해야 합니다")
	}
	info, err = a.SavePaletteColors("my-pal", cols[:1], true)
	if err != nil || len(info.Colors) != 1 {
		t.Errorf("덮어쓰기 = %+v, %v", info, err)
	}
	if _, err := a.SavePaletteColors("Bad Name", cols, false); err == nil {
		t.Error("잘못된 이름은 거부해야 합니다")
	}
}
