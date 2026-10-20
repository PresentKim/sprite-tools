package common

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
)

func TestOutputPath(t *testing.T) {
	tests := []struct {
		name string
		o    Options
		in   string
		want string
	}{
		{"접미사", Options{Suffix: "_out"}, filepath.Join("d", "a.png"), filepath.Join("d", "a_out.png")},
		{"확장자 없음", Options{Suffix: "_out"}, "a", "a_out"},
		{"덮어쓰기", Options{InPlace: true, Suffix: "_x"}, "a.png", "a.png"},
		{"출력 없음", Options{NoOutput: true, Suffix: "_x"}, "a.png", ""},
		{"출력 없음이 덮어쓰기보다 우선", Options{NoOutput: true, InPlace: true}, "a.png", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.o.OutputPath(tt.in); got != tt.want {
				t.Errorf("OutputPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		o       Options
		wantErr bool
	}{
		{"접미사 있음", Options{Suffix: "_x"}, false},
		{"접미사 없음", Options{}, true},
		{"덮어쓰기", Options{InPlace: true}, false},
		{"출력 없음", Options{NoOutput: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.o.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// touch: 디렉터리를 만들고 빈 파일을 생성합니다.
func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// names: 루트 기준 상대 경로 목록(슬래시 구분)으로 변환합니다.
func names(t *testing.T, root string, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func TestCollectFiles(t *testing.T) {
	// 공통 구조:
	//   a.png, B.PNG, c.txt, a_out.png
	//   sub/d.png, sub/deep/e.png
	//   .hidden/f.png, skip/g.png
	setup := func(t *testing.T) string {
		root := t.TempDir()
		for _, p := range []string{
			"a.png", "B.PNG", "c.txt", "a_out.png",
			"sub/d.png", "sub/deep/e.png",
			".hidden/f.png", "skip/g.png",
		} {
			touch(t, filepath.Join(root, filepath.FromSlash(p)))
		}
		return root
	}

	tests := []struct {
		name string
		opt  func(root string) Options
		want []string
	}{
		{
			"비재귀: 하위 디렉터리 제외, 대소문자 무시",
			func(root string) Options { return Options{Roots: []string{root}, Suffix: "_out"} },
			[]string{"B.PNG", "a.png"},
		},
		{
			"재귀: 숨김 디렉터리 제외",
			func(root string) Options { return Options{Roots: []string{root}, Suffix: "_out", Recursive: true} },
			[]string{"B.PNG", "a.png", "skip/g.png", "sub/d.png", "sub/deep/e.png"},
		},
		{
			"ExcludeDirs 제외",
			func(root string) Options {
				return Options{Roots: []string{root}, Suffix: "_out", Recursive: true,
					ExcludeDirs: []string{filepath.Join(root, "skip"), filepath.Join(root, "sub", "deep")}}
			},
			[]string{"B.PNG", "a.png", "sub/d.png"},
		},
		{
			"InPlace 에서는 이전 출력도 포함",
			func(root string) Options { return Options{Roots: []string{root}, InPlace: true} },
			[]string{"B.PNG", "a.png", "a_out.png"},
		},
		{
			"패턴 지정 (대문자 패턴도 허용)",
			func(root string) Options { return Options{Roots: []string{root}, Pattern: "*.TXT", Suffix: "_out"} },
			[]string{"c.txt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := setup(t)
			got, err := CollectFiles(tt.opt(root))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(names(t, root, got), tt.want) {
				t.Errorf("got %v, want %v", names(t, root, got), tt.want)
			}
		})
	}
}

func TestCollectFilesExplicitFile(t *testing.T) {
	root := t.TempDir()
	txt := filepath.Join(root, "note.txt")
	out := filepath.Join(root, "x_out.png")
	touch(t, txt)
	touch(t, out)
	got, err := CollectFiles(Options{Roots: []string{txt, out}, Suffix: "_out"})
	if err != nil {
		t.Fatal(err)
	}
	// 직접 지정한 파일은 패턴/접미사와 관계없이 포함됩니다.
	want := []string{txt, out}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectFilesDedupAndSort(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"z.png", "m.png", "a.png"} {
		touch(t, filepath.Join(root, n))
	}
	// 같은 파일이 디렉터리와 직접 지정으로 중복해서 들어옴
	got, err := CollectFiles(Options{
		Roots:  []string{root, filepath.Join(root, "a.png"), root},
		Suffix: "_out",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "a.png"), filepath.Join(root, "m.png"), filepath.Join(root, "z.png")}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectFilesErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := CollectFiles(Options{Roots: []string{filepath.Join(root, "nope")}, Suffix: "_o"}); err == nil {
		t.Error("없는 경로는 에러여야 합니다")
	}
	if _, err := CollectFiles(Options{Roots: []string{root}, Pattern: "[", Suffix: "_o"}); err == nil {
		t.Error("잘못된 패턴은 에러여야 합니다")
	}
}

func fileList(n int) []string {
	files := make([]string, n)
	for i := range files {
		files[i] = filepath.Join("dir", string(rune('a'+i))+".png")
	}
	return files
}

func TestRunFilesCounts(t *testing.T) {
	files := []string{"ok.png", "skip.png", "fail.png", "panic.png", "ok2.png"}
	task := func(in, out string) (string, error) {
		switch in {
		case "skip.png":
			return "", Skip("건너뜀 %s", in)
		case "fail.png":
			return "", errors.New("boom")
		case "panic.png":
			panic("oops")
		}
		return "완료", nil
	}
	byInput := map[string]Result{}
	sum := RunFiles(context.Background(), files, Options{Suffix: "_o", Threads: 3}, task, func(r Result) {
		byInput[r.Input] = r
	})
	if sum.Done != 2 || sum.Skipped != 1 || sum.Failed != 2 || sum.Canceled {
		t.Errorf("summary = %+v", sum)
	}
	if byInput["skip.png"].Status != StatusSkipped || byInput["fail.png"].Status != StatusFailed ||
		byInput["panic.png"].Status != StatusFailed || byInput["ok.png"].Status != StatusDone {
		t.Errorf("상태가 올바르지 않습니다: %+v", byInput)
	}
	if byInput["ok.png"].Output != "ok_o.png" {
		t.Errorf("Output = %q", byInput["ok.png"].Output)
	}
}

func TestRunFilesReport(t *testing.T) {
	for _, threads := range []int{1, 4, 100, 0} {
		files := fileList(10)
		var results []Result
		sum := RunFiles(context.Background(), files, Options{Suffix: "_o", Threads: threads},
			func(in, out string) (string, error) { return "", nil },
			func(r Result) { results = append(results, r) })
		if len(results) != len(files) || sum.Done != len(files) {
			t.Fatalf("threads=%d: report %d회, summary=%+v", threads, len(results), sum)
		}
		seen := map[int]bool{}
		for i, r := range results {
			if r.Completed != i+1 {
				t.Errorf("threads=%d: Completed = %d, want %d", threads, r.Completed, i+1)
			}
			if r.Total != len(files) {
				t.Errorf("threads=%d: Total = %d", threads, r.Total)
			}
			if seen[r.Index] {
				t.Errorf("threads=%d: Index %d 중복", threads, r.Index)
			}
			seen[r.Index] = true
		}
	}
}

func TestRunFilesCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ran, reported atomic.Int32
	sum := RunFiles(ctx, fileList(5), Options{Suffix: "_o", Threads: 2},
		func(in, out string) (string, error) { ran.Add(1); return "", nil },
		func(Result) { reported.Add(1) })
	if !sum.Canceled {
		t.Error("Canceled 가 true 여야 합니다")
	}
	if ran.Load() != 0 || reported.Load() != 0 || sum.Done+sum.Skipped+sum.Failed != 0 {
		t.Errorf("처리되면 안 됩니다: ran=%d reported=%d sum=%+v", ran.Load(), reported.Load(), sum)
	}
}

func TestRunFilesEmpty(t *testing.T) {
	sum := RunFiles(context.Background(), nil, Options{Suffix: "_o"},
		func(in, out string) (string, error) { return "", nil }, nil)
	if sum.Done+sum.Skipped+sum.Failed != 0 || sum.Canceled {
		t.Errorf("summary = %+v", sum)
	}
}
