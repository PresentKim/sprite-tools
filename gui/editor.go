package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sprite-tools/pkg/common"
)

// 편집기 문서의 크기 한도입니다 (RGBA 4바이트 * 픽셀 수).
const (
	maxEditorSide   = 8192
	maxEditorPixels = 4096 * 4096
)

// EditorImage: 편집기로 불러온 이미지입니다. Pixels 는 비곱셈(non-premultiplied) RGBA 바이트를 base64 로 인코딩한 값입니다.
type EditorImage struct {
	Path    string `json:"path"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Pixels  string `json:"pixels"`
	Indexed bool   `json:"indexed"` // 원본이 팔레트(인덱스) PNG 였는지
	Colors  int    `json:"colors"`  // Indexed 일 때 원본 팔레트 크기
}

// SaveResult: 편집기 저장 결과입니다. Canceled 는 저장 대화상자를 취소했을 때만 true 입니다.
type SaveResult struct {
	Canceled bool   `json:"canceled"`
	Path     string `json:"path"`
	Format   string `json:"format"` // indexed | rgba
	Colors   int    `json:"colors"` // indexed 일 때 팔레트 크기
	Backup   string `json:"backup"` // 덮어쓰기 전에 복사해 둔 원본 경로 (없으면 빈 값)
	Bytes    int64  `json:"bytes"`
}

// SetEditorDirty: 편집기에 저장하지 않은 변경 사항이 있는지 알려 줍니다 (창을 닫을 때 확인하는 데 씁니다).
func (a *App) SetEditorDirty(dirty bool) {
	a.editorDirty.Store(dirty)
}

// beforeClose: 편집기에 저장하지 않은 변경이 있으면 종료 전에 한 번 더 묻습니다. true 를 반환하면 종료를 막습니다.
func (a *App) beforeClose(ctx context.Context) bool {
	if !a.editorDirty.Load() {
		return false
	}
	choice, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "종료",
		Message:       "편집기에 저장하지 않은 변경 사항이 있습니다. 저장하지 않고 종료할까요?",
		Buttons:       []string{"종료", "취소"},
		DefaultButton: "취소",
		CancelButton:  "취소",
	})
	if err != nil {
		return false
	}
	return choice != "종료" && choice != "Yes"
}

// LoadImageRGBA: PNG 를 Go 에서 디코딩해 정밀한 RGBA 픽셀로 돌려줍니다 (브라우저 캔버스의 알파 반올림을 피하기 위함).
func (a *App) LoadImageRGBA(path string) (EditorImage, error) {
	return loadImageRGBA(path)
}

func loadImageRGBA(path string) (EditorImage, error) {
	path = absPath(path)
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return EditorImage{}, errors.New("PNG 파일만 열 수 있습니다")
	}
	img, err := common.LoadPNG(path)
	if err != nil {
		return EditorImage{}, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxEditorSide || h > maxEditorSide || w*h > maxEditorPixels {
		return EditorImage{}, fmt.Errorf("이미지가 너무 큽니다 (%d×%d, 최대 %d×%d 또는 %d만 픽셀)", w, h, maxEditorSide, maxEditorSide, maxEditorPixels/10000)
	}
	out := EditorImage{Path: path, Width: w, Height: h}
	var pix []byte
	if p, ok := img.(*image.Paletted); ok {
		// 팔레트 색은 이미 비곱셈 값이므로, 알파가 낮은 색도 손실 없이 옮기도록 항목별로 변환합니다.
		out.Indexed, out.Colors = true, len(p.Palette)
		table := make([]color.NRGBA, 256)
		for i, c := range p.Palette {
			table[i] = color.NRGBAModel.Convert(c).(color.NRGBA)
		}
		pix = make([]byte, w*h*4)
		for y := 0; y < h; y++ {
			row := p.Pix[p.PixOffset(b.Min.X, b.Min.Y+y):]
			for x := 0; x < w; x++ {
				c := table[row[x]]
				o := (y*w + x) * 4
				pix[o], pix[o+1], pix[o+2], pix[o+3] = c.R, c.G, c.B, c.A
			}
		}
	} else {
		pix = common.ToNRGBA(img).Pix
	}
	out.Pixels = base64.StdEncoding.EncodeToString(pix)
	return out, nil
}

// SaveImageRGBA: 편집한 픽셀을 PNG 로 원자적으로 저장합니다. 같은 이름의 파일을 덮어쓰고 backup 이면 원본을 먼저 백업합니다.
// 원본이 인덱스 PNG 였고 색이 256개 이하면 인덱스 PNG 로 다시 저장합니다.
func (a *App) SaveImageRGBA(path string, width, height int, pixelsB64 string, backup bool) (SaveResult, error) {
	return saveImageRGBA(path, path, width, height, pixelsB64, backup)
}

// SaveImageAs: 저장 대화상자로 위치를 고른 뒤 저장합니다. paletteFrom 은 인덱스 PNG 의 팔레트를 이어받을 원본 경로입니다(없으면 빈 값).
func (a *App) SaveImageAs(paletteFrom, suggest string, width, height int, pixelsB64 string, backup bool) (SaveResult, error) {
	opts := runtime.SaveDialogOptions{
		Title:                "다른 이름으로 저장",
		DefaultFilename:      suggest,
		Filters:              []runtime.FileFilter{{DisplayName: "PNG 이미지 (*.png)", Pattern: "*.png"}},
		CanCreateDirectories: true,
	}
	if paletteFrom != "" {
		opts.DefaultDirectory = filepath.Dir(paletteFrom)
		if opts.DefaultFilename == "" {
			opts.DefaultFilename = filepath.Base(paletteFrom)
		}
	}
	out, err := runtime.SaveFileDialog(a.ctx, opts)
	if err != nil {
		return SaveResult{}, err
	}
	if out == "" {
		return SaveResult{Canceled: true}, nil
	}
	if filepath.Ext(out) == "" {
		out += ".png"
	}
	return saveImageRGBA(out, paletteFrom, width, height, pixelsB64, backup)
}

// saveImageRGBA: path 에 저장합니다. paletteFrom 이 인덱스 PNG 면 그 팔레트 순서를 유지해 인덱스로 저장합니다.
func saveImageRGBA(path, paletteFrom string, width, height int, pixelsB64 string, backup bool) (SaveResult, error) {
	path = absPath(path)
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return SaveResult{}, errors.New("PNG 파일로만 저장할 수 있습니다 (확장자 .png)")
	}
	if width <= 0 || height <= 0 || width > maxEditorSide || height > maxEditorSide || width*height > maxEditorPixels {
		return SaveResult{}, fmt.Errorf("이미지 크기가 올바르지 않습니다: %d×%d", width, height)
	}
	pix, err := base64.StdEncoding.DecodeString(pixelsB64)
	if err != nil {
		return SaveResult{}, fmt.Errorf("픽셀 데이터를 해석할 수 없습니다: %v", err)
	}
	if len(pix) != width*height*4 {
		return SaveResult{}, fmt.Errorf("픽셀 데이터 크기가 맞지 않습니다 (%d바이트, 필요 %d바이트)", len(pix), width*height*4)
	}

	// 원본 팔레트는 덮어쓰기 전에 읽어 둡니다.
	var origPal color.Palette
	if paletteFrom != "" {
		if src, err := common.LoadPNG(absPath(paletteFrom)); err == nil {
			if p, ok := src.(*image.Paletted); ok {
				origPal = p.Palette
			}
		}
	}

	res := SaveResult{Path: path, Format: "rgba"}
	var enc image.Image
	if origPal != nil {
		if p := toPaletted(origPal, width, height, pix); p != nil {
			enc, res.Format, res.Colors = p, "indexed", len(p.Palette)
		}
	}
	if enc == nil {
		enc = &image.NRGBA{Pix: pix, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, enc); err != nil {
		return SaveResult{}, err
	}

	if backup {
		if _, err := os.Stat(path); err == nil {
			b, err := common.BackupFileNow(path)
			if err != nil {
				return SaveResult{}, err
			}
			res.Backup = b
		}
	}
	if err := common.WriteFileAtomic(path, buf.Bytes()); err != nil {
		return SaveResult{}, err
	}
	res.Bytes = int64(buf.Len())
	return res, nil
}

func packPix(p []byte) uint32 { return common.PackNRGBA(p[0], p[1], p[2], p[3]) }

// toPaletted: 원본 팔레트의 순서를 유지하며 픽셀을 인덱스 이미지로 바꿉니다. 색이 256개를 넘으면 nil 입니다.
//   - 완전 투명(알파 0) 픽셀은 모두 원본의 첫 투명 항목에 매핑하고, 원본에 투명 항목이 없으면 0번에 새 투명 항목을 넣습니다.
//   - 새 색은 처음 나온 순서대로 뒤에 덧붙입니다.
//   - 항목 전체가 256개를 넘으면 쓰이지 않는 원본 항목을 버립니다.
func toPaletted(orig color.Palette, w, h int, pix []byte) *image.Paletted {
	origN := make([]color.NRGBA, len(orig))
	origIdx := map[uint32]int{} // 불투명·반투명 색 -> 원본 첫 위치
	origT := -1                 // 원본의 첫 투명 항목
	for i, c := range orig {
		n := color.NRGBAModel.Convert(c).(color.NRGBA)
		origN[i] = n
		if n.A == 0 {
			if origT < 0 {
				origT = i
			}
			continue
		}
		k := common.PackNRGBA(n.R, n.G, n.B, n.A)
		if _, ok := origIdx[k]; !ok {
			origIdx[k] = i
		}
	}

	used := map[uint32]bool{}
	var fresh []uint32 // 원본에 없는 새 색 (처음 나온 순서)
	hasT := false
	for i := 0; i < len(pix); i += 4 {
		if pix[i+3] == 0 {
			hasT = true
			continue
		}
		k := packPix(pix[i:])
		if used[k] {
			continue
		}
		used[k] = true
		if _, ok := origIdx[k]; !ok {
			fresh = append(fresh, k)
		}
	}
	if len(used)+b2i(hasT) > 256 {
		return nil
	}

	build := func(keepUnused bool) []color.NRGBA {
		var pal []color.NRGBA
		if hasT && origT < 0 {
			pal = append(pal, color.NRGBA{})
		}
		seen := map[uint32]bool{}
		tDone := false
		for i, n := range origN {
			if n.A == 0 {
				if keepUnused || (hasT && i == origT && !tDone) {
					tDone = tDone || i == origT
					pal = append(pal, n)
				}
				continue
			}
			k := common.PackNRGBA(n.R, n.G, n.B, n.A)
			if seen[k] {
				if keepUnused {
					pal = append(pal, n)
				}
				continue
			}
			seen[k] = true
			if keepUnused || used[k] {
				pal = append(pal, n)
			}
		}
		for _, k := range fresh {
			r, g, b, a := common.UnpackNRGBA(k)
			pal = append(pal, color.NRGBA{R: r, G: g, B: b, A: a})
		}
		return pal
	}
	pal := build(true)
	if len(pal) > 256 {
		pal = build(false)
	}
	if len(pal) > 256 {
		return nil
	}

	index := map[uint32]uint8{}
	tIdx := -1
	for i, n := range pal {
		if n.A == 0 {
			if tIdx < 0 {
				tIdx = i
			}
			continue
		}
		k := common.PackNRGBA(n.R, n.G, n.B, n.A)
		if _, ok := index[k]; !ok {
			index[k] = uint8(i)
		}
	}
	cp := make(color.Palette, len(pal))
	for i, n := range pal {
		cp[i] = n
	}
	out := image.NewPaletted(image.Rect(0, 0, w, h), cp)
	for i := 0; i < w*h; i++ {
		p := pix[i*4 : i*4+4]
		if p[3] == 0 {
			if tIdx >= 0 {
				out.Pix[i] = uint8(tIdx)
			}
			continue
		}
		out.Pix[i] = index[packPix(p)]
	}
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
