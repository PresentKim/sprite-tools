package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
	"sprite-tools/pkg/processor"
)

// App provides the methods called from the UI (JS). The actual processing is done by the same processor package as the CLI.
type App struct {
	ctx context.Context

	mu        sync.Mutex
	cancelJob context.CancelFunc

	palMu    sync.Mutex
	palettes map[string]*loadedPalette

	srcMu    sync.Mutex
	srcCache map[string]cachedImage
	srcOrder []string

	editorDirty atomic.Int32 // number of unsaved documents in the editor
}

type loadedPalette struct {
	path   string
	colors []color.NRGBA
	labs   []common.OKLab
}

type cachedImage struct {
	mod time.Time
	img image.Image
}

func NewApp() *App {
	return &App{
		palettes: make(map[string]*loadedPalette),
		srcCache: make(map[string]cachedImage),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// JobRequest is the command and options chosen in the UI.
type JobRequest struct {
	Command   string  `json:"command"` // reduce | recolor | index | extract | downscale | upscale | trim | despeckle | slice | pack
	Threshold float64 `json:"threshold"`
	MaxColors int     `json:"maxColors"` // reduce: 0 uses Threshold, 1 or more is based on the maximum number of colors
	Scale     int     `json:"scale"`
	FrameSize string  `json:"frameSize"`
	OutDir    string  `json:"outDir"` // slice: frame folder; others: folder to save results in (next to the original if empty)
	KeepEmpty bool    `json:"keepEmpty"`
	Palette   string  `json:"palette"`
	Suffix    string  `json:"suffix"`
	InPlace   bool    `json:"inPlace"`
	Backup    bool    `json:"backup"` // with InPlace, copy the original to the backup folder before overwriting
	Threads   int     `json:"threads"`
	Pad       int     `json:"pad"`      // trim: transparent margin to add on every side
	SaveName  string  `json:"saveName"` // extract: if non-empty, also save under this name in the shared palette folder
	Force     bool    `json:"force"`    // extract: overwrite a palette with the same name
	Cols      int     `json:"cols"`     // pack: number of columns (0 = auto)
	Gap       int     `json:"gap"`      // pack: spacing between cells
	Align     string  `json:"align"`    // pack: topleft | center
	MinSize   int     `json:"minSize"`  // despeckle: erase pieces smaller than this
	Diagonal  bool    `json:"diagonal"` // despeckle: treat diagonal neighbors as connected
}

// InputFile is a file to process and the root folder it was found in (empty if the file was picked directly).
type InputFile struct {
	Path string `json:"path"`
	Root string `json:"root"`
}

// PackResult is the result of packing a sheet. If the save dialog is canceled, only Canceled is true.
type PackResult struct {
	Canceled bool   `json:"canceled"`
	Output   string `json:"output"`
	Message  string `json:"message"`
}

// FileEntry is the input file information shown in the list.
type FileEntry struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Dir    string `json:"dir"`
	Root   string `json:"root"` // the folder it was found in when a folder was expanded (used to keep the subtree structure in the output folder)
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  int64  `json:"bytes"`
	Error  string `json:"error,omitempty"`
}

// PreviewResult is a preview result. Image is a PNG data URL.
type PreviewResult struct {
	Status  common.Status `json:"status"`
	Message string        `json:"message"`
	Image   string        `json:"image,omitempty"`
	Width   int           `json:"width"`
	Height  int           `json:"height"`
	Colors  []string      `json:"colors,omitempty"`
	Millis  int64         `json:"millis"`
}

// PaletteInfo is a palette name and its color list.
type PaletteInfo struct {
	Name   string   `json:"name"`
	Path   string   `json:"path"`
	Colors []string `json:"colors"`
}

var defaultSuffixes = map[string]string{
	"reduce":    "-reduced",
	"downscale": "-downscaled",
	"index":     "-indexed",
	"recolor":   "-recolored",
	"upscale":   "-upscaled",
	"trim":      "-trimmed",
	"despeckle": "-clean",
	"extract":   "-palette",
}

// DefaultSuffix returns the default output suffix of each command.
func (a *App) DefaultSuffix(command string) string {
	return defaultSuffixes[command]
}

// ---------- Files ----------

// ExpandPaths expands dropped or selected files and folders into a list of PNG files.
func (a *App) ExpandPaths(paths []string, recursive bool) ([]FileEntry, error) {
	o := common.Options{Roots: paths, Pattern: "*.png", Recursive: recursive, NoOutput: true}
	o.ExcludeDirs = processor.PaletteDirs()
	files, err := common.CollectInputs(o)
	if err != nil {
		return nil, err
	}
	entries := make([]FileEntry, 0, len(files))
	for _, f := range files {
		if !strings.EqualFold(filepath.Ext(f.Path), ".png") {
			continue
		}
		e := describeFile(f.Path)
		if f.Root != "" {
			e.Root = absPath(f.Root)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Describe re-reads file information (e.g. refreshing the size after an overwrite).
func (a *App) Describe(paths []string) []FileEntry {
	out := make([]FileEntry, len(paths))
	for i, p := range paths {
		out[i] = describeFile(p)
	}
	return out
}

func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func describeFile(path string) FileEntry {
	path = absPath(path)
	e := FileEntry{Path: path, Name: filepath.Base(path), Dir: filepath.Dir(path)}
	f, err := os.Open(path)
	if err != nil {
		e.Error = err.Error()
		return e
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil {
		e.Bytes = st.Size()
	}
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		e.Error = i18n.T("err.png.invalid_short")
		return e
	}
	e.Width, e.Height = cfg.Width, cfg.Height
	return e
}

func (a *App) PickFiles() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   i18n.T("gui.dialog.open_png"),
		Filters: []runtime.FileFilter{{DisplayName: i18n.T("gui.dialog.filter_png"), Pattern: "*.png"}},
	})
}

func (a *App) PickFolder(title string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

func (a *App) PickPaletteFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   i18n.T("gui.dialog.open_palette"),
		Filters: []runtime.FileFilter{{DisplayName: i18n.T("gui.dialog.filter_png"), Pattern: "*.png"}},
	})
}

