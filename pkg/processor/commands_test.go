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

// solid: 한 가지 색으로 채운 w x h NRGBA 이미지를 만듭니다.
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
		t.Fatalf("크기 %v", n.Rect)
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
	// 확대 후 감지 배율로 축소하면 원본과 같아야 합니다.
	back, _, err := DownscaleImage(out, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !equalNRGBA(back.(*image.NRGBA), src) {
		t.Error("확대 → 축소 결과가 원본과 다름")
	}
	for _, bad := range []int{0, 1, 33, -2} {
		if _, _, err := UpscaleImage(src, bad); err == nil {
			t.Errorf("배율 %d 는 에러여야 합니다", bad)
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
	src.SetNRGBA(5, 4, color.NRGBA{0, 0, 255, 1}) // 알파 1도 내용으로 취급

	out, msg, err := TrimImage(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := out.(*image.NRGBA)
	if n.Rect.Dx() != 3 || n.Rect.Dy() != 3 {
		t.Fatalf("크기 %v", n.Rect)
	}
	if n.NRGBAAt(0, 0) != red || n.NRGBAAt(2, 2).A != 1 {
		t.Error("내용이 올바르게 복사되지 않음")
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
		t.Errorf("여백 적용 결과가 올바르지 않음: %v", n.Rect)
	}

	// 이미 꽉 찬 이미지는 건너뜀
	if _, _, err := TrimImage(solid(4, 4, red), 0); !common.IsSkip(err) {
		t.Errorf("꽉 찬 이미지: %v", err)
	}
	// 여백이 이미 pad 와 같으면 건너뜀
	padded := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	padded.SetNRGBA(2, 2, red)
	if _, _, err := TrimImage(padded, 2); !common.IsSkip(err) {
		t.Errorf("이미 여백이 맞는 이미지: %v", err)
	}
	// 완전 투명
	if _, _, err := TrimImage(image.NewNRGBA(image.Rect(0, 0, 4, 4)), 1); !common.IsSkip(err) {
		t.Errorf("투명 이미지: %v", err)
	}
	if _, _, err := TrimImage(src, -1); err == nil {
		t.Error("음수 여백은 에러여야 합니다")
	}
}

func TestExtractPaletteImage(t *testing.T) {
	a, b, c := color.NRGBA{10, 20, 30, 255}, color.NRGBA{200, 0, 0, 255}, color.NRGBA{0, 200, 0, 255}
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	// 등장 순서 c, a, b, a ... 빈도: a=3, b=2, c=2 (b 가 c 보다 늦게 등장)
	seq := []color.NRGBA{c, a, b, a, a, b, c, {255, 255, 255, 0}}
	for i, col := range seq {
		src.SetNRGBA(i%4, i/4, col)
	}
	// a 와 RGB 가 같은 반투명 픽셀은 a 로 합쳐져야 합니다.
	src.SetNRGBA(3, 1, color.NRGBA{10, 20, 30, 100})

	out, msg, err := ExtractPaletteImage(src)
	if err != nil {
		t.Fatal(err)
	}
	n := out.(*image.NRGBA)
	if n.Rect.Dx() != 3 || n.Rect.Dy() != 1 {
		t.Fatalf("크기 %v", n.Rect)
	}
	want := []color.NRGBA{a, c, b} // a=4, c=2(먼저 등장), b=2
	for i, w := range want {
		if got := n.NRGBAAt(i, 0); got != w {
			t.Errorf("색 %d = %v, want %v", i, got, w)
		}
	}
	if !strings.Contains(msg, "3개") {
		t.Errorf("msg = %q", msg)
	}

	if _, _, err := ExtractPaletteImage(image.NewNRGBA(image.Rect(0, 0, 3, 3))); !common.IsSkip(err) {
		t.Errorf("투명 이미지: %v", err)
	}

	many := image.NewNRGBA(image.Rect(0, 0, 300, 1))
	for x := 0; x < 300; x++ {
		many.SetNRGBA(x, 0, color.NRGBA{uint8(x), uint8(x >> 8), 1, 255})
	}
	_, _, err = ExtractPaletteImage(many)
	if err == nil || common.IsSkip(err) || !strings.Contains(err.Error(), "reduce") {
		t.Errorf("256색 초과: %v", err)
	}
}

func TestNormalizePaletteName(t *testing.T) {
	if got, err := NormalizePaletteName("  My-Pal2 "); err != nil || got != "my-pal2" {
		t.Errorf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "-x", "a b", "a_b", "../x", "한글"} {
		if _, err := NormalizePaletteName(bad); err == nil {
			t.Errorf("%q 는 에러여야 합니다", bad)
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
	// 3장 → 2열 2행, 칸 4x4
	if sheet.Rect.Dx() != 8 || sheet.Rect.Dy() != 8 {
		t.Fatalf("크기 %v", sheet.Rect)
	}
	if !strings.Contains(msg, "3프레임 → 2x2 시트, 8x8") {
		t.Errorf("msg = %q", msg)
	}
	if sheet.NRGBAAt(3, 3) != r || sheet.NRGBAAt(4, 0) != g || sheet.NRGBAAt(6, 0).A != 0 || sheet.NRGBAAt(4, 3).A != 0 {
		t.Error("왼쪽 위 정렬 배치가 올바르지 않음")
	}
	if sheet.NRGBAAt(0, 4) != b || sheet.NRGBAAt(0, 6).A != 0 {
		t.Error("두 번째 줄 배치가 올바르지 않음")
	}

	// 간격과 가운데 정렬
	sheet, _, err = PackSheet(imgs, 3, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if sheet.Rect.Dx() != 4*3+2*2 || sheet.Rect.Dy() != 4 {
		t.Fatalf("크기 %v", sheet.Rect)
	}
	// 두 번째 칸 x 시작 = 6, g(2x3)는 (7,0)~(8,2)
	if sheet.NRGBAAt(7, 0) != g || sheet.NRGBAAt(7, 2) != g || sheet.NRGBAAt(7, 3).A != 0 || sheet.NRGBAAt(6, 0).A != 0 {
		t.Error("가운데 정렬 배치가 올바르지 않음")
	}
	if sheet.NRGBAAt(4, 0).A != 0 {
		t.Error("간격은 투명이어야 합니다")
	}

	if _, _, err := PackImages(nil, 0, 0); err == nil {
		t.Error("빈 목록은 에러여야 합니다")
	}
	// 열 수가 개수보다 크면 개수로 제한
	sheet, _, _ = PackImages(imgs, 10, 0)
	if sheet.Rect.Dx() != 12 || sheet.Rect.Dy() != 4 {
		t.Errorf("크기 %v", sheet.Rect)
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

// pack → slice 왕복: 같은 크기의 프레임을 합쳤다가 다시 자르면 원본 프레임이 나와야 합니다.
func TestPackSliceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	var frames []*image.NRGBA
	var paths []string
	for i := 1; i <= 7; i++ {
		f := solid(6, 5, color.NRGBA{uint8(i * 30), uint8(255 - i*10), uint8(i), 255})
		f.SetNRGBA(i%6, i%5, color.NRGBA{1, 2, 3, 77})
		frames = append(frames, f)
		p := filepath.Join(dir, "in", "frame-"+string(rune('0'+i))+".png")
		if i >= 5 { // 자연 정렬 확인용으로 두 자리 이름 섞기
			p = filepath.Join(dir, "in", "frame-1"+string(rune('0'+i))+".png")
		}
		writeTestPNG(t, p, f)
		paths = append(paths, p)
	}
	// 입력 순서를 섞어서 넘겨도 자연 정렬되어야 합니다 (frame-1..4, frame-15..17).
	shuffled := []string{paths[6], paths[2], paths[0], paths[5], paths[1], paths[4], paths[3]}

	sheetPath := filepath.Join(dir, "sheet.png")
	if _, err := PackFiles(shuffled, sheetPath, 4, 0, false, false); err != nil {
		t.Fatal(err)
	}
	// 이미 있는 출력은 force 없이 거부
	if _, err := PackFiles(shuffled, sheetPath, 4, 0, false, false); err == nil {
		t.Error("기존 파일 덮어쓰기는 force 없이 거부되어야 합니다")
	}
	if _, err := PackFiles(shuffled, sheetPath, 4, 0, false, true); err != nil {
		t.Errorf("force 로는 덮어써야 합니다: %v", err)
	}
	// 입력 파일은 force 여도 덮어쓸 수 없음
	if _, err := PackFiles(shuffled, paths[0], 4, 0, false, true); err == nil {
		t.Error("입력 파일 덮어쓰기는 항상 거부되어야 합니다")
	}

	src, err := common.LoadPNG(sheetPath)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := SliceImage(src, 6, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 { // 2행 x 4열, 마지막 칸은 빈 프레임
		t.Fatalf("프레임 %d개", len(got))
	}
	for i, want := range frames {
		if !equalNRGBA(got[i].Image, want) {
			t.Errorf("프레임 %d 가 원본과 다름", i+1)
		}
	}
}

func TestSavePaletteRefusesExisting(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir()) // Windows 의 UserConfigDir
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
		t.Error("같은 이름은 force 없이 거부되어야 합니다")
	}
	if _, err := SavePalette("test-pal", pal, true); err != nil {
		t.Errorf("force: %v", err)
	}
	if _, err := SavePalette("bad name", pal, true); err == nil {
		t.Error("잘못된 이름은 거부되어야 합니다")
	}
	raw, lab, err := LoadPaletteColors(path)
	if err != nil || len(raw) != 1 || len(lab) != 1 {
		t.Errorf("저장한 팔레트를 읽을 수 있어야 합니다: %v %v", raw, err)
	}
}

func TestDespeckleImage(t *testing.T) {
	red := color.NRGBA{255, 0, 0, 255}
	blue := color.NRGBA{0, 0, 255, 128}
	src := image.NewNRGBA(image.Rect(0, 0, 10, 8))
	// 큰 덩어리: 3x3 (9픽셀)
	for y := 1; y <= 3; y++ {
		for x := 1; x <= 3; x++ {
			src.SetNRGBA(x, y, red)
		}
	}
	src.SetNRGBA(8, 1, blue)                    // 외톨이 1픽셀
	src.SetNRGBA(7, 5, blue)                    // 대각선으로만 이어진 2픽셀
	src.SetNRGBA(8, 6, blue)                    //
	src.SetNRGBA(0, 7, color.NRGBA{9, 9, 9, 1}) // 알파 1 도 내용

	// 8방향: 대각선 2픽셀은 한 덩어리(크기 2), 외톨이 2개(1픽셀) → 최소 2 면 외톨이 2개 제거
	out, msg, err := DespeckleImage(src, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	n := out.(*image.NRGBA)
	if n.NRGBAAt(8, 1).A != 0 || n.NRGBAAt(0, 7) != (color.NRGBA{}) {
		t.Error("외톨이가 지워지지 않음")
	}
	if n.NRGBAAt(7, 5) != blue || n.NRGBAAt(8, 6) != blue || n.NRGBAAt(2, 2) != red {
		t.Error("남겨야 할 픽셀이 바뀜")
	}
	if msg != "조각 2개 제거 (픽셀 2개)" {
		t.Errorf("msg = %q", msg)
	}
	if src.NRGBAAt(8, 1) != blue {
		t.Error("입력 이미지가 바뀜")
	}

	// 4방향: 대각선 2픽셀도 각각 1픽셀 조각이 되어 지워짐 (4개)
	out, msg, err = DespeckleImage(src, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if n = out.(*image.NRGBA); n.NRGBAAt(7, 5).A != 0 || n.NRGBAAt(8, 6).A != 0 || msg != "조각 4개 제거 (픽셀 4개)" {
		t.Errorf("4방향 결과가 다름: %q", msg)
	}

	// 최소 크기 10: 3x3(9)도 지워져 모두 사라짐
	out, msg, err = DespeckleImage(src, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if n = out.(*image.NRGBA); n.NRGBAAt(2, 2).A != 0 || msg != "조각 4개 제거 (픽셀 13개)" {
		t.Errorf("최소 10: %q", msg)
	}

	// 지울 것이 없으면 Skip, 잘못된 값은 오류
	if _, _, err := DespeckleImage(src, 1, true); !common.IsSkip(err) {
		t.Errorf("minSize 1: %v", err)
	}
	if _, _, err := DespeckleImage(solid(4, 4, red), 4, true); !common.IsSkip(err) {
		t.Errorf("조각 없음: %v", err)
	}
	if _, _, err := DespeckleImage(image.NewNRGBA(image.Rect(0, 0, 4, 4)), 3, true); !common.IsSkip(err) {
		t.Errorf("완전 투명: %v", err)
	}
	if _, _, err := DespeckleImage(src, 0, true); err == nil || common.IsSkip(err) {
		t.Errorf("minSize 0 는 오류여야 함: %v", err)
	}
}
