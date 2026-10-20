package main

import (
	"fmt"
	"os"
	"strings"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/processor"
)

func runUpscale(args []string) int {
	fs, _ := newFlagSet("upscale")
	scale := fs.Int("scale", 2, fmt.Sprintf("확대 배율 (%d~%d)", processor.MinUpscale, processor.MaxUpscale))
	b := addBatchFlags(fs, "-upscaled", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *scale < processor.MinUpscale || *scale > processor.MaxUpscale {
		return usageError(fs, "-scale 은 %d~%d 사이여야 합니다", processor.MinUpscale, processor.MaxUpscale)
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Upscale(in, out, *scale)
	})
}

func runTrim(args []string) int {
	fs, _ := newFlagSet("trim")
	pad := fs.Int("pad", 0, "자른 뒤 사방에 더할 투명 여백 (px)")
	b := addBatchFlags(fs, "-trimmed", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *pad < 0 {
		return usageError(fs, "-pad 는 0 이상이어야 합니다")
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Trim(in, out, *pad)
	})
}

func runDespeckle(args []string) int {
	fs, _ := newFlagSet("despeckle")
	minSize := fs.Int("min", 4, "이보다 픽셀 수가 적은 조각을 지움 (1 이상)")
	diagonal := fs.Bool("diagonal", true, "대각선으로 닿은 픽셀도 같은 조각으로 봄 (끄려면 -diagonal=false)")
	b := addBatchFlags(fs, "-clean", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *minSize < 1 {
		return usageError(fs, "-min 은 1 이상이어야 합니다")
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Despeckle(in, out, *minSize, *diagonal)
	})
}

func runExtract(args []string) int {
	fs, _ := newFlagSet("extract")
	save := fs.String("save", "", "공용 팔레트 폴더에도 이 이름으로 저장 (recolor -p <이름> 으로 사용, 영문 소문자/숫자/'-')")
	force := fs.Bool("force", false, "-save 이름이 이미 있어도 덮어쓰기")
	b := addBatchFlags(fs, "-palette", modeOutputNoInPlace)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *save != "" {
		if _, err := processor.NormalizePaletteName(*save); err != nil {
			return usageError(fs, "-save: %v", err)
		}
		files, err := common.CollectFiles(b.options(roots))
		if err != nil {
			fmt.Fprintf(os.Stderr, "오류: %v\n", err)
			return exitUsage
		}
		if len(files) > 1 {
			return usageError(fs, "-save 는 파일이 하나일 때만 쓸 수 있습니다 (대상 %d개)", len(files))
		}
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Extract(in, out, *save, *force)
	})
}

func runPack(args []string) int {
	fs, _ := newFlagSet("pack")
	out := fs.String("o", "", "저장할 시트 PNG 파일 (필수)")
	cols := fs.Int("cols", 0, "열 수 (0 = 개수의 제곱근을 올림)")
	gap := fs.Int("gap", 0, "칸 사이의 투명 간격 (px)")
	align := fs.String("align", "topleft", "칸 안에서 작은 이미지의 위치: topleft | center")
	force := fs.Bool("force", false, "이미 있는 출력 파일을 덮어쓰기")
	pattern := fs.String("i", "*.png", "폴더 탐색 시 파일 이름 패턴")
	recursive := fs.Bool("r", false, "하위 폴더까지 탐색")
	dryRun := fs.Bool("n", false, "실제로 저장하지 않고 합칠 파일 순서만 표시")
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *out == "" {
		return usageError(fs, "출력 파일(-o)을 지정해야 합니다. 예: -o sheet.png")
	}
	if *cols < 0 || *gap < 0 {
		return usageError(fs, "-cols, -gap 은 0 이상이어야 합니다")
	}
	var center bool
	switch strings.ToLower(*align) {
	case "topleft", "top-left", "tl":
	case "center":
		center = true
	default:
		return usageError(fs, "-align '%s': topleft 또는 center 만 가능합니다", *align)
	}

	o := common.Options{Roots: roots, Pattern: *pattern, Recursive: *recursive, NoOutput: true,
		ExcludeDirs: processor.PaletteDirs()}
	inputs, err := common.CollectInputs(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		return exitUsage
	}
	// 폴더를 훑다가 이전에 만든 시트(출력 파일)가 잡히면 입력에서 뺍니다. 직접 지정한 파일이면 아래 검사에서 거부됩니다.
	var files []string
	for _, in := range inputs {
		if in.Root != "" && common.SamePath(in.Path, *out) {
			continue
		}
		files = append(files, in.Path)
	}
	if len(files) == 0 {
		fmt.Printf("합칠 파일이 없습니다 (패턴: %s)\n", *pattern)
		return exitOK
	}
	files = processor.SortedPaths(files)

	if err := processor.CheckPackOutput(files, *out, *force); err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		return exitFailed
	}
	if *dryRun {
		fmt.Printf("[미리보기] %d개 파일을 이 순서로 합쳐 %s 에 저장합니다. 실제로는 아무것도 쓰지 않습니다.\n", len(files), *out)
		for i, f := range files {
			fmt.Printf("  %3d. %s\n", i+1, f)
		}
		return exitOK
	}
	msg, err := processor.PackFiles(files, *out, *cols, *gap, center, *force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: %v\n", err)
		return exitFailed
	}
	fmt.Printf("%s  (%s)\n", msg, *out)
	return exitOK
}
