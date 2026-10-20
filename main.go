package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/processor"
)

const (
	exitOK      = 0
	exitFailed  = 1
	exitUsage   = 2
	programName = "sprite-tools"
)

type command struct {
	name    string
	aliases []string // 짧은 별칭
	legacy  []string // 이전 이름 (호환용, 도움말에는 표시하지 않음)
	summary string
	usage   string // 인자 형식
	details string // 설명 및 예시
	run     func(args []string) int
}

var commands []command

func init() {
	commands = []command{
		{"reduce", []string{"rd"}, []string{"simplify"}, "비슷한 색상을 합쳐 색상 수를 줄입니다 (OKLab 기준)", "[경로...] [옵션]",
			`많이 쓰인 색상부터 대표 색상으로 잡고, 거리가 -t 미만인 색상을 대표 색상으로 합칩니다.
-k 로 최대 색상 수를 주면 그 수 이하가 되는 가장 약한 기준을 자동으로 찾습니다(-t 무시).

예시:
  sprite-tools reduce                        현재 폴더의 *.png → *-reduced.png
  sprite-tools rd hero.png -t 0.08           더 과감하게 합치기
  sprite-tools rd hero.png -k 16             색상을 16개 이하로 줄이기
  sprite-tools rd assets -r -w               assets 하위 전체를 원본에 덮어쓰기`, runReduce},
		{"recolor", []string{"rc"}, []string{"palette-convert"}, "Lospec 팔레트(또는 PNG 팔레트)의 색으로 바꿉니다", "-p <팔레트> [경로...] [옵션]",
			`-p 에는 Lospec 팔레트 이름(예: otterisk-96) 또는 팔레트 PNG 파일 경로를 줍니다.
받은 팔레트는 사용자 폴더(%APPDATA%\sprite-tools\palettes)에 저장되어 GUI와 함께 오프라인으로 쓸 수 있습니다.

예시:
  sprite-tools recolor -p otterisk-96
  sprite-tools rc -p my-palette.png hero.png
  sprite-tools rc -list`, runRecolor},
		{"index", []string{"ix"}, []string{"palette"}, "인덱스(팔레트) PNG로 변환하고 0번 색을 투명(#ffffff00)으로 맞춥니다", "[경로...] [옵션]",
			`색상 순서는 이미지에 처음 등장한 순서이며, 255색을 넘으면 실패합니다.

예시:
  sprite-tools index -w
  sprite-tools ix sprites -r`, runIndex},
		{"extract", []string{"ex"}, nil, "이미지의 색상을 팔레트 PNG(색 1개 = 1px, 한 줄)로 추출합니다", "[경로...] [옵션]",
			`많이 쓰인 색상 순서(동률이면 먼저 등장한 순서)로 저장하며, 완전 투명한 픽셀은 무시하고 알파는 255로 맞춥니다.
256색을 넘으면 실패하므로 먼저 reduce 로 줄이세요. 기본 출력은 <이름>-palette.png 입니다.
-save <이름> 을 주면 공용 팔레트 폴더에도 저장되어 'recolor -p <이름>' 으로 바로 쓸 수 있습니다
(같은 이름이 있으면 -force 가 있어야 덮어씁니다. 파일이 하나일 때만 사용 가능).

예시:
  sprite-tools extract hero.png
  sprite-tools ex hero.png -save hero-colors
  sprite-tools rc -p hero-colors enemy.png`, runExtract},
		{"downscale", []string{"ds"}, nil, "확대된 픽셀 아트를 1:1 크기로 줄입니다", "[경로...] [옵션]",
			`같은 색이 이어지는 길이로 확대 배율을 자동 감지합니다. -scale 로 직접 지정할 수 있습니다.

예시:
  sprite-tools downscale big.png
  sprite-tools ds -scale 4 -w`, runDownscale},
		{"upscale", []string{"us"}, nil, "정수 배율로 최근접 확대합니다 (2~32배)", "[경로...] [옵션]",
			`보간 없이 픽셀을 그대로 키웁니다. -scale 로 배율을 지정합니다(기본 2).

예시:
  sprite-tools upscale hero.png -scale 4
  sprite-tools us sprites -r -scale 8 -o big`, runUpscale},
		{"trim", []string{"tr"}, nil, "투명한 바깥 여백을 잘라냅니다", "[경로...] [옵션]",
			`투명하지 않은 픽셀을 모두 감싸는 최소 사각형으로 자르고, -pad 로 사방에 투명 여백을 더합니다.
완전히 투명한 이미지와 바뀌는 것이 없는 이미지는 건너뜁니다.

예시:
  sprite-tools trim icon.png
  sprite-tools tr sprites -r -pad 1 -w`, runTrim},
		{"slice", []string{"sl"}, []string{"split"}, "스프라이트 시트를 프레임 단위로 자릅니다", "[경로...] [옵션]",
			`<이름>-0001.png, <이름>-0002.png ... 로 저장합니다. 완전히 투명한 프레임은 건너뜁니다.

예시:
  sprite-tools slice sheet.png -size 32
  sprite-tools sl sheet.png -size 16x24 -o frames`, runSlice},
		{"pack", []string{"pk"}, nil, "여러 이미지를 한 장의 스프라이트 시트로 합칩니다 (slice 의 반대)", "-o <시트.png> [경로...] [옵션]",
			`입력을 자연 정렬 순서(frame-2 가 frame-10 보다 앞)로 모두 합쳐 하나의 PNG 로 저장합니다.
칸 크기는 가장 큰 너비 x 가장 큰 높이이고, 작은 이미지는 칸의 왼쪽 위(-align center 면 가운데)에 놓입니다.
-cols 0 이면 개수의 제곱근을 올림한 열 수를 씁니다. 입력 파일은 덮어쓸 수 없고, 이미 있는 출력 파일은 -force 가 있어야 덮어씁니다.

예시:
  sprite-tools pack frames -o sheet.png
  sprite-tools pk frames -cols 4 -gap 1 -align center -o sheet.png`, runPack},
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printHelp()
		return exitUsage
	}
	name := args[0]
	switch name {
	case "-h", "--help", "help", "h":
		if len(args) > 1 {
			if cmd, _ := resolveCommand(args[1]); cmd != nil {
				return cmd.run([]string{"-h"})
			}
		}
		printHelp()
		return exitOK
	}
	cmd, candidates := resolveCommand(name)
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "알 수 없는 명령: %s\n", name)
		if len(candidates) > 1 {
			fmt.Fprintf(os.Stderr, "여러 명령과 겹칩니다: %s\n", strings.Join(candidates, ", "))
		} else if s := suggest(name); s != "" {
			fmt.Fprintf(os.Stderr, "혹시 '%s' 를 찾으셨나요?\n", s)
		}
		fmt.Fprintf(os.Stderr, "'%s help' 로 명령 목록을 확인하세요.\n", programName)
		return exitUsage
	}
	if slices.Contains(cmd.legacy, strings.ToLower(name)) {
		fmt.Fprintf(os.Stderr, "참고: '%s' 는 '%s' 로 이름이 바뀌었습니다.\n", name, cmd.name)
	}
	return cmd.run(args[1:])
}