// Reveal opens the file (or folder) selected in the file explorer.
func (a *App) Reveal(path string) error {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return exec.Command("explorer", path).Start()
	}
	return exec.Command("explorer", "/select,", path).Start()
}

// imageMiddleware serves a local PNG to the UI via /local?path=... (for thumbnails and original previews).
// The dev-mode frontend server answers every path, so this intercepts it first as middleware rather than as a handler.
func (a *App) imageMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/local" {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Query().Get("path")
		if !strings.EqualFold(filepath.Ext(p), ".png") {
			http.Error(w, "png only", http.StatusForbidden)
			return
		}
		data, err := os.ReadFile(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	})
}

// ---------- Jobs ----------

func parseFrameSize(s string) (int, int, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), "x")
	if len(parts) == 0 || len(parts) > 2 {
		return 0, 0, i18n.Errorf("err.gui.frame_size_format")
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, i18n.Errorf("err.gui.frame_size_format")
	}
	h := w
	if len(parts) == 2 {
		if h, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
			return 0, 0, i18n.Errorf("err.gui.frame_size_format")
		}
	}
	if w <= 0 || h <= 0 {
		return 0, 0, i18n.Errorf("err.gui.frame_size_positive")
	}
	return w, h, nil
}

func (a *App) palette(spec string) (*loadedPalette, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, i18n.Errorf("err.gui.palette_required")
	}
	a.palMu.Lock()
	defer a.palMu.Unlock()
	if p, ok := a.palettes[spec]; ok {
		return p, nil
	}
	path, err := processor.ResolvePalette(spec)
	if err != nil {
		return nil, err
	}
	colors, labs, err := processor.LoadPaletteColors(path)
	if err != nil {
		return nil, err
	}
	p := &loadedPalette{path, colors, labs}
	a.palettes[spec] = p
	return p, nil
}

