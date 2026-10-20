package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
	"sprite-tools/pkg/processor"
)

// Size limit of an editor document (RGBA 4 bytes * pixel count).
const (
	maxEditorSide   = 8192
	maxEditorPixels = 4096 * 4096
)

// EditorImage is an image loaded into the editor. Pixels is non-premultiplied RGBA bytes encoded as base64.
type EditorImage struct {
	Path    string `json:"path"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Pixels  string `json:"pixels"`
	Indexed bool   `json:"indexed"` // whether the original was a paletted (indexed) PNG
	Colors  int    `json:"colors"`  // original palette size when Indexed
}

// SaveResult is the result of an editor save. Canceled is true only when the save dialog was canceled.
type SaveResult struct {
	Canceled bool   `json:"canceled"`
	Path     string `json:"path"`
	Format   string `json:"format"` // indexed | rgba
	Colors   int    `json:"colors"` // palette size when indexed
	Backup   string `json:"backup"` // path of the original copied before overwriting (empty if none)
	Bytes    int64  `json:"bytes"`
}

// SetEditorDirty tells whether the editor has any unsaved document (used to confirm when closing the window).
// Use SetEditorDirtyCount to report how many.
func (a *App) SetEditorDirty(dirty bool) {
	a.SetEditorDirtyCount(b2i(dirty))
}

// SetEditorDirtyCount reports the number of documents with unsaved changes in the editor. Greater than 0 is the same as SetEditorDirty being true.
func (a *App) SetEditorDirtyCount(n int) {
	if n < 0 {
		n = 0
	}
	a.editorDirty.Store(int32(n))
}

// dirtyMessage is the text shown in the close confirmation dialog.
func dirtyMessage(n int) string {
	if n <= 1 {
		return i18n.T("gui.dialog.unsaved.message_one")
	}
	return i18n.T("gui.dialog.unsaved.message_many", n)
}

// beforeClose asks once more before quitting if the editor has unsaved changes. Returning true prevents quitting.
func (a *App) beforeClose(ctx context.Context) bool {
	n := int(a.editorDirty.Load())
	if n <= 0 {
		return false
	}
	quit, cancel := i18n.T("gui.dialog.unsaved.quit"), i18n.T("gui.dialog.unsaved.cancel")
	choice, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         i18n.T("gui.dialog.unsaved.title"),
		Message:       dirtyMessage(n),
		Buttons:       []string{quit, cancel},
		DefaultButton: cancel,
		CancelButton:  cancel,
	})
	if err != nil {
		return false
	}
	return choice != quit && choice != "Yes"
}

// LoadImageRGBA decodes a PNG in Go and returns exact RGBA pixels (to avoid the browser canvas alpha rounding).
func (a *App) LoadImageRGBA(path string) (EditorImage, error) {
	return loadImageRGBA(path)
}

func loadImageRGBA(path string) (EditorImage, error) {
	path = absPath(path)
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return EditorImage{}, i18n.Errorf("err.gui.open_png_only")
	}
	img, err := common.LoadPNG(path)
	if err != nil {
		return EditorImage{}, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxEditorSide || h > maxEditorSide || w*h > maxEditorPixels {
		return EditorImage{}, i18n.Errorf("err.gui.image_too_large", w, h, maxEditorSide, maxEditorSide, maxEditorPixels/10000)
	}
	out := EditorImage{Path: path, Width: w, Height: h}
	var pix []byte
	if p, ok := img.(*image.Paletted); ok {
		// Palette colors are already non-premultiplied, so convert entry by entry so that low-alpha colors are moved without loss.
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

// SaveImageRGBA atomically saves the edited pixels as a PNG. It overwrites a file of the same name, and with backup it backs up the original first.
// If the original was an indexed PNG and has 256 colors or fewer, it is saved as an indexed PNG again.
func (a *App) SaveImageRGBA(path string, width, height int, pixelsB64 string, backup bool) (SaveResult, error) {
	return saveImageRGBA(path, path, width, height, pixelsB64, backup)
}

// SaveImageAs picks a location with a save dialog and then saves. paletteFrom is the source path whose indexed-PNG palette is inherited (empty if none).
func (a *App) SaveImageAs(paletteFrom, suggest string, width, height int, pixelsB64 string, backup bool) (SaveResult, error) {
	opts := runtime.SaveDialogOptions{
		Title:                i18n.T("gui.dialog.save_as"),
		DefaultFilename:      suggest,
		Filters:              []runtime.FileFilter{{DisplayName: i18n.T("gui.dialog.filter_png"), Pattern: "*.png"}},
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

// saveImageRGBA saves to path. If paletteFrom is an indexed PNG, it keeps that palette order and saves as indices.
func saveImageRGBA(path, paletteFrom string, width, height int, pixelsB64 string, backup bool) (SaveResult, error) {
	path = absPath(path)
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return SaveResult{}, i18n.Errorf("err.gui.save_png_only")
	}
	if width <= 0 || height <= 0 || width > maxEditorSide || height > maxEditorSide || width*height > maxEditorPixels {
		return SaveResult{}, i18n.Errorf("err.gui.bad_size", width, height)
	}
	pix, err := base64.StdEncoding.DecodeString(pixelsB64)
	if err != nil {
		return SaveResult{}, i18n.Errorf("err.gui.pixel_decode", err)
	}
	if len(pix) != width*height*4 {
		return SaveResult{}, i18n.Errorf("err.gui.pixel_len", len(pix), width*height*4)
	}

	// Read the original palette before overwriting.
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

// toPaletted converts pixels to an indexed image keeping the original palette order. Returns nil if there are more than 256 colors.
//   - Fully transparent (alpha 0) pixels all map to the first transparent entry of the original; if it has none, a new transparent entry is put at index 0.
//   - New colors are appended in order of first appearance.
//   - If the total entries exceed 256, unused original entries are dropped.
func toPaletted(orig color.Palette, w, h int, pix []byte) *image.Paletted {
	origN := make([]color.NRGBA, len(orig))
	origIdx := map[uint32]int{} // opaque/translucent color -> original first position
	origT := -1                 // first transparent entry of the original
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
	var fresh []uint32 // new colors not in the original (in order of first appearance)
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

// ProcessResult is the result of ProcessPixels. When Skipped, there was nothing to process so Pixels is empty and Message holds the reason.
type ProcessResult struct {
	Skipped bool   `json:"skipped"`
	Message string `json:"message"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Pixels  string `json:"pixels"`
}