// resolveCommand: 정식 이름, 별칭, 이전 이름, 겹치지 않는 앞부분(예: "red")으로 명령을 찾습니다.
// 찾지 못했고 앞부분이 여러 명령과 겹치면 후보 목록을 함께 반환합니다.
func resolveCommand(name string) (*command, []string) {
	name = strings.ToLower(name)
	if name == "" {
		return nil, nil
	}
	for i := range commands {
		c := &commands[i]
		if c.name == name || slices.Contains(c.aliases, name) || slices.Contains(c.legacy, name) {
			return c, nil
		}
	}
	var matches []*command
	var names []string
	for i := range commands {
		if strings.HasPrefix(commands[i].name, name) {
			matches = append(matches, &commands[i])
			names = append(names, commands[i].name)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return nil, names
}

// suggest: 오타로 보이는 명령에 대해 가장 비슷한 명령을 추천합니다.
func suggest(name string) string {
	best, bestDist := "", 3
	for _, c := range commands {
		for _, n := range append([]string{c.name}, c.legacy...) {
			if d := levenshtein(name, n); d < bestDist {
				best, bestDist = c.name, d
			}
		}
	}
	return best
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func printHelp() {
	fmt.Println("Sprite Tools - 픽셀 아트 명령줄 도구")
	fmt.Println()
	fmt.Printf("사용법:\n  %s <명령> [경로...] [옵션]\n\n", programName)
	fmt.Println("명령:       별칭")
	for _, c := range commands {
		fmt.Printf("  %-10s %-5s %s\n", c.name, strings.Join(c.aliases, ","), c.summary)
	}
	fmt.Println()
	fmt.Println("공통 동작:")
	fmt.Println("  - 경로를 생략하면 현재 폴더의 PNG를 처리합니다. 파일과 폴더를 여러 개 줄 수 있습니다.")
	fmt.Println("  - 기본적으로 원본은 그대로 두고 접미사(-s)가 붙은 새 파일로 저장합니다. 덮어쓰려면 -w.")
	fmt.Println("  - -o <폴더> 로 다른 폴더에 저장할 수 있습니다 (하위 폴더 구조 유지, -w 와는 함께 쓸 수 없음).")
	fmt.Println("  - -n 으로 실제 처리 없이 대상 파일만 미리 볼 수 있습니다.")
	fmt.Println("  - 명령은 별칭이나 겹치지 않는 앞부분으로 줄여 쓸 수 있습니다 (예: rd, red, d).")
	fmt.Println()
	fmt.Printf("자세한 옵션: %s help <명령>\n", programName)
}

func newFlagSet(name string) (*flag.FlagSet, *command) {
	cmd, _ := resolveCommand(name)
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintf(out, "%s\n\n사용법:\n  %s %s %s\n", cmd.summary, programName, name, cmd.usage)
		if len(cmd.aliases) > 0 {
			fmt.Fprintf(out, "  (별칭: %s)\n", strings.Join(cmd.aliases, ", "))
		}
		fmt.Fprintf(out, "\n옵션:\n")
		fs.PrintDefaults()
		fmt.Fprintf(out, "\n%s\n", cmd.details)
	}
	return fs, cmd
}

// batchFlags: 여러 파일을 처리하는 명령에 공통으로 쓰이는 옵션입니다.
type batchFlags struct {
	pattern   *string
	suffix    *string
	inPlace   *bool
	recursive *bool
	threads   *int
	dryRun    *bool
	outDir    *string
}

// outputMode: 명령이 결과 파일을 만드는 방식입니다.
type outputMode int

const (
	modeNoOutput        outputMode = iota // 작업이 출력 경로를 직접 결정함 (slice, pack)
	modeOutput                            // 접미사/-w/-o 모두 사용
	modeOutputNoInPlace                   // 원본을 덮어쓸 수 없음 (extract)
)

func addBatchFlags(fs *flag.FlagSet, defaultSuffix string, mode outputMode) *batchFlags {
	b := &batchFlags{
		pattern:   fs.String("i", "*.png", "폴더 탐색 시 파일 이름 패턴"),
		recursive: fs.Bool("r", false, "하위 폴더까지 탐색"),
		threads:   fs.Int("j", 0, "동시 처리 스레드 수 (0 = CPU 코어 수)"),
		dryRun:    fs.Bool("n", false, "실제로 처리하지 않고 대상 파일만 표시"),
	}
	if mode != modeNoOutput {
		b.suffix = fs.String("s", defaultSuffix, "출력 파일 접미사 (-o 와 함께 쓸 때는 비워도 됨)")
		b.outDir = fs.String("o", "", "결과를 저장할 폴더 (하위 폴더 구조 유지, 없으면 만듦)")
		if mode == modeOutput {
			b.inPlace = fs.Bool("w", false, "새 파일 대신 원본을 덮어쓰기")
		}
	}
	return b
}

func (b *batchFlags) options(roots []string) common.Options {
	o := common.Options{
		Roots:     roots,
		Pattern:   *b.pattern,
		Recursive: *b.recursive,
		Threads:   *b.threads,
		NoOutput:  b.suffix == nil,
	}
	if b.suffix != nil {
		o.Suffix = *b.suffix
		o.OutDir = *b.outDir
		if b.inPlace != nil {
			o.InPlace = *b.inPlace
		}
	}
	// 캐시된 팔레트 이미지가 작업 대상에 섞이지 않도록 제외합니다.
	o.ExcludeDirs = append(o.ExcludeDirs, processor.PaletteDirs()...)
	return o
}

// parseArgs: 플래그와 경로를 순서에 상관없이 섞어 쓸 수 있도록 파싱합니다.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func parseOrExit(fs *flag.FlagSet, args []string) ([]string, int, bool) {
	roots, err := parseArgs(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return nil, exitOK, false
	}
	if err != nil {
		return nil, exitUsage, false
	}
	return roots, 0, true
}

func execute(b *batchFlags, roots []string, task common.Task) int {
	o := b.options(roots)
	if err := o.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		return exitUsage
	}
	files, err := common.CollectInputs(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		return exitUsage
	}
	if len(files) == 0 {
		fmt.Printf("처리할 파일이 없습니다 (패턴: %s)\n", o.Pattern)
		return exitOK
	}

	if *b.dryRun {
		fmt.Printf("[미리보기] %d개 파일이 처리됩니다. 실제로는 아무것도 쓰지 않습니다.\n", len(files))
		failed := 0
		for i, plan := range o.PlanOutputs(files) {
			f, out := files[i].Path, plan.Output
			switch {
			case plan.Err != nil:
				failed++
				fmt.Printf("  %s -> %s  (실패 예정: %v)\n", f, out, plan.Err)
			case out == "":
				fmt.Printf("  %s\n", f)
			case out == f:
				fmt.Printf("  %s (덮어쓰기)\n", f)
			default:
				fmt.Printf("  %s -> %s\n", f, out)
			}
		}
		if failed > 0 {
			return exitFailed
		}
		return exitOK
	}

	threads := o.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
	}
	fmt.Printf("%d개 파일 처리 시작 (스레드 %d개)\n", len(files), min(threads, len(files)))

	labels := map[common.Status]string{
		common.StatusDone:    "완료  ",
		common.StatusSkipped: "건너뜀",
		common.StatusFailed:  "실패  ",
	}
	width := len(fmt.Sprint(len(files)))
	sum := common.RunInputs(context.Background(), files, o, task, func(r common.Result) {
		target := r.Input
		if r.Output != "" && r.Output != r.Input {
			if o.OutDir != "" {
				target += " -> " + r.Output
			} else {
				target += " -> " + filepath.Base(r.Output)
			}
		}
		msg := ""
		if r.Message != "" {
			msg = "  (" + r.Message + ")"
		}
		fmt.Printf("[%*d/%d] %s %s%s\n", width, r.Completed, r.Total, labels[r.Status], target, msg)
	})

	fmt.Printf("\n완료 %d · 건너뜀 %d · 실패 %d  (%.1fs)\n", sum.Done, sum.Skipped, sum.Failed, sum.Elapsed.Seconds())
	if sum.Failed > 0 {
		return exitFailed
	}
	return exitOK
}