// imageFunc turns a request into an "image -> image" transform function (except slice).
func (a *App) imageFunc(req JobRequest) (func(image.Image) (image.Image, string, error), error) {
	switch req.Command {
	case "reduce":
		if req.MaxColors < 0 {
			return nil, i18n.Errorf("err.gui.max_colors")
		}
		if req.MaxColors > 0 {
			return func(src image.Image) (image.Image, string, error) {
				return processor.ReduceToCount(src, req.MaxColors)
			}, nil
		}
		if req.Threshold <= 0 {
			return nil, i18n.Errorf("err.gui.threshold")
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.ReduceImage(src, req.Threshold)
		}, nil
	case "downscale":
		if req.Scale < 0 || req.Scale == 1 {
			return nil, i18n.Errorf("err.gui.downscale_scale")
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.DownscaleImage(src, req.Scale)
		}, nil
	case "upscale":
		if req.Scale < processor.MinUpscale || req.Scale > processor.MaxUpscale {
			return nil, i18n.Errorf("err.gui.upscale_range", processor.MinUpscale, processor.MaxUpscale)
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.UpscaleImage(src, req.Scale)
		}, nil
	case "trim":
		if req.Pad < 0 {
			return nil, i18n.Errorf("err.gui.pad")
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.TrimImage(src, req.Pad)
		}, nil
	case "despeckle":
		if req.MinSize < 1 {
			return nil, i18n.Errorf("err.gui.min_size")
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.DespeckleImage(src, req.MinSize, req.Diagonal)
		}, nil
	case "extract":
		return processor.ExtractPaletteImage, nil
	case "index":
		return processor.IndexImage, nil
	case "recolor":
		p, err := a.palette(req.Palette)
		if err != nil {
			return nil, err
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.RecolorImage(src, p.colors, p.labs)
		}, nil
	}
	return nil, i18n.Errorf("err.gui.unknown_command", req.Command)
}

// buildTask turns a request into the task and options to run. produced is a callback that reports saved files
// when the task decides its own output paths (slice); it may be nil (it can be called from several threads).
func (a *App) buildTask(req JobRequest, produced func(paths []string)) (common.Task, common.Options, error) {
	o := common.Options{Threads: req.Threads}
	if req.Command == "slice" {
		w, h, err := parseFrameSize(req.FrameSize)
		if err != nil {
			return nil, o, err
		}
		o.NoOutput = true
		return func(in, _ string) (string, error) {
			msg, paths, err := processor.SliceFilePaths(in, w, h, req.OutDir, req.KeepEmpty)
			if produced != nil && len(paths) > 0 {
				produced(paths)
			}
			return msg, err
		}, o, nil
	}

	fn, err := a.imageFunc(req)
	if err != nil {
		return nil, o, err
	}
	o.Suffix, o.InPlace, o.OutDir = req.Suffix, req.InPlace, strings.TrimSpace(req.OutDir)
	if req.Command == "extract" {
		o.InPlace = false // the original must not be overwritten with a palette image.
	}
	o.Backup = req.Backup && o.InPlace
	if err := o.Validate(); err != nil {
		return nil, o, err
	}
	if req.Command == "extract" {
		saveName := strings.TrimSpace(req.SaveName)
		if saveName != "" {
			if _, err := processor.NormalizePaletteName(saveName); err != nil {
				return nil, o, err
			}
		}
		return func(in, out string) (string, error) {
			msg, err := processor.Extract(in, out, saveName, req.Force)
			if err == nil && saveName != "" {
				a.forgetPalette(saveName)
			}
			return msg, err
		}, o, nil
	}
	return func(in, out string) (string, error) {
		src, err := common.LoadPNG(in)
		if err != nil {
			return "", err
		}
		dst, msg, err := fn(src)
		if err != nil {
			return "", err
		}
		if err := common.SavePNG(out, dst); err != nil {
			return "", err
		}
		return msg, nil
	}, o, nil
}

