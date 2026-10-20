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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/processor"
)

// App: 화면(JS)에서 호출하는 메서드를 제공합니다. 실제 처리는 CLI 와 같은 processor 패키지가 담당합니다.
type App struct {
	ctx context.Context

	mu        sync.Mutex
	cancelJob context.CancelFunc

	palMu    sync.Mutex
	palettes map[string]*loadedPalette

	srcMu    sync.Mutex
	srcCache map[string]cachedImage
	srcOrder []string
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

// JobRequest: 화면에서 선택한 명령과 옵션입니다.
type JobRequest struct {
	Command   string  `json:"command"` // reduce | slice | downscale | index | recolor
	Threshold float64 `json:"threshold"`
	MaxColors int     `json:"maxColors"` // reduce: 0 이면 Threshold 사용, 1 이상이면 최대 색상 수 기준
	Scale     int     `json:"scale"`
	FrameSize string  `json:"frameSize"`
	OutDir    string  `json:"outDir"`
	KeepEmpty bool    `json:"keepEmpty"`
	Palette   string  `json:"palette"`
	Suffix    string  `json:"suffix"`
	InPlace   bool    `json:"inPlace"`
	Threads   int     `json:"threads"`
}

// FileEntry: 목록에 표시할 입력 파일 정보입니다.
type FileEntry struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Dir    string `json:"dir"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  int64  `json:"bytes"`
	Error  string `json:"error,omitempty"`
}

// PreviewResult: 미리보기 결과입니다. Image 는 PNG data URL 입니다.
type PreviewResult struct {
	Status  common.Status `json:"status"`
	Message string        `json:"message"`
	Image   string        `json:"image,omitempty"`
	Width   int           `json:"width"`
	Height  int           `json:"height"`
	Colors  []string      `json:"colors,omitempty"`
	Millis  int64         `json:"millis"`
}

// PaletteInfo: 팔레트 이름과 색상 목록입니다.
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
}

// DefaultSuffix: 명령별 기본 출력 접미사를 반환합니다.
func (a *App) DefaultSuffix(command string) string {
	return defaultSuffixes[command]
}

// ---------- 파일 ----------

// ExpandPaths: 드롭하거나 선택한 파일·폴더를 PNG 파일 목록으로 펼칩니다.
func (a *App) ExpandPaths(paths []string, recursive bool) ([]FileEntry, error) {
	o := common.Options{Roots: paths, Pattern: "*.png", Recursive: recursive, NoOutput: true}
	o.ExcludeDirs = processor.PaletteDirs()
	files, err := common.CollectFiles(o)
	if err != nil {
		return nil, err
	}
	entries := make([]FileEntry, 0, len(files))
	for _, f := range files {
		if !strings.EqualFold(filepath.Ext(f), ".png") {
			continue
		}
		entries = append(entries, describeFile(f))
	}
	return entries, nil
}

// Describe: 파일 정보를 다시 읽습니다 (덮어쓰기 후 크기 갱신 등).
func (a *App) Describe(paths []string) []FileEntry {
	out := make([]FileEntry, len(paths))
	for i, p := range paths {
		out[i] = describeFile(p)
	}
	return out
}

func describeFile(path string) FileEntry {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
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
		e.Error = "PNG 파일이 아니거나 손상됨"
		return e
	}
	e.Width, e.Height = cfg.Width, cfg.Height
	return e
}

func (a *App) PickFiles() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "PNG 파일 선택",
		Filters: []runtime.FileFilter{{DisplayName: "PNG 이미지 (*.png)", Pattern: "*.png"}},
	})
}

func (a *App) PickFolder(title string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

func (a *App) PickPaletteFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "팔레트 PNG 선택",
		Filters: []runtime.FileFilter{{DisplayName: "PNG 이미지 (*.png)", Pattern: "*.png"}},
	})
}

// Reveal: 탐색기에서 파일(또는 폴더)을 선택한 상태로 엽니다.
func (a *App) Reveal(path string) error {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return exec.Command("explorer", path).Start()
	}
	return exec.Command("explorer", "/select,", path).Start()
}

// imageMiddleware: /local?path=... 로 로컬 PNG 를 화면에 제공합니다 (썸네일·원본 미리보기용).
// 개발 모드의 프런트엔드 서버가 모든 경로에 응답하므로 핸들러가 아닌 미들웨어로 먼저 가로챕니다.
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

// ---------- 작업 ----------

func parseFrameSize(s string) (int, int, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), "x")
	if len(parts) == 0 || len(parts) > 2 {
		return 0, 0, fmt.Errorf("프레임 크기 형식이 잘못되었습니다 (예: 16 또는 16x24)")
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("프레임 크기 형식이 잘못되었습니다 (예: 16 또는 16x24)")
	}
	h := w
	if len(parts) == 2 {
		if h, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
			return 0, 0, fmt.Errorf("프레임 크기 형식이 잘못되었습니다 (예: 16 또는 16x24)")
		}
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("프레임 크기는 0보다 커야 합니다")
	}
	return w, h, nil
}

func (a *App) palette(spec string) (*loadedPalette, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("팔레트를 선택하세요")
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

// imageFunc: 요청을 "이미지 → 이미지" 변환 함수로 바꿉니다 (slice 제외).
func (a *App) imageFunc(req JobRequest) (func(image.Image) (image.Image, string, error), error) {
	switch req.Command {
	case "reduce":
		if req.MaxColors < 0 {
			return nil, errors.New("최대 색상 수는 1 이상이어야 합니다")
		}
		if req.MaxColors > 0 {
			return func(src image.Image) (image.Image, string, error) {
				return processor.ReduceToCount(src, req.MaxColors)
			}, nil
		}
		if req.Threshold <= 0 {
			return nil, errors.New("병합 강도는 0보다 커야 합니다")
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.ReduceImage(src, req.Threshold)
		}, nil
	case "downscale":
		if req.Scale < 0 || req.Scale == 1 {
			return nil, errors.New("배율은 2 이상이거나 자동이어야 합니다")
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.DownscaleImage(src, req.Scale)
		}, nil
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
	return nil, fmt.Errorf("알 수 없는 명령: %s", req.Command)
}

func (a *App) buildTask(req JobRequest) (common.Task, common.Options, error) {
	o := common.Options{Threads: req.Threads}
	if req.Command == "slice" {
		w, h, err := parseFrameSize(req.FrameSize)
		if err != nil {
			return nil, o, err
		}
		o.NoOutput = true
		return func(in, _ string) (string, error) {
			return processor.SliceFile(in, w, h, req.OutDir, req.KeepEmpty)
		}, o, nil
	}

	fn, err := a.imageFunc(req)
	if err != nil {
		return nil, o, err
	}
	o.Suffix, o.InPlace = req.Suffix, req.InPlace
	if err := o.Validate(); err != nil {
		return nil, o, err
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

// Run: 파일들을 백그라운드에서 처리합니다. 진행 상황은 "job:result", 완료는 "job:done" 이벤트로 전달됩니다.
func (a *App) Run(req JobRequest, paths []string) error {
	if len(paths) == 0 {
		return errors.New("처리할 파일이 없습니다")
	}
	task, o, err := a.buildTask(req)
	if err != nil {
		return err
	}

	a.mu.Lock()
	if a.cancelJob != nil {
		a.mu.Unlock()
		return errors.New("이미 작업이 진행 중입니다")
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
		sum := common.RunFiles(ctx, paths, o, task, func(r common.Result) {
			runtime.EventsEmit(a.ctx, "job:result", r)
		})
		runtime.EventsEmit(a.ctx, "job:done", map[string]any{
			"done": sum.Done, "skipped": sum.Skipped, "failed": sum.Failed,
			"canceled": sum.Canceled, "seconds": sum.Elapsed.Seconds(),
		})
	}()
	return nil
}

// Cancel: 진행 중인 작업을 멈춥니다. 이미 처리 중인 파일은 끝까지 처리됩니다.
func (a *App) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelJob != nil {
		a.cancelJob()
	}
}

// ---------- 미리보기 ----------

// loadSource: 슬라이더를 움직일 때마다 디스크를 다시 읽지 않도록 최근 이미지를 캐시합니다.
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

// Preview: 파일을 저장하지 않고 처리 결과만 계산해 보여줍니다.
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

// contactSheet: 잘라낸 프레임을 원래 위치에 간격을 두고 배치해 자르기 결과를 한눈에 보여줍니다.
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

// uniqueColors: 이미지에 쓰인 불투명 색상을 사용 빈도순 hex 로 반환합니다. limit 를 넘으면 nil.
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

// ---------- 팔레트 ----------

// ListPalettes: 로컬에 저장된 팔레트 목록을 색상과 함께 반환합니다.
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

// LoadPalette: 팔레트 이름(Lospec, 필요하면 다운로드) 또는 PNG 경로로 팔레트를 불러옵니다.
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

// PalettesDir: 팔레트 캐시 폴더 경로입니다.
func (a *App) PalettesDir() string {
	dir, _ := processor.EnsurePalettesDirExists()
	return dir
}
