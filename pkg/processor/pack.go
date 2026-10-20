package processor

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"sprite-tools/pkg/common"
)

// PackImages: 이미지들을 한 장의 스프라이트 시트로 합칩니다 (slice 의 반대).
// 칸 크기는 가장 큰 너비 x 가장 큰 높이이고 작은 이미지는 칸의 왼쪽 위에 놓입니다.
// cols 가 0 이하면 ceil(sqrt(개수)) 열, gap 은 칸 사이의 투명 간격(px)입니다.
func PackImages(images []image.Image, cols, gap int) (*image.NRGBA, string, error) {
	return PackSheet(images, cols, gap, false)
}

// PackSheet: PackImages 와 같지만 center 가 true 면 작은 이미지를 칸의 가운데에 놓습니다.
func PackSheet(images []image.Image, cols, gap int, center bool) (*image.NRGBA, string, error) {
	if len(images) == 0 {
		return nil, "", errors.New("합칠 이미지가 없습니다")
	}
	if gap < 0 {
		return nil, "", fmt.Errorf("간격은 0 이상이어야 합니다: %d", gap)
	}
	if cols < 0 {
		return nil, "", fmt.Errorf("열 수는 0 이상이어야 합니다: %d", cols)
	}
	count := len(images)
	if cols == 0 {
		cols = int(math.Ceil(math.Sqrt(float64(count))))
	}
	cols = min(cols, count)
	rows := (count + cols - 1) / cols

	cellW, cellH := 0, 0
	nrgba := make([]*image.NRGBA, count)
	for i, img := range images {
		nrgba[i] = common.ToNRGBA(img)
		cellW = max(cellW, nrgba[i].Rect.Dx())
		cellH = max(cellH, nrgba[i].Rect.Dy())
	}
	if cellW == 0 || cellH == 0 {
		return nil, "", errors.New("크기가 0인 이미지가 있습니다")
	}
	sheetW, sheetH := cols*cellW+(cols-1)*gap, rows*cellH+(rows-1)*gap
	if sheetW*sheetH > maxOutputPixels {
		return nil, "", fmt.Errorf("시트가 너무 큽니다: %dx%d", sheetW, sheetH)
	}

	dst := image.NewNRGBA(image.Rect(0, 0, sheetW, sheetH))
	for i, n := range nrgba {
		w, h := n.Rect.Dx(), n.Rect.Dy()
		x0, y0 := (i%cols)*(cellW+gap), (i/cols)*(cellH+gap)
		if center {
			x0 += (cellW - w) / 2
			y0 += (cellH - h) / 2
		}
		for y := 0; y < h; y++ {
			so := y * n.Stride
			do := (y0+y)*dst.Stride + x0*4
			copy(dst.Pix[do:do+w*4], n.Pix[so:so+w*4])
		}
	}
	msg := fmt.Sprintf("%d프레임 → %dx%d 시트, %dx%d", count, cols, rows, sheetW, sheetH)
	return dst, msg, nil
}

// SortedPaths: 경로 목록을 자연 정렬(frame-2 < frame-10)한 복사본을 반환합니다.
func SortedPaths(paths []string) []string {
	sorted := slices.Clone(paths)
	common.NaturalSort(sorted)
	return sorted
}

// LoadImages: PNG 파일들을 순서대로 읽습니다. 하나라도 실패하면 어느 파일인지 알려 줍니다.
func LoadImages(paths []string) ([]image.Image, error) {
	images := make([]image.Image, len(paths))
	for i, p := range paths {
		img, err := common.LoadPNG(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", p, err)
		}
		images[i] = img
	}
	return images, nil
}

// ErrOutputExists: 출력 파일이 이미 있고 덮어쓰기가 허용되지 않았을 때 반환됩니다.
var ErrOutputExists = errors.New("출력 파일이 이미 있습니다")

// CheckPackOutput: 시트 출력 경로가 안전한지 확인합니다.
// 입력 파일을 덮어쓰는 경로는 항상 거부하고, 이미 있는 다른 파일은 force 가 아니면 ErrOutputExists 로 거부합니다.
func CheckPackOutput(inputs []string, outPath string, force bool) error {
	if !strings.EqualFold(filepath.Ext(outPath), ".png") {
		return fmt.Errorf("출력 파일은 .png 여야 합니다: %s", outPath)
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	for _, in := range inputs {
		if absIn, err := filepath.Abs(in); err == nil && strings.EqualFold(absIn, absOut) {
			return fmt.Errorf("출력 파일이 입력 파일과 같아 덮어쓰게 됩니다: %s", outPath)
		}
	}
	if st, err := os.Stat(outPath); err == nil {
		if st.IsDir() {
			return fmt.Errorf("출력 경로가 폴더입니다: %s", outPath)
		}
		if !force {
			return fmt.Errorf("%w: %s (덮어쓰려면 -force)", ErrOutputExists, outPath)
		}
	}
	return nil
}

// PackFiles: PNG 파일들을 자연 정렬 순서로 한 장의 시트로 합쳐 outPath 에 저장합니다.
func PackFiles(paths []string, outPath string, cols, gap int, center, force bool) (string, error) {
	if err := CheckPackOutput(paths, outPath, force); err != nil {
		return "", err
	}
	images, err := LoadImages(SortedPaths(paths))
	if err != nil {
		return "", err
	}
	sheet, msg, err := PackSheet(images, cols, gap, center)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}
	if err := common.SavePNG(outPath, sheet); err != nil {
		return "", err
	}
	return msg, nil
}
