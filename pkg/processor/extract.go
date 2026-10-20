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

// MaxExtractColors is the maximum number of colors that can be extracted (Lospec palette PNG convention).
const MaxExtractColors = 256

// ExtractPaletteImage builds a Lospec-style palette PNG (one color = 1px, a single row) from the colors used in an image.
// Fully transparent pixels are ignored; the rest are set to alpha 255 and identical colors are merged.
// Colors are ordered by how often they are used, and ties go to the color that appeared first.
// More than 256 colors is an error; no colors returns a Skip error.
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

// NormalizePaletteName lowercases a palette name and validates it against the Lospec naming rule (lowercase letters, digits, hyphen).
func NormalizePaletteName(name string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(name))
	if !paletteNamePattern.MatchString(clean) {
		return "", i18n.Errorf("err.palette.name_invalid", name)
	}
	return clean, nil
}

// SavePalette saves a palette PNG by name in the shared palette folder so it can be used as recolor -p <name>.
// If a palette with the same name already exists (including the palettes folder next to the executable), it is rejected unless force is set.
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

// Extract is the file-level ExtractPaletteImage. If saveName is set, it also saves to the shared palette folder.
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
		// Check for a name collision first, so no output file is left behind on failure.
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
