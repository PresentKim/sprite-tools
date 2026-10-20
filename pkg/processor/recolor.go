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
	"slices"
	"strings"
	"time"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

var paletteNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// GetPalettesDir returns the shared folder where palettes are downloaded and stored (%APPDATA%\sprite-tools\palettes, etc.).
// The CLI and the GUI share the same folder.
func GetPalettesDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", i18n.Errorf("err.palette.config_dir", err)
	}
	return filepath.Join(base, "sprite-tools", "palettes"), nil
}

// PaletteDirs lists the folders searched for palettes. A palettes folder next to the executable (portable setup), if present, is searched first.
func PaletteDirs() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		local := filepath.Join(filepath.Dir(exe), "palettes")
		if st, err := os.Stat(local); err == nil && st.IsDir() {
			dirs = append(dirs, local)
		}
	}
	if dir, err := GetPalettesDir(); err == nil {
		dirs = append(dirs, dir)
	}
	return dirs
}

// EnsurePalettesDirExists creates the shared palette folder if it does not exist.
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

// ResolvePalette turns a palette specifier into a PNG file path.
// An existing .png file path is used as is; otherwise it is treated as a Lospec palette name and taken from the cache or downloaded.
func ResolvePalette(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if strings.EqualFold(filepath.Ext(spec), ".png") {
		if _, err := os.Stat(spec); err != nil {
			return "", i18n.Errorf("err.palette.file_not_found", spec)
		}
		return spec, nil
	}
	return DownloadPalette(spec)
}

// DownloadPalette downloads the palette's 1x PNG from Lospec and saves it to the shared palette folder, unless it is already cached.
func DownloadPalette(name string) (string, error) {
	cleanName := strings.ToLower(strings.TrimSpace(name))
	if !paletteNamePattern.MatchString(cleanName) {
		return "", i18n.Errorf("err.palette.name_invalid_lospec", name)
	}

	palettesDir, err := EnsurePalettesDirExists()
	if err != nil {
		return "", err
	}
	savePath := filepath.Join(palettesDir, cleanName+".png")

	if _, err := common.LoadPNG(savePath); err == nil {
		fmt.Println(i18n.T("msg.palette.cache", savePath))
		return savePath, nil
	}

	url := fmt.Sprintf("https://lospec.com/palette-list/%s-1x.png", cleanName)
	fmt.Println(i18n.T("msg.palette.downloading", url))
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sprite-tools")
	resp, err := client.Do(req)
	if err != nil {
		return "", i18n.Errorf("err.palette.download_failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", i18n.Errorf("err.palette.not_on_lospec", cleanName, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", i18n.Errorf("err.palette.read_failed", err)
	}
	if _, err := png.Decode(bytes.NewReader(bodyBytes)); err != nil {
		return "", i18n.Errorf("err.palette.not_png", cleanName)
	}

	if err := common.WriteFileAtomic(savePath, bodyBytes); err != nil {
		return "", i18n.Errorf("err.palette.save_failed", err)
	}
	fmt.Println(i18n.T("msg.palette.saved", savePath))
	return savePath, nil
}

// ListLocalPalettes returns the names of the saved palettes (deduplicated, sorted by name).
func ListLocalPalettes() ([]string, error) {
	seen := make(map[string]bool)
	var palettes []string
	for _, dir := range PaletteDirs() {
		files, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() || !strings.EqualFold(filepath.Ext(file.Name()), ".png") {
				continue
			}
			name := strings.ToLower(strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())))
			if !seen[name] {
				seen[name] = true
				palettes = append(palettes, name)
			}
		}
	}
	slices.Sort(palettes)
	return palettes, nil
}

// LoadPaletteColors extracts the list of unique colors and the OKLab data list from a palette PNG file.
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
		return nil, nil, i18n.Errorf("err.palette.no_colors", palettePath)
	}
	return rawColors, oklabColors, nil
}

// Recolor is the file-level RecolorImage.
func Recolor(inputPath, outputPath string, paletteColors []color.NRGBA, paletteOKLabs []common.OKLab) (string, error) {
	return convertFile(inputPath, outputPath, func(src image.Image) (image.Image, string, error) {
		return RecolorImage(src, paletteColors, paletteOKLabs)
	})
}

// RecolorImage converts the image's colors to the nearest palette color by OKLab distance.
// The alpha of the original pixels is kept as is.
func RecolorImage(src image.Image, paletteColors []color.NRGBA, paletteOKLabs []common.OKLab) (image.Image, string, error) {
	bounds := src.Bounds()
	n := common.ToNRGBA(src)
	dst := image.NewNRGBA(bounds)
	colorCache := make(map[uint32]color.NRGBA)
	changed := 0

	lastKey, hasLast := uint32(0), false
	var lastMatched color.NRGBA
	for i := 0; i < len(n.Pix); i += 4 {
		p := n.Pix[i : i+4 : i+4]
		alpha := p[3]
		if alpha == 0 {
			continue
		}
		key := common.PackNRGBA(p[0], p[1], p[2], alpha)
		if !hasLast || key != lastKey {
			matched, ok := colorCache[key]
			if !ok {
				pixelOKLab := common.PackedToOKLab(key)
				minDist := math.MaxFloat64
				best := 0
				for j, pOKLab := range paletteOKLabs {
					if dist := common.DeltaOKLabWeighted(pixelOKLab, pOKLab); dist < minDist {
						minDist = dist
						best = j
					}
				}
				matched = paletteColors[best]
				matched.A = alpha
				colorCache[key] = matched
				if (color.NRGBA{p[0], p[1], p[2], alpha}) != matched {
					changed++
				}
			}
			lastKey, lastMatched, hasLast = key, matched, true
		}
		dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = lastMatched.R, lastMatched.G, lastMatched.B, lastMatched.A
	}

	return dst, i18n.T("msg.recolor.result", len(colorCache), changed), nil
}
