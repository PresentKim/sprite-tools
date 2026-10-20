package common

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Task: 입력 파일 하나를 처리하고 결과 요약 메시지를 반환합니다.
// 처리할 필요가 없는 경우 Skip(...) 으로 만든 에러를 반환합니다.
type Task func(inputPath, outputPath string) (string, error)

type skipError struct{ msg string }

func (e skipError) Error() string { return e.msg }

// Skip: 실패가 아닌 "건너뜀"으로 집계될 에러를 만듭니다.
func Skip(format string, a ...any) error {
	return skipError{fmt.Sprintf(format, a...)}
}

func IsSkip(err error) bool {
	var s skipError
	return errors.As(err, &s)
}

type Options struct {
	Roots       []string // 처리할 파일 또는 디렉터리 (비어 있으면 ".")
	Pattern     string   // 디렉터리 탐색 시 파일 이름 패턴 (대소문자 무시)
	Recursive   bool     // 하위 디렉터리까지 탐색
	Suffix      string   // 출력 파일 접미사 (InPlace 가 아닐 때)
	InPlace     bool     // 원본 파일을 직접 덮어씀
	OutDir      string   // 비어 있지 않으면 결과를 이 폴더에 저장 (루트 기준 상대 경로 유지, InPlace 와 함께 쓸 수 없음)
	NoOutput    bool     // 작업이 출력 경로를 직접 결정함 (slice 등)
	Threads     int
	ExcludeDirs []string // 탐색에서 제외할 디렉터리
	Backup      bool     // InPlace 일 때, 덮어쓰기 전에 원본을 <파일 폴더>/BackupDirName/<실행 시각>/ 에 복사
}

// BackupDirName: 덮어쓰기 전 원본을 보관하는 숨김 폴더 이름입니다. 점으로 시작하므로 폴더 탐색에서 건너뜁니다.
const BackupDirName = ".sprite-tools-backup"

// backupStampLayout: 한 번 실행에 하나씩 붙는 백업 하위 폴더 이름의 형식입니다.
const backupStampLayout = "20060102-150405"

// Input: 수집된 입력 파일 하나와, 그 파일을 찾은 루트 폴더입니다.
// 파일을 직접 지정했다면 Root 는 비어 있습니다.
type Input struct {
	Path string
	Root string
}

type Status string

const (
	StatusDone    Status = "done"
	StatusSkipped Status = "skipped"
	StatusFailed  Status = "failed"
)

// Result: 파일 하나의 처리 결과입니다.
type Result struct {
	Index     int    `json:"index"`     // 입력 목록에서의 위치
	Completed int    `json:"completed"` // 지금까지 끝난 파일 수
	Total     int    `json:"total"`
	Input     string `json:"input"`
	Output    string `json:"output"`
	Status    Status `json:"status"`
	Message   string `json:"message"`
	Backup    string `json:"backup,omitempty"` // 덮어쓰기 전에 복사해 둔 원본 경로 (백업했을 때만)
}

type Summary struct {
	Done       int           `json:"done"`
	Skipped    int           `json:"skipped"`
	Failed     int           `json:"failed"`
	Canceled   bool          `json:"canceled"`
	Elapsed    time.Duration `json:"elapsed"`
	Backups    int           `json:"backups"`    // 실제로 남은 백업 파일 수
	BackupDirs []string      `json:"backupDirs"` // 백업이 저장된 폴더들 (정렬됨)
}

// OutputPath: 옵션에 따른 출력 경로를 계산합니다. 루트를 모르는(직접 지정한) 파일로 취급합니다.
func (o Options) OutputPath(in string) string {
	return o.OutputFor(Input{Path: in})
}

