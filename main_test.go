package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
)

func TestResolveCommand(t *testing.T) {
	cases := map[string]string{
		"reduce": "reduce", "rd": "reduce", "red": "reduce", "simplify": "reduce",
		"slice": "slice", "sl": "slice", "split": "slice",
		"downscale": "downscale", "ds": "downscale", "dow": "downscale",
		"despeckle": "despeckle", "dsp": "despeckle", "de": "despeckle",
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
	// despeckle 이 생기면서 "d" 만으로는 downscale 을 고를 수 없습니다 (ds 는 downscale, dsp 는 despeckle 별칭이라 겹치지 않음).
	if cmd, cands := resolveCommand("d"); cmd != nil || !slices.Equal(cands, []string{"downscale", "despeckle"}) {
		t.Errorf("ambiguous prefix d: got %v %v", cmd, cands)
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

func TestDespeckleCommand(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 2; y < 6; y++ {
		for x := 2; x < 6; x++ {
			img.Set(x, y, color.NRGBA{200, 10, 10, 255})
		}
	}
	img.Set(0, 0, color.NRGBA{10, 10, 200, 255}) // 외톨이
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if code := run([]string{"dsp", "-min", "0", p}); code != exitUsage {
		t.Errorf("-min 0: exit %d, want %d", code, exitUsage)
	}
	if code := run([]string{"dsp", p}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	out, err := common.LoadPNG(filepath.Join(dir, "a-clean.png"))
	if err != nil {
		t.Fatal(err)
	}
	n := common.ToNRGBA(out)
	if n.NRGBAAt(0, 0).A != 0 || n.NRGBAAt(3, 3).A != 255 {
		t.Error("외톨이만 지워져야 합니다")
	}
}

func TestExtractLang(t *testing.T) {
	cases := []struct {
		in    []string
		rest  []string
		lang  string
		given bool
	}{
		{[]string{"reduce", "a.png"}, []string{"reduce", "a.png"}, "", false},
		{[]string{"--lang", "ko", "reduce"}, []string{"reduce"}, "ko", true},
		{[]string{"reduce", "-lang", "ko", "a.png"}, []string{"reduce", "a.png"}, "ko", true},
		{[]string{"reduce", "--lang=ko_KR.UTF-8"}, []string{"reduce"}, "ko_KR.UTF-8", true},
		{[]string{"--lang", "en", "x", "--lang", "ko"}, []string{"x"}, "ko", true},
		{[]string{"x", "--", "--lang", "ko"}, []string{"x", "--", "--lang", "ko"}, "", false},
	}
	for _, c := range cases {
		rest, lang, given, err := extractLang(c.in)
		if err != nil || !slices.Equal(rest, c.rest) || lang != c.lang || given != c.given {
			t.Errorf("%v: got %v %q %v %v", c.in, rest, lang, given, err)
		}
	}
	if _, _, _, err := extractLang([]string{"reduce", "--lang"}); err == nil {
		t.Error("--lang without a value should fail")
	}
}

func TestLanguagePrecedence(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLanguage("en") })
	check := func(want string, env string, args ...string) {
		t.Helper()
		t.Setenv(langEnv, env)
		if _, err := setupLanguage(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if got := i18n.Language(); got != want {
			t.Errorf("env=%q args=%v: language %q, want %q", env, args, got, want)
		}
	}
	check("en", "")                        // 기본값은 영어 (OS 로캘을 보지 않음)
	check("ko", "ko")                      // 환경 변수
	check("ko", "ko_KR.UTF-8")             // 로캘 형식도 허용
	check("en", "ko", "--lang", "en", "x") // 플래그가 환경 변수보다 우선
	check("ko", "en", "x", "-lang=ko")     // 명령 뒤에 있어도 됨
	check("en", "klingon")                 // 잘못된 환경 변수는 경고 후 기본값
	if _, err := setupLanguage([]string{"--lang", "klingon"}); err == nil {
		t.Error("unknown --lang should fail")
	}
	if code := run([]string{"--lang", "klingon", "help"}); code != exitUsage {
		t.Errorf("unknown --lang: exit %d", code)
	}
}

func TestCommandsHaveText(t *testing.T) {
	for _, l := range i18n.Languages() {
		if err := i18n.SetLanguage(l.Code); err != nil {
			t.Fatal(err)
		}
		for i := range commands {
			c := &commands[i]
			for _, s := range []string{c.summary(), c.usage(), c.details()} {
				if s == "" || strings.HasPrefix(s, "cmd.") {
					t.Errorf("%s/%s: missing command text %q", l.Code, c.name, s)
				}
			}
		}
	}
	_ = i18n.SetLanguage("en")
}

var hangul = regexp.MustCompile(`[\x{AC00}-\x{D7A3}]`)

// captureOutput runs fn while redirecting os.Stdout and os.Stderr, and returns what was written.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan string)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	fn()
	os.Stdout, os.Stderr = oldOut, oldErr
	w.Close()
	return <-done
}

func TestDefaultOutputIsEnglish(t *testing.T) {
	t.Setenv(langEnv, "")
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writeTestPNG(t, p)
	cmds := [][]string{
		{"help"}, {"help", "reduce"}, {"rd", "-h"}, {"red", "-n", p}, {"rd", p}, {"rd", p}, // 같은 명령을 두 번 실행해도 영어 출력
		{"nope"}, {"redcue"}, {"re"}, {"simplify", "-n", p}, {"rd", "-k", "-1"}, {"pack"}, {"rd", filepath.Join(dir, "missing.png")},
	}
	for _, args := range cmds {
		out := captureOutput(t, func() { run(args) })
		if out == "" || hangul.MatchString(out) {
			t.Errorf("%v: expected non-empty English output, got:\n%s", args, out)
		}
	}
	// 같은 명령이 한국어로는 한글을 출력합니다.
	out := captureOutput(t, func() { run([]string{"help", "--lang", "ko"}) })
	if !hangul.MatchString(out) || !strings.Contains(out, "공통 동작:") {
		t.Errorf("--lang ko help: %s", out)
	}
	t.Setenv(langEnv, "ko")
	out = captureOutput(t, func() { run([]string{"nope"}) })
	if !strings.Contains(out, "알 수 없는 명령: nope") {
		t.Errorf("env ko: %s", out)
	}
	_ = i18n.SetLanguage("en")
}
