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
	"regexp"
	"strings"
	"time"

	"sprite-tools/pkg/common"
)

var paletteNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// ResolvePalette: 팔레트 지정값을 PNG 파일 경로로 바꿉니다.
// 존재하는 .png 파일 경로면 그대로 사용하고, 아니면 Lospec 팔레트 이름으로 보고 캐시 또는 다운로드합니다.
func ResolvePalette(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if strings.EqualFold(filepath.Ext(spec), ".png") {
		if _, err := os.Stat(spec); err != nil {
			return "", fmt.Errorf("팔레트 파일을 찾을 수 없습니다: %s", spec)
		}
		return spec, nil
	}
	return DownloadPalette(spec)
}

// DownloadPalette: Lospec에서 팔레트 1x PNG 이미지를 실행 파일 위치의 palettes 폴더에 저장합니다.
func DownloadPalette(name string) (string, error) {
	cleanName := strings.ToLower(strings.TrimSpace(name))
	if !paletteNamePattern.MatchString(cleanName) {
		return "", fmt.Errorf("잘못된 팔레트 이름입니다: '%s' (영문 소문자, 숫자, '-'만 사용, 예: otterisk-96)", name)
	}

	palettesDir, err := EnsurePalettesDirExists()
	if err != nil {
		return "", err
	}
	savePath := filepath.Join(palettesDir, cleanName+".png")

	if _, err := common.LoadPNG(savePath); err == nil {
		fmt.Printf("팔레트 캐시 사용: %s\n", savePath)
		return savePath, nil
	}

	url := fmt.Sprintf("https://lospec.com/palette-list/%s-1x.png", cleanName)
	fmt.Printf("팔레트 다운로드 중: %s\n", url)
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sprite-tools")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("다운로드 실패 (네트워크 확인): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Lospec에 없는 팔레트입니다: '%s' (HTTP %d). https://lospec.com/palette-list 에서 이름을 확인하세요", cleanName, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("응답 읽기 실패: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(bodyBytes)); err != nil {
		return "", fmt.Errorf("Lospec에 없는 팔레트입니다: '%s' (PNG 응답이 아님)", cleanName)
	}

	if err := common.WriteFileAtomic(savePath, bodyBytes); err != nil {
		return "", fmt.Errorf("팔레트 저장 실패: %v", err)
	}
	fmt.Printf("팔레트 저장됨: %s\n", savePath)
	return savePath, nil
}

// ListLocalPalettes: 실행 파일 위치의 palettes 디렉터리에 저장된 팔레트 목록을 반환합니다.
func ListLocalPalettes() ([]string, error) {
	palettesDir, err := GetPalettesDir()
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(palettesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var palettes []string
	for _, file := range files {
		if !file.IsDir() && strings.EqualFold(filepath.Ext(file.Name()), ".png") {
			palettes = append(palettes, strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())))
		}
	}
	return palettes, nil
}

// LoadPaletteColors: 팔레트 PNG 파일에서 고유 색상 목록 및 OKLab 데이터 목록을 추출합니다.
func LoadPaletteColors(palettePath string) ([]color.NRGBA, []common.OKLab, error) {
	img, err := common.LoadPNG(palettePath)
	if err != nil {
		return nil, nil, err
	}

	bounds := img.Bounds()
	colorSet := make(map[color.NRGBA]bool)
	var rawColors []color.NRGBA
	var oklabColors []common.OKLab

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A == 0 || colorSet[c] {
				continue
			}
			colorSet[c] = true
			rawColors = append(rawColors, c)
			oklabColors = append(oklabColors, common.ColorToOKLab(c))
		}
	}

	if len(rawColors) == 0 {
		return nil, nil, fmt.Errorf("팔레트 파일에서 유효한 색상을 찾을 수 없습니다: %s", palettePath)
	}
	return rawColors, oklabColors, nil
}

// ConvertToPalette: 입력 이미지의 색상을 OKLab 거리 기준 가장 가까운 팔레트 색상으로 변환합니다.
// 원본 픽셀의 알파 값은 그대로 유지합니다.
func ConvertToPalette(inputPath, outputPath string, paletteColors []color.NRGBA, paletteOKLabs []common.OKLab) (string, error) {
	src, err := common.LoadPNG(inputPath)
	if err != nil {
		return "", err
	}

	bounds := src.Bounds()
	dst := image.NewNRGBA(bounds)
	colorCache := make(map[color.Color]color.NRGBA)
	changed := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := src.At(x, y)
			alpha := common.Alpha8(c)
			if alpha == 0 {
				continue
			}

			matched, ok := colorCache[c]
			if !ok {
				pixelOKLab := common.ColorToOKLab(c)
				minDist := math.MaxFloat64
				best := 0
				for i, pOKLab := range paletteOKLabs {
					if dist := common.DeltaOKLabWeighted(pixelOKLab, pOKLab); dist < minDist {
						minDist = dist
						best = i
					}
				}
				matched = paletteColors[best]
				matched.A = alpha
				colorCache[c] = matched
				if color.NRGBAModel.Convert(c) != matched {
					changed++
				}
			}
			dst.SetNRGBA(x, y, matched)
		}
	}

	if err := common.SavePNG(outputPath, dst); err != nil {
		return "", err
	}
	return fmt.Sprintf("원본 색상 %d개 중 %d개 교체", len(colorCache), changed), nil
}