// Batch commands that can be applied to the current image in the editor.
var editorCommands = map[string]bool{"reduce": true, "recolor": true, "downscale": true, "upscale": true, "trim": true, "despeckle": true}

// ProcessPixels applies a batch command (reduce, recolor, downscale, upscale, trim, despeckle) to the pixels being edited (non-premultiplied RGBA, base64) and returns the result.
// It does not touch files; if there is nothing to process (Skip), it reports Skipped without an error.
func (a *App) ProcessPixels(req JobRequest, width, height int, pixelsB64 string) (ProcessResult, error) {
	if !editorCommands[req.Command] {
		return ProcessResult{}, i18n.Errorf("err.gui.editor_command", req.Command)
	}
	if width <= 0 || height <= 0 || width > maxEditorSide || height > maxEditorSide || width*height > maxEditorPixels {
		return ProcessResult{}, i18n.Errorf("err.gui.bad_size", width, height)
	}
	pix, err := base64.StdEncoding.DecodeString(pixelsB64)
	if err != nil {
		return ProcessResult{}, i18n.Errorf("err.gui.pixel_decode", err)
	}
	if len(pix) != width*height*4 {
		return ProcessResult{}, i18n.Errorf("err.gui.pixel_len", len(pix), width*height*4)
	}
	fn, err := a.imageFunc(req)
	if err != nil {
		return ProcessResult{}, err
	}
	src := &image.NRGBA{Pix: pix, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
	dst, msg, err := fn(src)
	if err != nil {
		if common.IsSkip(err) {
			return ProcessResult{Skipped: true, Message: err.Error()}, nil
		}
		return ProcessResult{}, err
	}
	n := common.ToNRGBA(dst)
	w, h := n.Rect.Dx(), n.Rect.Dy()
	if w > maxEditorSide || h > maxEditorSide || w*h > maxEditorPixels {
		return ProcessResult{}, i18n.Errorf("err.gui.result_too_large", w, h)
	}
	return ProcessResult{Message: msg, Width: w, Height: h, Pixels: base64.StdEncoding.EncodeToString(n.Pix)}, nil
}

// paletteStrip: "// paletteStrip makes a palette PNG (one color = 1px, a single row) from a list of "#rrggbb". Transparency is ignored and colors are saved as opaque.
func paletteStrip(colors []string) (image.Image, error) {
	if len(colors) == 0 {
		return nil, i18n.Errorf("err.gui.no_colors")
	}
	if len(colors) > processor.MaxExtractColors {
		return nil, i18n.Errorf("err.gui.too_many_colors", len(colors), processor.MaxExtractColors)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, len(colors), 1))
	for i, s := range colors {
		t := strings.TrimPrefix(strings.TrimSpace(s), "#")
		if len(t) != 6 {
			return nil, i18n.Errorf("err.gui.bad_color", s)
		}
		var r, g, b uint8
		if _, err := fmt.Sscanf(t, "%02x%02x%02x", &r, &g, &b); err != nil {
			return nil, i18n.Errorf("err.gui.bad_color", s)
		}
		copy(dst.Pix[i*4:], []uint8{r, g, b, 255})
	}
	return dst, nil
}

// PaletteExists tells whether a saved palette with the same name already exists.
func (a *App) PaletteExists(name string) (bool, error) {
	clean, err := processor.NormalizePaletteName(name)
	if err != nil {
		return false, err
	}
	for _, dir := range processor.PaletteDirs() {
		if _, err := os.Stat(filepath.Join(dir, clean+".png")); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// SavePaletteColors saves a color list ("#rrggbb") to the shared palette folder by name (same folder and rules as extract -save in batch processing).
// If the name exists, it is rejected unless force is set.
func (a *App) SavePaletteColors(name string, colors []string, force bool) (PaletteInfo, error) {
	img, err := paletteStrip(colors)
	if err != nil {
		return PaletteInfo{}, err
	}
	if _, err := processor.SavePalette(name, img, force); err != nil {
		return PaletteInfo{}, err
	}
	clean, _ := processor.NormalizePaletteName(name)
	a.forgetPalette(clean)
	return a.LoadPalette(clean)
}
