package processor

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

// isFrameEmpty reports whether the alpha of the rect area (in img coordinates) is all 0.
func isFrameEmpty(img *image.NRGBA, rect image.Rectangle) bool {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		row := img.Pix[img.PixOffset(rect.Min.X, y):img.PixOffset(rect.Max.X, y)]
		for i := 3; i < len(row); i += 4 {
			if row[i] != 0 {
				return false
			}
		}
	}
	return true
}

// copyFrame copies the rect area into a new NRGBA that starts at (0,0).
func copyFrame(img *image.NRGBA, rect image.Rectangle) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		off := img.PixOffset(rect.Min.X, rect.Min.Y+y)
		copy(dst.Pix[y*dst.Stride:(y+1)*dst.Stride], img.Pix[off:off+rect.Dx()*4])
	}
	return dst
}

// Frame is one cut-out frame and its position in the sheet.
type Frame struct {
	Image    *image.NRGBA
	Col, Row int
}

// SliceImage cuts a sprite sheet into frameW x frameH frames. If keepEmpty is false, fully transparent frames are excluded.
func SliceImage(src image.Image, frameW, frameH int, keepEmpty bool) ([]Frame, string, error) {
	bounds := src.Bounds()
	if bounds.Dx() == frameW && bounds.Dy() == frameH {
		return nil, "", common.Skip("skip.slice.same_size", frameW, frameH)
	}
	if bounds.Dx() < frameW || bounds.Dy() < frameH {
		return nil, "", common.Skip("skip.slice.smaller", bounds.Dx(), bounds.Dy(), frameW, frameH)
	}

	n := common.ToNRGBA(src)
	cols, rows := bounds.Dx()/frameW, bounds.Dy()/frameH
	var frames []Frame
	empty := 0
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			rect := image.Rect(col*frameW, row*frameH, (col+1)*frameW, (row+1)*frameH)
			if !keepEmpty && isFrameEmpty(n, rect) {
				empty++
				continue
			}
			frames = append(frames, Frame{copyFrame(n, rect), col, row})
		}
	}

	msg := i18n.T("msg.slice.result", len(frames), cols, rows)
	if empty > 0 {
		msg += i18n.T("msg.slice.empty", empty)
	}
	if bounds.Dx()%frameW != 0 || bounds.Dy()%frameH != 0 {
		msg += i18n.T("msg.slice.clipped", bounds.Dx()%frameW, bounds.Dy()%frameH)
	}
	return frames, msg, nil
}

// SliceFile cuts a sprite sheet and saves the frames as "<name>-0001.png".
// If outDir is empty, they are saved in the same folder as the original.
func SliceFile(path string, frameW, frameH int, outDir string, keepEmpty bool) (string, error) {
	msg, _, err := SliceFilePaths(path, frameW, frameH, outDir, keepEmpty)
	return msg, err
}

// SliceFilePaths is like SliceFile but also returns the list of saved frame file paths
// (on a mid-way failure it returns the paths saved so far).
func SliceFilePaths(path string, frameW, frameH int, outDir string, keepEmpty bool) (string, []string, error) {
	src, err := common.LoadPNG(path)
	if err != nil {
		return "", nil, err
	}
	frames, msg, err := SliceImage(src, frameW, frameH, keepEmpty)
	if err != nil {
		return "", nil, err
	}

	baseName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if outDir == "" {
		outDir = filepath.Dir(path)
	} else if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", nil, err
	}
	written := make([]string, 0, len(frames))
	for i, f := range frames {
		outPath := filepath.Join(outDir, fmt.Sprintf("%s-%04d.png", baseName, i+1))
		if err := common.SavePNG(outPath, f.Image); err != nil {
			return "", written, err
		}
		written = append(written, outPath)
	}
	return msg, written, nil
}
