package common

import (
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
	DryRun      bool
	ExcludeDirs []string // 탐색에서 제외할 디렉터리
}

type Summary struct {
	Done, Skipped, Failed int
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

// Run: 입력 파일을 수집해 병렬로 처리하고 진행 상황과 요약을 출력합니다.
func Run(o Options, task Task) (Summary, error) {
	var sum Summary
	if !o.InPlace && !o.NoOutput && o.Suffix == "" {
		return sum, errors.New("출력 접미사(-s)가 비어 있습니다. 원본을 덮어쓰려면 -w 를 사용하세요")
	}

	files, err := CollectFiles(o)
	if err != nil {
		return sum, err
	}
	if len(files) == 0 {
		fmt.Printf("처리할 파일이 없습니다 (패턴: %s)\n", o.Pattern)
		return sum, nil
	}

	if o.DryRun {
		fmt.Printf("[미리보기] %d개 파일이 처리됩니다. 실제로는 아무것도 쓰지 않습니다.\n", len(files))
		for _, f := range files {
			if out := o.OutputPath(f); out != "" && out != f {
				fmt.Printf("  %s -> %s\n", f, out)
			} else if out == f {
				fmt.Printf("  %s (덮어쓰기)\n", f)
			} else {
				fmt.Printf("  %s\n", f)
			}
		}
		return sum, nil
	}

	threads := o.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
	}
	threads = min(threads, len(files))

	start := time.Now()
	fmt.Printf("%d개 파일 처리 시작 (스레드 %d개)\n", len(files), threads)

	jobs := make(chan int)
	var mu sync.Mutex
	var wg sync.WaitGroup
	completed := 0
	width := len(fmt.Sprint(len(files)))

	for w := 0; w < threads; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				in := files[i]
				out := o.OutputPath(in)
				msg, err := runTask(task, in, out)

				mu.Lock()
				completed++
				var label string
				switch {
				case err == nil:
					sum.Done++
					label = "완료  "
				case IsSkip(err):
					sum.Skipped++
					label = "건너뜀"
					msg = err.Error()
				default:
					sum.Failed++
					label = "실패  "
					msg = err.Error()
				}
				target := in
				if out != "" && out != in {
					target = in + " -> " + filepath.Base(out)
				}
				if msg != "" {
					msg = "  (" + msg + ")"
				}
				fmt.Printf("[%*d/%d] %s %s%s\n", width, completed, len(files), label, target, msg)
				mu.Unlock()
			}
		}()
	}

	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	fmt.Printf("\n완료 %d · 건너뜀 %d · 실패 %d  (%.1fs)\n", sum.Done, sum.Skipped, sum.Failed, time.Since(start).Seconds())
	return sum, nil
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