// Run processes the files in the background. Progress is delivered as "job:result" events and completion as "job:done".
func (a *App) Run(req JobRequest, files []InputFile) error {
	if len(files) == 0 {
		return i18n.Errorf("err.gui.no_files")
	}
	if req.Command == "extract" && strings.TrimSpace(req.SaveName) != "" && len(files) > 1 {
		return i18n.Errorf("err.gui.save_single", len(files))
	}
	var outMu sync.Mutex
	var outputs []string // files newly created by this run (excluding overwritten originals)
	task, o, err := a.buildTask(req, func(paths []string) {
		outMu.Lock()
		outputs = append(outputs, paths...)
		outMu.Unlock()
	})
	if err != nil {
		return err
	}
	inputs := make([]common.Input, len(files))
	for i, f := range files {
		inputs[i] = common.Input{Path: f.Path, Root: f.Root}
	}

	a.mu.Lock()
	if a.cancelJob != nil {
		a.mu.Unlock()
		return i18n.Errorf("err.gui.job_running")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancelJob = cancel
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.cancelJob = nil
			a.mu.Unlock()
			cancel()
		}()
		sum := common.RunInputs(ctx, inputs, o, task, func(r common.Result) {
			if r.Status == common.StatusDone && r.Output != "" && !common.SamePath(r.Input, r.Output) {
				outMu.Lock()
				outputs = append(outputs, r.Output)
				outMu.Unlock()
			}
			runtime.EventsEmit(a.ctx, "job:result", r)
		})
		outMu.Lock()
		produced := append([]string{}, outputs...)
		outMu.Unlock()
		sort.Strings(produced)
		backupDirs := sum.BackupDirs
		if backupDirs == nil {
			backupDirs = []string{}
		}
		runtime.EventsEmit(a.ctx, "job:done", map[string]any{
			"done": sum.Done, "skipped": sum.Skipped, "failed": sum.Failed,
			"canceled": sum.Canceled, "seconds": sum.Elapsed.Seconds(),
			"outputs": produced, "backups": sum.Backups, "backupDirs": backupDirs,
		})
	}()
	return nil
}

// BackupPair is an overwritten original file and the backup file copied before overwriting.
type BackupPair struct {
	Path   string `json:"path"`
	Backup string `json:"backup"`
}

// RestoreReport is the result of restoring backups. Restored is the number of files put back, Errors the reason for each failed file.
type RestoreReport struct {
	Restored int      `json:"restored"`
	Errors   []string `json:"errors"`
}

// RestoreBackups puts backed-up originals back in their original locations. It is refused while a job is running.
func (a *App) RestoreBackups(pairs []BackupPair) (RestoreReport, error) {
	a.mu.Lock()
	busy := a.cancelJob != nil
	a.mu.Unlock()
	if busy {
		return RestoreReport{}, i18n.Errorf("err.gui.job_running_restore")
	}
	rep := RestoreReport{Errors: []string{}}
	for _, p := range pairs {
		if !strings.EqualFold(filepath.Ext(p.Path), ".png") {
			rep.Errors = append(rep.Errors, i18n.T("err.gui.not_png", p.Path))
			continue
		}
		if err := common.RestoreBackup(p.Backup, p.Path); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", filepath.Base(p.Path), err))
			continue
		}
		rep.Restored++
	}
	return rep, nil
}

// Cancel stops the running job. A file already being processed is finished.
func (a *App) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelJob != nil {
		a.cancelJob()
	}
}

// ---------- Preview ----------

// loadSource caches the most recent image so the disk is not re-read every time a slider moves.
func (a *App) loadSource(path string) (image.Image, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	a.srcMu.Lock()
	if c, ok := a.srcCache[path]; ok && c.mod.Equal(st.ModTime()) {
		a.srcMu.Unlock()
		return c.img, nil
	}
	a.srcMu.Unlock()

	img, err := common.LoadPNG(path)
	if err != nil {
		return nil, err
	}

	a.srcMu.Lock()
	defer a.srcMu.Unlock()
	if _, ok := a.srcCache[path]; !ok {
		a.srcOrder = append(a.srcOrder, path)
	}
	a.srcCache[path] = cachedImage{st.ModTime(), img}
	for len(a.srcOrder) > 8 {
		delete(a.srcCache, a.srcOrder[0])
		a.srcOrder = a.srcOrder[1:]
	}
	return img, nil
}

