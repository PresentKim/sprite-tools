package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"sprite-tools/pkg/common"
)

func TestResolveCommand(t *testing.T) {
	cases := map[string]string{
		"reduce": "reduce", "rd": "reduce", "red": "reduce", "simplify": "reduce",
		"slice": "slice", "sl": "slice", "split": "slice",
		"downscale": "downscale", "ds": "downscale", "d": "downscale",
		"index": "index", "ix": "index", "i": "index", "palette": "index",
		"recolor": "recolor", "rc": "recolor", "rec": "recolor", "palette-convert": "recolor",
		"upscale": "upscale", "us": "upscale", "up": "upscale",
		"trim": "trim", "tr": "trim", "t": "trim",
		"extract": "extract", "ex": "extract", "e": "extract",
		"pack": "pack", "pk": "pack", "p": "pack",
		"RD": "reduce",
	}
	for in, want := range cases {
		cmd, _ := resolveCommand(in)
		if cmd == nil || cmd.name != want {
			t.Errorf("%q: want %s, got %v", in, want, cmd)
		}
	}

	if cmd, cands := resolveCommand("re"); cmd != nil || !slices.Equal(cands, []string{"reduce", "recolor"}) {
		t.Errorf("ambiguous prefix: got %v %v", cmd, cands)
	}
	if cmd, _ := resolveCommand("xyz"); cmd != nil {
		t.Errorf("unknown command resolved to %s", cmd.name)
	}
}

func TestAliasesUnique(t *testing.T) {
	seen := map[string]string{}
	for _, c := range commands {
		for _, n := range append(append([]string{c.name}, c.aliases...), c.legacy...) {
			if prev, ok := seen[n]; ok {
				t.Errorf("%q used by both %s and %s", n, prev, c.name)
			}
			seen[n] = c.name
		}
	}
}

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.NRGBA{200, 10, 10, 255})
	img.Set(2, 2, color.NRGBA{10, 10, 200, 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestBackupFlag(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writeTestPNG(t, p)
	orig, _ := os.ReadFile(p)

	// -w 없이 -backup 만 쓰면 사용법 오류
	if code := run([]string{"index", "-backup", p}); code != exitUsage {
		t.Errorf("-backup 단독 사용: exit %d, want %d", code, exitUsage)
	}
	if code := run([]string{"index", "-w", "-backup", p}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, common.BackupDirName, "*", "a.png"))
	if len(matches) != 1 {
		t.Fatalf("백업 파일 %d개, want 1", len(matches))
	}
	if got, _ := os.ReadFile(matches[0]); !bytes.Equal(got, orig) {
		t.Error("백업이 원본과 다릅니다")
	}
}
