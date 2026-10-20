package processor

import (
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

// MaxExtractColors: 추출할 수 있는 최대 색상 수 (Lospec 팔레트 PNG 관례).
const MaxExtractColors = 256

// ExtractPaletteImage: 이미지에 쓰인 색상을 Lospec 스타일 팔레트 PNG(색 1개 = 1px, 한 줄)로 만듭니다.
// 완전 투명한 픽셀은 무시하고, 나머지는 알파를 255로 맞춘 뒤 같은 색을 합칩니다.
// 많이 쓰인 색상 순서이며 동률이면 먼저 등장한 색이 앞섭니다.
// 색상이 256개를 넘으면 에러, 색상이 없으면 Skip 에러를 반환합니다.
func ExtractPaletteImage(src image.Image) (image.Image, string, error) {
	n := common.ToNRGBA(src)
	index := make(map[uint32]int)
	var keys []uint32
	var counts []int

	pix := n.Pix
	lastKey, lastIdx := uint32(0), -1
	for i := 0; i < len(pix); i += 4 {
		p := pix[i : i+4 : i+4]
		if p[3] == 0 {
			continue
		}
		key := common.PackNRGBA(p[0], p[1], p[2], 255)
		if lastIdx < 0 || key != lastKey {
			idx, ok := index[key]
			if !ok {
				idx = len(keys)
				index[key] = idx
				keys = append(keys, key)
				counts = append(counts, 0)
			}
			lastKey, lastIdx = key, idx
		}
		counts[lastIdx]++
	}

	if len(keys) == 0 {
		return nil, "", common.Skip("skip.extract.no_colors")
	}
	if len(keys) > MaxExtractColors {
		return nil, "", i18n.Errorf("err.extract.too_many_colors", len(keys), MaxExtractColors)
	}

	order := make([]int, len(keys))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return counts[b] - counts[a] })

	dst := image.NewNRGBA(image.Rect(0, 0, len(keys), 1))
	for x, i := range order {
		r, g, b, a := common.UnpackNRGBA(keys[i])
		copy(dst.Pix[x*4:], []uint8{r, g, b, a})
	}
	return dst, i18n.T("msg.extract.result", len(keys)), nil
}

// NormalizePaletteName: 팔레트 이름을 소문자로 바꾸고 Lospec 이름 규칙(영문 소문자, 숫자, '-')을 검사합니다.
func NormalizePaletteName(name string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(name))
	if !paletteNamePattern.MatchString(clean) {
		return "", i18n.Errorf("err.palette.name_invalid", name)
	}
	return clean, nil
}

// SavePalette: 팔레트 PNG 를 공용 팔레트 폴더에 이름으로 저장해 recolor -p <이름> 에서 쓸 수 있게 합니다.
// 같은 이름의 팔레트가 이미 있으면(실행 파일 옆 palettes 폴더 포함) force 가 아닌 한 거부합니다.
func SavePalette(name string, palette image.Image, force bool) (string, error) {
	clean, err := NormalizePaletteName(name)
	if err != nil {
		return "", err
	}
	if !force {
		for _, dir := range PaletteDirs() {
			if _, err := os.Stat(filepath.Join(dir, clean+".png")); err == nil {
				return "", i18n.Errorf("err.palette.exists", clean)
			}
		}
	}
	dir, err := EnsurePalettesDirExists()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, clean+".png")
	if err := common.SavePNG(path, palette); err != nil {
		return "", i18n.Errorf("err.palette.save_failed", err)
	}
	return path, nil
}

// Extract: 파일 단위 ExtractPaletteImage. saveName 이 있으면 공용 팔레트 폴더에도 저장합니다.
func Extract(inputPath, outputPath, saveName string, force bool) (string, error) {
	if saveName != "" {
		if _, err := NormalizePaletteName(saveName); err != nil {
			return "", err
		}
	}
	src, err := common.LoadPNG(inputPath)
	if err != nil {
		return "", err
	}
	pal, msg, err := ExtractPaletteImage(src)
	if err != nil {
		return "", err
	}
	if saveName != "" {
		// 이름 충돌을 먼저 확인해, 실패해도 출력 파일이 남지 않게 합니다.
		saved, err := SavePalette(saveName, pal, force)
		if err != nil {
			return "", err
		}
		msg += i18n.T("msg.extract.saved", saved)
	}
	if err := common.SavePNG(outputPath, pal); err != nil {
		return "", err
	}
	return msg, nil
}