// Preview computes and shows only the processed result, without saving files.
func (a *App) Preview(req JobRequest, path string) PreviewResult {
	start := time.Now()
	res := a.preview(req, path)
	res.Millis = time.Since(start).Milliseconds()
	return res
}

func (a *App) preview(req JobRequest, path string) PreviewResult {
	fail := func(err error) PreviewResult {
		if common.IsSkip(err) {
			return PreviewResult{Status: common.StatusSkipped, Message: err.Error()}
		}
		return PreviewResult{Status: common.StatusFailed, Message: err.Error()}
	}

	src, err := a.loadSource(path)
	if err != nil {
		return fail(err)
	}

	var dst image.Image
	var msg string
	if req.Command == "slice" {
		w, h, err := parseFrameSize(req.FrameSize)
		if err != nil {
			return fail(err)
		}
		frames, m, err := processor.SliceImage(src, w, h, req.KeepEmpty)
		if err != nil {
			return fail(err)
		}
		dst, msg = contactSheet(frames, src.Bounds(), w, h), m
	} else {
		fn, err := a.imageFunc(req)
		if err != nil {
			return fail(err)
		}
		if dst, msg, err = fn(src); err != nil {
			return fail(err)
		}
		if req.Command == "extract" {
			dst = swatchStrip(dst)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return fail(err)
	}
	b := dst.Bounds()
	res := PreviewResult{
		Status:  common.StatusDone,
		Message: msg,
		Image:   "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		Width:   b.Dx(),
		Height:  b.Dy(),
	}
	if req.Command != "slice" {
		res.Colors = uniqueColors(dst, 256)
	}
	return res
}

// swatchStrip enlarges a 1px palette row into a square cell per color so it can be seen at a glance (at most 16 colors per row).
func swatchStrip(pal image.Image) image.Image {
	n := common.ToNRGBA(pal)
	count := n.Rect.Dx()
	const cell, perRow = 24, 16
	cols := min(count, perRow)
	rows := (count + perRow - 1) / perRow
	dst := image.NewNRGBA(image.Rect(0, 0, cols*cell, rows*cell))
	for i := 0; i < count; i++ {
		c := color.NRGBA{n.Pix[i*4], n.Pix[i*4+1], n.Pix[i*4+2], n.Pix[i*4+3]}
		x0, y0 := (i%perRow)*cell, (i/perRow)*cell
		for y := 0; y < cell; y++ {
			for x := 0; x < cell; x++ {
				dst.SetNRGBA(x0+x, y0+y, c)
			}
		}
	}
	return dst
}

// contactSheet places the cut-out frames at their original positions with gaps, to show the slice result at a glance.
func contactSheet(frames []processor.Frame, src image.Rectangle, fw, fh int) image.Image {
	gap := max(1, min(fw, fh)/8)
	cols, rows := src.Dx()/fw, src.Dy()/fh
	sheet := image.NewNRGBA(image.Rect(0, 0, cols*fw+(cols-1)*gap, rows*fh+(rows-1)*gap))
	for _, f := range frames {
		ox, oy := f.Col*(fw+gap), f.Row*(fh+gap)
		for y := 0; y < fh; y++ {
			copy(sheet.Pix[sheet.PixOffset(ox, oy+y):sheet.PixOffset(ox+fw, oy+y)], f.Image.Pix[f.Image.PixOffset(0, y):f.Image.PixOffset(fw, y)])
		}
	}
	return sheet
}

// uniqueColors returns the opaque colors used in the image as hex, ordered by frequency. Returns nil if there are more than limit.
func uniqueColors(img image.Image, limit int) []string {
	counts := make(map[color.NRGBA]int)
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A == 0 {
				continue
			}
			c.A = 255
			counts[c]++
			if len(counts) > limit {
				return nil
			}
		}
	}
	type kv struct {
		c color.NRGBA
		n int
	}
	list := make([]kv, 0, len(counts))
	for c, n := range counts {
		list = append(list, kv{c, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return hex(list[i].c) < hex(list[j].c)
	})
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = hex(e.c)
	}
	return out
}

