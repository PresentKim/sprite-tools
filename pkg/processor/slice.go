package processor

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"sprite-tools/pkg/common"
)

// isFrameEmpty: rect 영역(img 좌표계)의 알파가 모두 0 인지 확인합니다.
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

// copyFrame: rect 영역을 (0,0) 에서 시작하는 새 NRGBA 로 복사합니다.
func copyFrame(img *image.NRGBA, rect image.Rectangle) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		off := img.PixOffset(rect.Min.X, rect.Min.Y+y)
		copy(dst.Pix[y*dst.Stride:(y+1)*dst.Stride], img.Pix[off:off+rect.Dx()*4])
	}
	return dst
}

// Frame: 잘라낸 프레임 하나와 시트에서의 위치입니다.
type Frame struct {
	Image    *image.NRGBA
	Col, Row int
}

// SliceImage: 스프라이트 시트를 frameW x frameH 크기로 자릅니다. keepEmpty 가 false 면 완전히 투명한 프레임은 제외합니다.
func SliceImage(src image.Image, frameW, frameH int, keepEmpty bool) ([]Frame, string, error) {
	bounds := src.Bounds()
	if bounds.Dx() == frameW && bounds.Dy() == frameH {
		return nil, "", common.Skip("이미 프레임 크기 %dx%d", frameW, frameH)
	}
	if bounds.Dx() < frameW || bounds.Dy() < frameH {
		return nil, "", common.Skip("이미지 %dx%d 가 프레임 %dx%d 보다 작음", bounds.Dx(), bounds.Dy(), frameW, frameH)
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

	msg := fmt.Sprintf("%d프레임, %dx%d 격자", len(frames), cols, rows)
	if empty > 0 {
		msg += fmt.Sprintf(", 빈 프레임 %d개 제외", empty)
	}
	if bounds.Dx()%frameW != 0 || bounds.Dy()%frameH != 0 {
		msg += fmt.Sprintf(", 경고: 가장자리 %dx%dpx 잘림", bounds.Dx()%frameW, bounds.Dy()%frameH)
	}
	return frames, msg, nil
}

// SliceFile: 스프라이트 시트를 잘라 "<이름>-0001.png" 형태로 저장합니다.
// outDir 가 비어 있으면 원본과 같은 폴더에 저장합니다.
func SliceFile(path string, frameW, frameH int, outDir string, keepEmpty bool) (string, error) {
	src, err := common.LoadPNG(path)
	if err != nil {
		return "", err
	}
	frames, msg, err := SliceImage(src, frameW, frameH, keepEmpty)
	if err != nil {
		return "", err
	}

	baseName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if outDir == "" {
		outDir = filepath.Dir(path)
	} else if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}
	for i, f := range frames {
		outPath := filepath.Join(outDir, fmt.Sprintf("%s-%04d.png", baseName, i+1))
		if err := common.SavePNG(outPath, f.Image); err != nil {
			return "", err
		}
	}
	return msg, nil
}
