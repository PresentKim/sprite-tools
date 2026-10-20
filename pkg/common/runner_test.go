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
		{"suffix", Options{Suffix: "_out"}, filepath.Join("d", "a.png"), filepath.Join("d", "a_out.png")},
		{"no extension", Options{Suffix: "_out"}, "a", "a_out"},
		{"in place", Options{InPlace: true, Suffix: "_x"}, "a.png", "a.png"},
		{"no output", Options{NoOutput: true, Suffix: "_x"}, "a.png", ""},
		{"no output takes precedence over in place", Options{NoOutput: true, InPlace: true}, "a.png", ""},
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
		{"with suffix", Options{Suffix: "_x"}, false},
		{"without suffix", Options{}, true},
		{"in place", Options{InPlace: true}, false},
		{"no output", Options{NoOutput: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.o.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// touch creates the directory and an empty file.
func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// names converts to a list of paths relative to the root (slash-separated).
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
	// Common layout:
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
			"non-recursive: excludes subdirectories, case-insensitive",
			func(root string) Options { return Options{Roots: []string{root}, Suffix: "_out"} },
			[]string{"B.PNG", "a.png"},
		},
		{
			"recursive: excludes hidden directories",
			func(root string) Options { return Options{Roots: []string{root}, Suffix: "_out", Recursive: true} },
			[]string{"B.PNG", "a.png", "skip/g.png", "sub/d.png", "sub/deep/e.png"},
		},
		{
			"ExcludeDirs excluded",
			func(root string) Options {
				return Options{Roots: []string{root}, Suffix: "_out", Recursive: true,
					ExcludeDirs: []string{filepath.Join(root, "skip"), filepath.Join(root, "sub", "deep")}}
			},
			[]string{"B.PNG", "a.png", "sub/d.png"},
		},
		{
			"InPlace also includes earlier outputs",
			func(root string) Options { return Options{Roots: []string{root}, InPlace: true} },
			[]string{"B.PNG", "a.png", "a_out.png"},
		},
		{
			"pattern given (uppercase pattern also accepted)",
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
	// Directly given files are included regardless of pattern/suffix.
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
	// The same file comes in twice, via a directory and directly
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
		t.Error("a nonexistent path should be an error")
	}
	if _, err := CollectFiles(Options{Roots: []string{root}, Pattern: "[", Suffix: "_o"}); err == nil {
		t.Error("an invalid pattern should be an error")
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
			return "", Skip("skipped %s", in)
		case "fail.png":
			return "", errors.New("boom")
		case "panic.png":
			panic("oops")
		}
		return "done", nil
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
		t.Errorf("unexpected state: %+v", byInput)
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
			t.Fatalf("threads=%d: report called %d times, summary=%+v", threads, len(results), sum)
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
				t.Errorf("threads=%d: duplicate Index %d", threads, r.Index)
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
		t.Error("Canceled should be true")
	}
	if ran.Load() != 0 || reported.Load() != 0 || sum.Done+sum.Skipped+sum.Failed != 0 {
		t.Errorf("nothing should be processed: ran=%d reported=%d sum=%+v", ran.Load(), reported.Load(), sum)
	}
}

func TestRunFilesEmpty(t *testing.T) {
	sum := RunFiles(context.Background(), nil, Options{Suffix: "_o"},
		func(in, out string) (string, error) { return "", nil }, nil)
	if sum.Done+sum.Skipped+sum.Failed != 0 || sum.Canceled {
		t.Errorf("summary = %+v", sum)
	}
}