func usageError(fs *flag.FlagSet, format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "오류: "+format+"\n\n", a...)
	fs.Usage()
	return exitUsage
}

func runReduce(args []string) int {
	fs, _ := newFlagSet("reduce")
	threshold := fs.Float64("t", 0.05, "색상 병합 기준 거리 (클수록 더 많이 합침, 권장 0.02~0.15)")
	maxColors := fs.Int("k", 0, "최대 색상 수 (0 = 사용 안 함, 지정하면 -t 대신 이 수 이하가 되도록 자동 조절)")
	b := addBatchFlags(fs, "-reduced", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *maxColors < 0 {
		return usageError(fs, "-k 는 0 이상이어야 합니다")
	}
	if *maxColors > 0 {
		return execute(b, roots, func(in, out string) (string, error) {
			return processor.ReduceMax(in, out, *maxColors)
		})
	}
	if *threshold <= 0 {
		return usageError(fs, "-t 는 0보다 커야 합니다")
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Reduce(in, out, *threshold)
	})
}

func runDownscale(args []string) int {
	fs, _ := newFlagSet("downscale")
	scale := fs.Int("scale", 0, "축소 배율 직접 지정 (0 = 자동 감지)")
	b := addBatchFlags(fs, "-downscaled", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *scale < 0 || *scale == 1 {
		return usageError(fs, "-scale 은 2 이상이거나 0(자동)이어야 합니다")
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Downscale(in, out, *scale)
	})
}

