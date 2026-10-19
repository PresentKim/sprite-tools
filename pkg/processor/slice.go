package processor

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"

	"sprite-tools/pkg/common"
)

func isFrameEmpty(img image.Image, rect image.Rectangle) bool {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 0 {
				return false
			}
		}
	}
	return true
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

	cols, rows := bounds.Dx()/frameW, bounds.Dy()/frameH
	var frames []Frame
	empty := 0
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			origin := bounds.Min.Add(image.Pt(col*frameW, row*frameH))
			rect := image.Rectangle{Min: origin, Max: origin.Add(image.Pt(frameW, frameH))}
			if !keepEmpty && isFrameEmpty(src, rect) {
				empty++
				continue
			}
			dst := image.NewNRGBA(image.Rect(0, 0, frameW, frameH))
			draw.Draw(dst, dst.Bounds(), src, rect.Min, draw.Src)
			frames = append(frames, Frame{dst, col, row})
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
