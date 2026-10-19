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
	NoOutput    bool     // 작업이 출력 경로를 직접 결정함 (slice 등)
	Threads     int
	ExcludeDirs []string // 탐색에서 제외할 디렉터리
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
}

type Summary struct {
	Done     int           `json:"done"`
	Skipped  int           `json:"skipped"`
	Failed   int           `json:"failed"`
	Canceled bool          `json:"canceled"`
	Elapsed  time.Duration `json:"elapsed"`
}

// OutputPath: 옵션에 따른 출력 경로를 계산합니다.
func (o Options) OutputPath(in string) string {
	if o.NoOutput {
		return ""
	}
	if o.InPlace {
		return in
	}
	ext := filepath.Ext(in)
	return strings.TrimSuffix(in, ext) + o.Suffix + ext
}

// Validate: 출력 설정이 올바른지 확인합니다.
func (o Options) Validate() error {
	if !o.InPlace && !o.NoOutput && o.Suffix == "" {
		return errors.New("출력 접미사가 비어 있습니다. 원본을 덮어쓰려면 덮어쓰기(-w)를 사용하세요")
	}
	return nil
}

func (o Options) isOwnOutput(path string) bool {
	if o.InPlace || o.NoOutput || o.Suffix == "" {
		return false
	}
	name := filepath.Base(path)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return strings.HasSuffix(strings.ToLower(stem), strings.ToLower(o.Suffix))
}

// CollectFiles: 옵션에 맞는 입력 파일 목록을 정렬된 상태로 반환합니다.
func CollectFiles(o Options) ([]string, error) {
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

	excluded := make([]string, 0, len(o.ExcludeDirs))
	for _, d := range o.ExcludeDirs {
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
	var files []string
	add := func(path string) {
		key := strings.ToLower(filepath.Clean(path))
		if !seen[key] {
			seen[key] = true
			files = append(files, path)
		}
	}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("경로를 찾을 수 없습니다: %s", root)
		}
		if !info.IsDir() {
			// 직접 지정한 파일은 패턴과 관계없이 처리합니다.
			add(root)
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
			add(path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(files)
	return files, nil
}

// RunFiles: 주어진 파일들을 병렬로 처리합니다. report 는 파일 하나가 끝날 때마다 (순서대로 하나씩) 호출됩니다.
// ctx 가 취소되면 아직 시작하지 않은 파일은 처리하지 않습니다.
func RunFiles(ctx context.Context, files []string, o Options, task Task, report func(Result)) Summary {
	var sum Summary
	start := time.Now()

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
				in := files[i]
				out := o.OutputPath(in)
				msg, err := runTask(task, in, out)

				r := Result{Index: i, Total: len(files), Input: in, Output: out, Message: msg}
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

	sum.Elapsed = time.Since(start)
	return sum
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