// OutputFor: 입력 파일의 출력 경로를 계산합니다.
// OutDir 가 있으면 루트 기준 상대 경로를 유지한 채 OutDir 아래에 두고(직접 지정한 파일은 OutDir 바로 아래),
// 없으면 원본 옆에 접미사를 붙입니다.
func (o Options) OutputFor(in Input) string {
	if o.NoOutput {
		return ""
	}
	if o.InPlace {
		return in.Path
	}
	rel := in.Path
	if o.OutDir != "" {
		rel = filepath.Base(in.Path)
		if in.Root != "" {
			if r, err := filepath.Rel(in.Root, in.Path); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
	}
	ext := filepath.Ext(rel)
	rel = strings.TrimSuffix(rel, ext) + o.Suffix + ext
	if o.OutDir != "" {
		return filepath.Join(o.OutDir, rel)
	}
	return rel
}

// Validate: 출력 설정이 올바른지 확인합니다.
// 출력 폴더(OutDir)를 쓰면 접미사가 비어 있어도 되지만, 출력이 원본과 같은 경로가 되는 파일은 실행 시 실패로 처리됩니다.
func (o Options) Validate() error {
	if o.InPlace && o.OutDir != "" {
		return errors.New("덮어쓰기(-w)와 출력 폴더(-o)는 함께 쓸 수 없습니다")
	}
	if o.Backup && !o.InPlace {
		return errors.New("원본 백업(-backup)은 덮어쓰기(-w)와 함께 쓸 때만 의미가 있습니다")
	}
	if !o.InPlace && !o.NoOutput && o.Suffix == "" && o.OutDir == "" {
		return errors.New("출력 접미사가 비어 있습니다. 원본을 덮어쓰려면 덮어쓰기(-w)를, 다른 폴더에 저장하려면 출력 폴더(-o)를 사용하세요")
	}
	return nil
}

// SamePath: 두 경로가 같은 파일을 가리키는지(대소문자 무시) 확인합니다.
func SamePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(aa, bb)
}

// Plan: 입력 하나의 출력 계획입니다. Err 가 있으면 이 파일은 처리하지 않습니다.
type Plan struct {
	Output string
	Err    error
}

// PlanOutputs: 모든 입력의 출력 경로를 계산하고, 원본을 덮어쓰게 되거나 다른 입력과 출력이 겹치는 파일에는 Err 를 채웁니다.
// 덮어쓰기(InPlace)는 의도된 동작이므로 검사하지 않습니다.
func (o Options) PlanOutputs(inputs []Input) []Plan {
	plans := make([]Plan, len(inputs))
	claimed := make(map[string]int)
	for i, in := range inputs {
		out := o.OutputFor(in)
		plans[i].Output = out
		if out == "" || o.InPlace {
			continue
		}
		if SamePath(in.Path, out) {
			plans[i].Err = errors.New("출력 경로가 원본과 같아 덮어쓰게 됩니다. 접미사를 지정하거나 다른 출력 폴더를 사용하세요")
			continue
		}
		abs, err := filepath.Abs(out)
		if err != nil {
			abs = out
		}
		key := strings.ToLower(abs)
		if prev, ok := claimed[key]; ok {
			plans[i].Err = fmt.Errorf("출력 경로가 %s 와 겹칩니다", inputs[prev].Path)
			continue
		}
		claimed[key] = i
	}
	return plans
}

func (o Options) isOwnOutput(path string) bool {
	if o.InPlace || o.NoOutput || o.Suffix == "" {
		return false
	}
	name := filepath.Base(path)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return strings.HasSuffix(strings.ToLower(stem), strings.ToLower(o.Suffix))
}

// CollectFiles: 옵션에 맞는 입력 파일 경로 목록을 정렬된 상태로 반환합니다.
func CollectFiles(o Options) ([]string, error) {
	inputs, err := CollectInputs(o)
	if err != nil {
		return nil, err
	}
	files := make([]string, len(inputs))
	for i, in := range inputs {
		files[i] = in.Path
	}
	return files, nil
}

