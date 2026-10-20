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
	"sprite-tools/pkg/i18n"
)

// PackImages combines images into a single sprite sheet (the opposite of slice).
// The cell size is the largest width x largest height, and smaller images are placed at the top-left of a cell.
// If cols is 0 or less, ceil(sqrt(count)) columns are used; gap is the transparent spacing (px) between cells.
func PackImages(images []image.Image, cols, gap int) (*image.NRGBA, string, error) {
	return PackSheet(images, cols, gap, false)
}

// PackSheet is like PackImages but places smaller images at the center of a cell when center is true.
func PackSheet(images []image.Image, cols, gap int, center bool) (*image.NRGBA, string, error) {
	if len(images) == 0 {
		return nil, "", i18n.Errorf("err.pack.no_images")
	}
	if gap < 0 {
		return nil, "", i18n.Errorf("err.pack.gap", gap)
	}
	if cols < 0 {
		return nil, "", i18n.Errorf("err.pack.cols", cols)
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
		return nil, "", i18n.Errorf("err.pack.zero_size")
	}
	sheetW, sheetH := cols*cellW+(cols-1)*gap, rows*cellH+(rows-1)*gap
	if sheetW*sheetH > maxOutputPixels {
		return nil, "", i18n.Errorf("err.pack.sheet_too_large", sheetW, sheetH)
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
	msg := i18n.T("msg.pack.result", count, cols, rows, sheetW, sheetH)
	return dst, msg, nil
}

// SortedPaths returns a copy of the path list in natural order (frame-2 < frame-10).
func SortedPaths(paths []string) []string {
	sorted := slices.Clone(paths)
	common.NaturalSort(sorted)
	return sorted
}

// LoadImages reads PNG files in order. If any fails, it reports which file.
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

// ErrOutputExists is returned when the output file already exists and overwriting is not allowed. (It is a marker for errors.Is; users see a localized message)
var ErrOutputExists = errors.New("output file already exists")

// CheckPackOutput checks that the sheet output path is safe.
// A path that would overwrite an input file is always rejected, and any other existing file is rejected with ErrOutputExists unless force is set.
func CheckPackOutput(inputs []string, outPath string, force bool) error {
	if !strings.EqualFold(filepath.Ext(outPath), ".png") {
		return i18n.Errorf("err.pack.not_png", outPath)
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	for _, in := range inputs {
		if absIn, err := filepath.Abs(in); err == nil && strings.EqualFold(absIn, absOut) {
			return i18n.Errorf("err.pack.same_as_input", outPath)
		}
	}
	if st, err := os.Stat(outPath); err == nil {
		if st.IsDir() {
			return i18n.Errorf("err.pack.is_dir", outPath)
		}
		if !force {
			return i18n.Wrap(ErrOutputExists, "err.pack.output_exists", outPath)
		}
	}
	return nil
}

// PackFiles merges PNG files into one sheet in natural sort order and saves it to outPath.
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