func runIndex(args []string) int {
	fs, _ := newFlagSet("index")
	b := addBatchFlags(fs, "-indexed", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	return execute(b, roots, processor.Index)
}

// parseSize: "16" 또는 "16x24" 형식의 프레임 크기를 해석합니다.
func parseSize(s string) (int, int, error) {
	parts := strings.Split(strings.ToLower(s), "x")
	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("형식이 잘못됨")
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("형식이 잘못됨")
	}
	h := w
	if len(parts) == 2 {
		if h, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
			return 0, 0, fmt.Errorf("형식이 잘못됨")
		}
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("0보다 커야 함")
	}
	return w, h, nil
}

func runSlice(args []string) int {
	fs, _ := newFlagSet("slice")
	size := fs.String("size", "16", "프레임 크기 (예: 16 또는 16x24)")
	outDir := fs.String("o", "", "프레임을 저장할 폴더 (기본: 원본과 같은 폴더)")
	keepEmpty := fs.Bool("keep-empty", false, "완전히 투명한 프레임도 저장")
	b := addBatchFlags(fs, "", modeNoOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	w, h, err := parseSize(*size)
	if err != nil {
		return usageError(fs, "-size '%s': %v", *size, err)
	}
	return execute(b, roots, func(in, _ string) (string, error) {
		return processor.SliceFile(in, w, h, *outDir, *keepEmpty)
	})
}

func runRecolor(args []string) int {
	fs, _ := newFlagSet("recolor")
	paletteSpec := fs.String("p", "", "Lospec 팔레트 이름 또는 팔레트 PNG 경로 (예: otterisk-96)")
	list := fs.Bool("list", false, "다운로드된 로컬 팔레트 목록 표시")
	b := addBatchFlags(fs, "-recolored", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}

	if *list {
		palettes, err := processor.ListLocalPalettes()
		if err != nil {
			fmt.Fprintf(os.Stderr, "팔레트 목록 조회 실패: %v\n", err)
			return exitFailed
		}
		fmt.Printf("로컬 팔레트 (%s)\n", strings.Join(processor.PaletteDirs(), ", "))
		if len(palettes) == 0 {
			fmt.Println("  (없음) -p <이름> 으로 처음 사용할 때 자동으로 다운로드됩니다.")
		}
		for _, p := range palettes {
			fmt.Printf("  - %s\n", p)
		}
		return exitOK
	}

	if *paletteSpec == "" {
		return usageError(fs, "팔레트(-p)를 지정해야 합니다. 예: -p otterisk-96")
	}

	palettePath, err := processor.ResolvePalette(*paletteSpec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		return exitFailed
	}
	rawColors, labColors, err := processor.LoadPaletteColors(palettePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		return exitFailed
	}
	fmt.Printf("팔레트 %s (%d색)\n", palettePath, len(rawColors))

	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Recolor(in, out, rawColors, labColors)
	})
}