// CollectInputs: CollectFiles 와 같지만 각 파일을 찾은 루트도 함께 반환합니다(출력 폴더에서 상대 경로를 유지하는 데 필요).
// OutDir 안쪽은 탐색하지 않으므로 이전 출력이 다시 입력으로 들어오지 않습니다.
func CollectInputs(o Options) ([]Input, error) {
	roots := o.Roots
	if len(roots) == 0 {
		roots = []string{"."}
	}
	pattern := strings.ToLower(o.Pattern)
	if pattern == "" {
		pattern = "*.png"
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("잘못된 파일 패턴 '%s': %v", o.Pattern, err)
	}

	excludeDirs := o.ExcludeDirs
	if o.OutDir != "" && !o.InPlace && !o.NoOutput {
		excludeDirs = append(slices.Clone(excludeDirs), o.OutDir)
	}
	excluded := make([]string, 0, len(excludeDirs))
	for _, d := range excludeDirs {
		if abs, err := filepath.Abs(d); err == nil {
			excluded = append(excluded, abs)
		}
	}
	isExcluded := func(dir string) bool {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return false
		}
		for _, e := range excluded {
			if strings.EqualFold(abs, e) {
				return true
			}
		}
		return false
	}

	seen := make(map[string]bool)
	var files []Input
	add := func(path, root string) {
		key := strings.ToLower(filepath.Clean(path))
		if !seen[key] {
			seen[key] = true
			files = append(files, Input{Path: path, Root: root})
		}
	}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("경로를 찾을 수 없습니다: %s", root)
		}
		if !info.IsDir() {
			// 직접 지정한 파일은 패턴과 관계없이 처리합니다.
			add(root, "")
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path == root {
					return nil
				}
				if !o.Recursive || strings.HasPrefix(d.Name(), ".") || isExcluded(path) {
					return filepath.SkipDir
				}
				return nil
			}
			if ok, _ := filepath.Match(pattern, strings.ToLower(d.Name())); !ok {
				return nil
			}
			if o.isOwnOutput(path) {
				return nil
			}
			add(path, root)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(files, func(a, b Input) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// RunFiles: 루트를 모르는 파일 경로들을 RunInputs 로 처리합니다.
func RunFiles(ctx context.Context, files []string, o Options, task Task, report func(Result)) Summary {
	inputs := make([]Input, len(files))
	for i, f := range files {
		inputs[i] = Input{Path: f}
	}
	return RunInputs(ctx, inputs, o, task, report)
}

// RunInputs: 주어진 파일들을 병렬로 처리합니다. report 는 파일 하나가 끝날 때마다 (순서대로 하나씩) 호출됩니다.
// ctx 가 취소되면 아직 시작하지 않은 파일은 처리하지 않습니다.
// 출력 폴더가 있으면 작업 전에 필요한 하위 폴더를 만들고, 출력이 원본/다른 출력과 겹치는 파일은 실패로 처리합니다.
func RunInputs(ctx context.Context, files []Input, o Options, task Task, report func(Result)) Summary {
	var sum Summary
	plans := o.PlanOutputs(files)
	start := time.Now()
	stamp := start.Format(backupStampLayout) // 한 번 실행의 백업은 모두 같은 시각 폴더에 둡니다.

	threads := o.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
	}
	threads = max(1, min(threads, len(files)))

	jobs := make(chan int)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for w := 0; w < threads; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				in, out := files[i].Path, plans[i].Output
				var msg string
				err := plans[i].Err
				if err == nil && o.OutDir != "" && out != "" {
					err = os.MkdirAll(filepath.Dir(out), 0o755)
				}
				// 원본을 먼저 복사해 두고, 작업이 실제로 끝나지 않았다면(건너뜀·실패) 복사본을 지웁니다.
				var backup string
				if err == nil && o.Backup && o.InPlace && out != "" {
					backup, err = backupFile(in, stamp)
				}
				if err == nil {
					msg, err = runTask(task, in, out)
				}
				if err != nil && backup != "" {
					os.Remove(backup)
					backup = ""
				}

				r := Result{Index: i, Total: len(files), Input: in, Output: out, Message: msg, Backup: backup}
				switch {
				case err == nil:
					r.Status = StatusDone
				case IsSkip(err):
					r.Status = StatusSkipped
					r.Message = err.Error()
				default:
					r.Status = StatusFailed
					r.Message = err.Error()
				}

				mu.Lock()
				switch r.Status {
				case StatusDone:
					sum.Done++
				case StatusSkipped:
					sum.Skipped++
				default:
					sum.Failed++
				}
				if r.Backup != "" {
					sum.Backups++
				}
				r.Completed = sum.Done + sum.Skipped + sum.Failed
				if report != nil {
					report(r)
				}
				mu.Unlock()
			}
		}()
	}

