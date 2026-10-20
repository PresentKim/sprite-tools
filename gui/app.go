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
	Command   string  `json:"command"` // reduce | recolor | index | extract | downscale | upscale | trim | slice | pack
	Threshold float64 `json:"threshold"`
	MaxColors int     `json:"maxColors"` // reduce: 0 이면 Threshold 사용, 1 이상이면 최대 색상 수 기준
	Scale     int     `json:"scale"`
	FrameSize string  `json:"frameSize"`
	OutDir    string  `json:"outDir"` // slice: 프레임 폴더, 그 외: 결과를 저장할 폴더 (비어 있으면 원본 옆)
	KeepEmpty bool    `json:"keepEmpty"`
	Palette   string  `json:"palette"`
	Suffix    string  `json:"suffix"`
	InPlace   bool    `json:"inPlace"`
	Backup    bool    `json:"backup"` // InPlace 일 때 덮어쓰기 전에 원본을 백업 폴더에 복사
	Threads   int     `json:"threads"`
	Pad       int     `json:"pad"`      // trim: 사방에 더할 투명 여백
	SaveName  string  `json:"saveName"` // extract: 비어 있지 않으면 공용 팔레트 폴더에도 이 이름으로 저장
	Force     bool    `json:"force"`    // extract: 같은 이름의 팔레트를 덮어쓰기
	Cols      int     `json:"cols"`     // pack: 열 수 (0 = 자동)
	Gap       int     `json:"gap"`      // pack: 칸 사이 간격
	Align     string  `json:"align"`    // pack: topleft | center
}

// InputFile: 처리할 파일과, 그 파일을 찾은 루트 폴더(직접 고른 파일이면 빈 값)입니다.
type InputFile struct {
	Path string `json:"path"`
	Root string `json:"root"`
}

// PackResult: 시트 합치기 결과입니다. 저장 대화상자를 취소하면 Canceled 만 true 입니다.
type PackResult struct {
	Canceled bool   `json:"canceled"`
	Output   string `json:"output"`
	Message  string `json:"message"`
}

// FileEntry: 목록에 표시할 입력 파일 정보입니다.
type FileEntry struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Dir    string `json:"dir"`
	Root   string `json:"root"` // 폴더를 펼쳐 찾은 경우 그 폴더 (출력 폴더에서 하위 구조를 유지하는 데 사용)
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
	"upscale":   "-upscaled",
	"trim":      "-trimmed",
	"extract":   "-palette",
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

// Describe: 파일 정보를 다시 읽습니다 (덮어쓰기 후 크기 갱신 등).
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
	case "upscale":
		if req.Scale < processor.MinUpscale || req.Scale > processor.MaxUpscale {
			return nil, fmt.Errorf("확대 배율은 %d~%d 사이여야 합니다", processor.MinUpscale, processor.MaxUpscale)
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.UpscaleImage(src, req.Scale)
		}, nil
	case "trim":
		if req.Pad < 0 {
			return nil, errors.New("여백은 0 이상이어야 합니다")
		}
		return func(src image.Image) (image.Image, string, error) {
			return processor.TrimImage(src, req.Pad)
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
	return nil, fmt.Errorf("알 수 없는 명령: %s", req.Command)
}

// buildTask: 요청을 실행할 작업과 옵션으로 바꿉니다. produced 는 작업이 출력 경로를 직접 정하는 경우(slice)
// 저장한 파일을 알려 주는 콜백이며 nil 이어도 됩니다 (여러 스레드에서 호출될 수 있음).
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
		o.InPlace = false // 팔레트 이미지로 원본을 덮어쓰면 안 됩니다.
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

// Run: 파일들을 백그라운드에서 처리합니다. 진행 상황은 "job:result", 완료는 "job:done" 이벤트로 전달됩니다.
func (a *App) Run(req JobRequest, files []InputFile) error {
	if len(files) == 0 {
		return errors.New("처리할 파일이 없습니다")
	}
	if req.Command == "extract" && strings.TrimSpace(req.SaveName) != "" && len(files) > 1 {
		return fmt.Errorf("팔레트 목록에 저장은 파일이 하나일 때만 쓸 수 있습니다 (대상 %d개)", len(files))
	}
	var outMu sync.Mutex
	var outputs []string // 이번 실행으로 새로 만들어진 파일 (덮어쓴 원본은 제외)
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

// BackupPair: 덮어쓴 원본 파일과, 덮어쓰기 전에 복사해 둔 백업 파일입니다.
type BackupPair struct {
	Path   string `json:"path"`
	Backup string `json:"backup"`
}

// RestoreReport: 백업 복원 결과입니다. Restored 는 되돌린 파일 수, Errors 는 실패한 파일별 사유입니다.
type RestoreReport struct {
	Restored int      `json:"restored"`
	Errors   []string `json:"errors"`
}

// RestoreBackups: 백업해 둔 원본을 원래 위치로 되돌립니다. 작업이 진행 중일 때는 거부합니다.
func (a *App) RestoreBackups(pairs []BackupPair) (RestoreReport, error) {
	a.mu.Lock()
	busy := a.cancelJob != nil
	a.mu.Unlock()
	if busy {
		return RestoreReport{}, errors.New("작업이 진행 중입니다. 끝난 뒤에 되돌리세요")
	}
	rep := RestoreReport{Errors: []string{}}
	for _, p := range pairs {
		if !strings.EqualFold(filepath.Ext(p.Path), ".png") {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: PNG 파일이 아닙니다", p.Path))
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

// swatchStrip: 1px 짜리 팔레트 한 줄을 색마다 정사각형 칸으로 키워 한눈에 볼 수 있게 합니다 (한 줄에 최대 16색).
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

// ---------- 시트 합치기 ----------

func packSheet(req JobRequest, paths []string) (*image.NRGBA, string, error) {
	if len(paths) == 0 {
		return nil, "", errors.New("합칠 파일이 없습니다")
	}
	images, err := processor.LoadImages(processor.SortedPaths(paths))
	if err != nil {
		return nil, "", err
	}
	return processor.PackSheet(images, req.Cols, req.Gap, req.Align == "center")
}

// PreviewPack: 목록의 파일을 자연 정렬 순서로 합친 시트를 저장하지 않고 미리 보여줍니다.
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

// RunPack: 저장 위치를 물어본 뒤 파일들을 한 장의 시트로 합쳐 저장합니다.
// 저장 대화상자가 덮어쓰기 여부를 확인하므로 기존 파일은 덮어쓰되, 입력 파일을 덮어쓰는 경로는 거부합니다.
func (a *App) RunPack(req JobRequest, paths []string) (PackResult, error) {
	if len(paths) == 0 {
		return PackResult{}, errors.New("합칠 파일이 없습니다")
	}
	sorted := processor.SortedPaths(paths)
	if _, _, err := packSheet(req, sorted); err != nil {
		return PackResult{}, err
	}
	out, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                "시트 저장",
		DefaultDirectory:     filepath.Dir(sorted[0]),
		DefaultFilename:      "sheet.png",
		Filters:              []runtime.FileFilter{{DisplayName: "PNG 이미지 (*.png)", Pattern: "*.png"}},
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

// ---------- 팔레트 ----------

// forgetPalette: 같은 이름으로 팔레트를 다시 저장했을 때 캐시된 색상이 남지 않게 지웁니다.
func (a *App) forgetPalette(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	a.palMu.Lock()
	defer a.palMu.Unlock()
	delete(a.palettes, name)
}

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