func TestOutputForOutDir(t *testing.T) {
	root := filepath.Join("proj", "art")
	o := Options{OutDir: "out", Suffix: "-x"}
	tests := []struct {
		name string
		o    Options
		in   Input
		want string
	}{
		{"keeps path relative to root", o, Input{filepath.Join(root, "sub", "a.png"), root}, filepath.Join("out", "sub", "a-x.png")},
		{"file directly under root", o, Input{filepath.Join(root, "a.png"), root}, filepath.Join("out", "a-x.png")},
		{"directly given file goes right under OutDir", o, Input{Path: filepath.Join("elsewhere", "deep", "a.png")}, filepath.Join("out", "a-x.png")},
		{"without suffix", Options{OutDir: "out"}, Input{filepath.Join(root, "sub", "a.png"), root}, filepath.Join("out", "sub", "a.png")},
		{"without OutDir, next to the original", Options{Suffix: "-x"}, Input{filepath.Join(root, "a.png"), root}, filepath.Join(root, "a-x.png")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.o.OutputFor(tt.in); got != tt.want {
				t.Errorf("OutputFor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateOutDir(t *testing.T) {
	if err := (Options{InPlace: true, OutDir: "out"}).Validate(); err == nil {
		t.Error("InPlace + OutDir should be an error")
	}
	if err := (Options{OutDir: "out"}).Validate(); err != nil {
		t.Errorf("OutDir alone should allow an empty suffix: %v", err)
	}
	if err := (Options{}).Validate(); err == nil {
		t.Error("neither suffix nor OutDir should be an error")
	}
}

func TestCollectInputsRoots(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	touch(t, filepath.Join(root, "a.png"))
	touch(t, filepath.Join(root, "sub", "a.png"))
	explicit := filepath.Join(other, "z.png")
	touch(t, explicit)

	inputs, err := CollectInputs(Options{Roots: []string{root, explicit}, Recursive: true, OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, in := range inputs {
		got[in.Path] = in.Root
	}
	want := map[string]string{
		filepath.Join(root, "a.png"):        root,
		filepath.Join(root, "sub", "a.png"): root,
		explicit:                            "",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for p, r := range want {
		if g, ok := got[p]; !ok || g != r {
			t.Errorf("%s: root = %q (present=%v), want %q", p, g, ok, r)
		}
	}
}

func TestCollectFilesExcludesOutDir(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "a.png"))
	touch(t, filepath.Join(root, "out", "a.png"))
	touch(t, filepath.Join(root, "out", "deep", "b.png"))
	touch(t, filepath.Join(root, "sub", "c.png"))
	got, err := CollectFiles(Options{Roots: []string{root}, Recursive: true, OutDir: filepath.Join(root, "out")})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.png", "sub/c.png"}; !slices.Equal(names(t, root, got), want) {
		t.Errorf("got %v, want %v", names(t, root, got), want)
	}
}

func TestOutDirKeepsOwnOutputSkipping(t *testing.T) {
	// When the output folder is the source folder itself, earlier outputs (suffixed files) must be dropped from the input.
	root := t.TempDir()
	touch(t, filepath.Join(root, "a.png"))
	touch(t, filepath.Join(root, "a-x.png"))
	got, err := CollectFiles(Options{Roots: []string{root}, OutDir: root, Suffix: "-x"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.png"}; !slices.Equal(names(t, root, got), want) {
		t.Errorf("got %v, want %v", names(t, root, got), want)
	}
}

func TestRunInputsOutDir(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "a.png"))
	touch(t, filepath.Join(root, "sub", "a.png"))
	outDir := filepath.Join(t.TempDir(), "result")
	o := Options{Roots: []string{root}, Recursive: true, OutDir: outDir, Suffix: "-x", Threads: 2}
	inputs, err := CollectInputs(o)
	if err != nil {
		t.Fatal(err)
	}
	outs := map[string]bool{}
	sum := RunInputs(context.Background(), inputs, o, func(in, out string) (string, error) {
		return "", os.WriteFile(out, []byte("x"), 0o644)
	}, func(r Result) { outs[r.Output] = true })
	if sum.Done != 2 || sum.Failed != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	for _, rel := range []string{"a-x.png", filepath.Join("sub", "a-x.png")} {
		if _, err := os.Stat(filepath.Join(outDir, rel)); err != nil {
			t.Errorf("%s should have been created: %v", rel, err)
		}
		if !outs[filepath.Join(outDir, rel)] {
			t.Errorf("result lacks output path %s", rel)
		}
	}
}

func TestPlanOutputsConflicts(t *testing.T) {
	root := t.TempDir()
	// Exporting to the same folder without a suffix would overwrite the original, so it is treated as a failure.
	in := Input{Path: filepath.Join(root, "a.png"), Root: root}
	plans := Options{OutDir: root}.PlanOutputs([]Input{in})
	if plans[0].Err == nil {
		t.Error("the same path as the original should be an error")
	}
	// Same-named files from different directories given directly collide in OutDir
	a := Input{Path: filepath.Join(root, "x", "a.png")}
	b := Input{Path: filepath.Join(root, "y", "a.png")}
	plans = Options{OutDir: filepath.Join(root, "out")}.PlanOutputs([]Input{a, b})
	if plans[0].Err != nil || plans[1].Err == nil {
		t.Errorf("only the second should be a collision: %+v", plans)
	}
	// Overwriting is not checked
	plans = Options{InPlace: true}.PlanOutputs([]Input{in})
	if plans[0].Err != nil {
		t.Errorf("InPlace is allowed: %v", plans[0].Err)
	}
}

func TestNaturalSort(t *testing.T) {
	list := []string{"frame-10.png", "frame-2.png", "Frame-1.png", "frame-02.png", "a.png", "frame-1b.png"}
	NaturalSort(list)
	want := []string{"a.png", "Frame-1.png", "frame-1b.png", "frame-2.png", "frame-02.png", "frame-10.png"}
	if !slices.Equal(list, want) {
		t.Errorf("got %v, want %v", list, want)
	}
}