feed:
	for i := range files {
		select {
		case <-ctx.Done():
			sum.Canceled = true
			break feed
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()

	if o.Backup && o.InPlace {
		sum.BackupDirs = finishBackupDirs(files, stamp)
	}
	sum.Elapsed = time.Since(start)
	return sum
}

// backupFile: 원본을 <폴더>/BackupDirName/<stamp>/<이름> 으로 복사하고 그 경로를 반환합니다.
// 원본이 없으면 복사할 것이 없으므로 빈 경로를 돌려주고, 이후 작업이 스스로 실패하도록 둡니다.
func backupFile(in, stamp string) (string, error) {
	data, err := os.ReadFile(in)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("원본 백업 실패: %v", err)
	}
	dir := filepath.Join(filepath.Dir(in), BackupDirName, stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("원본 백업 실패: %v", err)
	}
	dst := filepath.Join(dir, filepath.Base(in))
	if err := WriteFileAtomic(dst, data); err != nil {
		return "", fmt.Errorf("원본 백업 실패: %v", err)
	}
	return dst, nil
}

// finishBackupDirs: 실행이 끝난 뒤 비어 있는 백업 폴더를 정리하고, 백업이 남은 폴더 목록을 반환합니다.
// 실행 중에 지우면 다른 스레드가 쓰려는 폴더를 없앨 수 있으므로 모두 끝난 뒤에 합니다.
func finishBackupDirs(files []Input, stamp string) []string {
	seen := make(map[string]bool)
	var kept []string
	for _, f := range files {
		root := filepath.Join(filepath.Dir(f.Path), BackupDirName)
		dir := filepath.Join(root, stamp)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if err := os.Remove(dir); err != nil { // 비어 있을 때만 지워짐
			if _, statErr := os.Stat(dir); statErr == nil {
				kept = append(kept, dir)
			}
			continue
		}
		os.Remove(root) // 다른 백업이 없을 때만 지워짐
	}
	slices.Sort(kept)
	return kept
}

// RestoreBackup: 백업해 둔 원본을 원래 위치(dest)로 되돌립니다. 백업 파일은 그대로 남깁니다.
// BackupDirName 폴더 안의 파일만 복원 대상으로 인정합니다.
func RestoreBackup(backup, dest string) error {
	// 백업은 <폴더>/.sprite-tools-backup/<시각>/<이름> 에 있으므로, <폴더>/<이름> 으로만 복원을 허용합니다.
	backup, dest = filepath.Clean(backup), filepath.Clean(dest)
	runDir := filepath.Dir(backup)
	backupRoot := filepath.Dir(runDir)
	if filepath.Base(backupRoot) != BackupDirName ||
		!strings.EqualFold(filepath.Dir(backupRoot), filepath.Dir(dest)) ||
		!strings.EqualFold(filepath.Base(backup), filepath.Base(dest)) {
		return fmt.Errorf("백업 파일과 복원 위치가 맞지 않습니다: %s -> %s", backup, dest)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("백업 파일을 읽을 수 없습니다: %v", err)
	}
	if err := WriteFileAtomic(dest, data); err != nil {
		return fmt.Errorf("복원 실패: %v", err)
	}
	return nil
}

// runTask: 작업 중 panic 이 발생해도 전체 실행이 중단되지 않도록 보호합니다.
func runTask(task Task, in, out string) (msg string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("내부 오류: %v", r)
		}
	}()
	return task(in, out)
}

// BackupFileNow: 파일 하나를 현재 시각 이름의 백업 폴더(<폴더>/BackupDirName/<시각>/<이름>)로 복사하고 그 경로를 반환합니다.
// 원본이 없으면 복사할 것이 없으므로 빈 경로와 nil 을 돌려줍니다.
func BackupFileNow(path string) (string, error) {
	return backupFile(path, time.Now().Format(backupStampLayout))
}
