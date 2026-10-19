package processor

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"sprite-tools/pkg/common"
)

// GetPalettesDir: 실행 파일(바이너리)이 있는 디렉터리 내 palettes 경로를 반환합니다.
func GetPalettesDir() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("실행 파일 경로를 가져올 수 없습니다: %v", err)
	}
	execDir := filepath.Dir(execPath)
	return filepath.Join(execDir, "palettes"), nil
}

// EnsurePalettesDirExists: 실행 파일 위치에 palettes 디렉터리가 없으면 생성합니다.
func EnsurePalettesDirExists() (string, error) {
	dir, err := GetPalettesDir()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// DownloadPalette: Lospec에서 팔레트 1x PNG 이미지를 실행 파일 위치의 palettes 폴더에 저장합니다.
func DownloadPalette(name string) (string, error) {
	palettesDir, err := EnsurePalettesDirExists()
	if err != nil {
		return "", err
	}

	cleanName := strings.ToLower(strings.TrimSpace(name))
	savePath := filepath.Join(palettesDir, cleanName+".png")

	if info, err := os.Stat(savePath); err == nil && info.Size() > 0 {
		file, err := os.Open(savePath)
		if err == nil {
			_, err = png.Decode(file)
			file.Close()
			if err == nil {
				fmt.Printf("[PALETTE] 기존 팔레트 로드됨: %s\n", savePath)
				return savePath, nil
			}
		}
		os.Remove(savePath)
	}

	url := fmt.Sprintf("https://lospec.com/palette-list/%s-1x.png", cleanName)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("다운로드 실패: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("존재하지 않는 팔레트입니다: '%s' (HTTP %d)", cleanName, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("응답 읽기 실패: %v", err)
	}

	_, err = png.Decode(bytes.NewReader(bodyBytes))
	if err != nil {
		errMsg := strings.TrimSpace(string(bodyBytes))
		if len(errMsg) > 80 {
			errMsg = errMsg[:80] + "..."
		}
		return "", fmt.Errorf("존재하지 않는 팔레트입니다: '%s' (응답: %s)", cleanName, errMsg)
	}

	out, err := os.Create(savePath)
	if err != nil {
		return "", fmt.Errorf("팔레트 파일 생성 실패: %v", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, bytes.NewReader(bodyBytes)); err != nil {
		os.Remove(savePath)
		return "", fmt.Errorf("팔레트 저장 실패: %v", err)
	}

	fmt.Printf("[PALETTE] 성공적으로 다운로드됨: %s\n", savePath)
	return savePath, nil
}

// ListLocalPalettes: 실행 파일 위치의 palettes 디렉터리에 저장된 팔레트 목록을 출력합니다.
func ListLocalPalettes() ([]string, error) {
	palettesDir, err := EnsurePalettesDirExists()
	if err != nil {
		return nil, err
	}

	files, err := os.ReadDir(palettesDir)
	if err != nil {
		return nil, err
	}

	var palettes []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(strings.ToLower(file.Name()), ".png") {
			palettes = append(palettes, strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())))
		}
	}
	return palettes, nil
}

// LoadPaletteColors: 팔레트 PNG 파일에서 고유 색상 목록 및 OKLab 데이터 목록을 추출합니다.
func LoadPaletteColors(palettePath string) ([]color.Color, []common.OKLab, error) {
	file, err := os.Open(palettePath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	img, err := png.Decode(file)
	if err != nil {
		return nil, nil, err
	}

	bounds := img.Bounds()
	colorSet := make(map[color.Color]bool)
	var rawColors []color.Color
	var oklabColors []common.OKLab

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.At(x, y)
			_, _, _, a := c.RGBA()

			if a == 0 {
				continue
			}

			if !colorSet[c] {
				colorSet[c] = true
				rawColors = append(rawColors, c)
				oklabColors = append(oklabColors, common.ColorToOKLab(c))
			}
		}
	}

	if len(rawColors) == 0 {
		return nil, nil, fmt.Errorf("팔레트 파일에서 유효한 색상을 찾을 수 없습니다: %s", palettePath)
	}

	return rawColors, oklabColors, nil
}

// ConvertToPalette: 입력 이미지의 색상을 OKLab 거리 기준 가장 가까운 팔레트 색상으로 변환합니다.
func ConvertToPalette(inputPath, outputPath string, paletteColors []color.Color, paletteOKLabs []common.OKLab) error {
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
	dst := image.NewRGBA(bounds)

	colorCache := make(map[color.Color]color.Color)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := src.At(x, y)

			_, _, _, a := c.RGBA()
			if a == 0 {
				dst.Set(x, y, color.Transparent)
				continue
			}

			if mapped, ok := colorCache[c]; ok {
				dst.Set(x, y, mapped)
				continue
			}

			pixelOKLab := common.ColorToOKLab(c)

			minDist := math.MaxFloat64
			bestMatchIdx := 0

			for i, pOKLab := range paletteOKLabs {
				dist := common.DeltaOKLabWeighted(pixelOKLab, pOKLab)
				if dist < minDist {
					minDist = dist
					bestMatchIdx = i
				}
			}

			matchedColor := paletteColors[bestMatchIdx]
			colorCache[c] = matchedColor
			dst.Set(x, y, matchedColor)
		}
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if err := png.Encode(out, dst); err != nil {
		return err
	}

	fmt.Printf("[CONVERTED] %s -> %s\n", filepath.Base(inputPath), filepath.Base(outputPath))
	return nil
}