func hex(c color.NRGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// ---------- Packing sheets ----------

func packSheet(req JobRequest, paths []string) (*image.NRGBA, string, error) {
	if len(paths) == 0 {
		return nil, "", i18n.Errorf("err.gui.pack_none")
	}
	images, err := processor.LoadImages(processor.SortedPaths(paths))
	if err != nil {
		return nil, "", err
	}
	return processor.PackSheet(images, req.Cols, req.Gap, req.Align == "center")
}

// PreviewPack previews, without saving, the sheet made by merging the listed files in natural sort order.
func (a *App) PreviewPack(req JobRequest, paths []string) PreviewResult {
	start := time.Now()
	sheet, msg, err := packSheet(req, paths)
	if err != nil {
		return PreviewResult{Status: common.StatusFailed, Message: err.Error(), Millis: time.Since(start).Milliseconds()}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, sheet); err != nil {
		return PreviewResult{Status: common.StatusFailed, Message: err.Error()}
	}
	return PreviewResult{
		Status:  common.StatusDone,
		Message: msg,
		Image:   "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		Width:   sheet.Rect.Dx(),
		Height:  sheet.Rect.Dy(),
		Millis:  time.Since(start).Milliseconds(),
	}
}

// RunPack asks for a save location, then merges the files into one sheet and saves it.
// The save dialog confirms overwriting, so an existing file is overwritten, but a path that would overwrite an input file is rejected.
func (a *App) RunPack(req JobRequest, paths []string) (PackResult, error) {
	if len(paths) == 0 {
		return PackResult{}, i18n.Errorf("err.gui.pack_none")
	}
	sorted := processor.SortedPaths(paths)
	if _, _, err := packSheet(req, sorted); err != nil {
		return PackResult{}, err
	}
	out, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                i18n.T("gui.dialog.save_sheet"),
		DefaultDirectory:     filepath.Dir(sorted[0]),
		DefaultFilename:      "sheet.png",
		Filters:              []runtime.FileFilter{{DisplayName: i18n.T("gui.dialog.filter_png"), Pattern: "*.png"}},
		CanCreateDirectories: true,
	})
	if err != nil {
		return PackResult{}, err
	}
	if out == "" {
		return PackResult{Canceled: true}, nil
	}
	if filepath.Ext(out) == "" {
		out += ".png"
	}
	msg, err := processor.PackFiles(sorted, out, req.Cols, req.Gap, req.Align == "center", true)
	if err != nil {
		return PackResult{}, err
	}
	return PackResult{Output: out, Message: msg}, nil
}

// ---------- Palettes ----------

// forgetPalette clears the cached colors when a palette is saved again under the same name.
func (a *App) forgetPalette(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	a.palMu.Lock()
	defer a.palMu.Unlock()
	delete(a.palettes, name)
}

// ListPalettes returns the locally saved palettes along with their colors.
func (a *App) ListPalettes() ([]PaletteInfo, error) {
	names, err := processor.ListLocalPalettes()
	if err != nil {
		return nil, err
	}
	var list []PaletteInfo
	for _, n := range names {
		if info, err := a.LoadPalette(n); err == nil {
			list = append(list, info)
		}
	}
	return list, nil
}

// LoadPalette loads a palette by palette name (Lospec, downloaded if needed) or by PNG path.
func (a *App) LoadPalette(spec string) (PaletteInfo, error) {
	p, err := a.palette(spec)
	if err != nil {
		return PaletteInfo{}, err
	}
	info := PaletteInfo{Name: strings.TrimSpace(spec), Path: p.path, Colors: make([]string, len(p.colors))}
	for i, c := range p.colors {
		info.Colors[i] = hex(c)
	}
	return info, nil
}

// PalettesDir is the path of the palette cache folder.
func (a *App) PalettesDir() string {
	dir, _ := processor.EnsurePalettesDirExists()
	return dir
}
