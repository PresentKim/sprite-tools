package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/processor"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "simplify":
		cmd := flag.NewFlagSet("simplify", flag.ExitOnError)
		pattern := cmd.String("i", "*.png", "Input pattern")
		threshold := cmd.Float64("t", 0.05, "Color threshold for OKLab (default: 0.05)")
		suffix := cmd.String("s", "", "Output suffix")
		overwrite := cmd.Bool("w", true, "Overwrite existing files")
		threads := cmd.Int("j", 0, "Concurrency threads")
		cmd.Parse(os.Args[2:])

		common.RunParallel(*pattern, *suffix, *overwrite, *threads, func(in, out string) error {
			return processor.Simplify(in, out, *threshold)
		})

	case "split":
		cmd := flag.NewFlagSet("split", flag.ExitOnError)
		pattern := cmd.String("i", "*.png", "Input pattern")
		size := cmd.Int("size", 16, "Frame size")
		cmd.Parse(os.Args[2:])

		processor.SplitAll(*pattern, *size)

	case "downscale":
		cmd := flag.NewFlagSet("downscale", flag.ExitOnError)
		pattern := cmd.String("i", "*.png", "Input pattern")
		suffix := cmd.String("s", "", "Output suffix")
		overwrite := cmd.Bool("w", true, "Overwrite existing files")
		threads := cmd.Int("j", 0, "Concurrency threads")
		cmd.Parse(os.Args[2:])

		common.RunParallel(*pattern, *suffix, *overwrite, *threads, func(in, out string) error {
			return processor.Downscale(in, out)
		})

	case "palette":
		cmd := flag.NewFlagSet("palette", flag.ExitOnError)
		pattern := cmd.String("i", "*.png", "Input pattern")
		suffix := cmd.String("s", "", "Output suffix")
		overwrite := cmd.Bool("w", true, "Overwrite existing files")
		threads := cmd.Int("j", 0, "Concurrency threads")
		cmd.Parse(os.Args[2:])

		common.RunParallel(*pattern, *suffix, *overwrite, *threads, func(in, out string) error {
			return processor.FixPalette(in, out)
		})

	case "palette-convert":
		cmd := flag.NewFlagSet("palette-convert", flag.ExitOnError)
		paletteName := cmd.String("p", "", "Palette name from Lospec (e.g. otterisk-96)")
		listCmd := cmd.Bool("list", false, "List downloaded local palettes")
		pattern := cmd.String("i", "*.png", "Input pattern")
		suffix := cmd.String("s", "-recolored", "Output suffix")
		overwrite := cmd.Bool("w", false, "Overwrite existing files")
		threads := cmd.Int("j", 0, "Concurrency threads")
		cmd.Parse(os.Args[2:])

		// 1. 로컬 저장된 팔레트 목록 조회 기능
		if *listCmd {
			palettes, err := processor.ListLocalPalettes()
			if err != nil {
				fmt.Printf("팔레트 목록 조회 실패: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("=== 다운로드된 로컬 팔레트 목록 ===")
			if len(palettes) == 0 {
				fmt.Println("저장된 팔레트가 없습니다. -p 옵션으로 팔레트를 지정하여 다운로드하세요.")
			} else {
				for _, p := range palettes {
					fmt.Printf("- %s\n", p)
				}
			}
			return
		}

		if *paletteName == "" {
			fmt.Println("[오류] 팔레트 이름(-p)을 입력해야 합니다. 예: -p otterisk-96")
			cmd.Usage()
			os.Exit(1)
		}

		// 2. 팔레트 다운로드 또는 로컬 로드
		palettePath, err := processor.DownloadPalette(*paletteName)
		if err != nil {
			fmt.Printf("[ERROR] %v\n", err)
			os.Exit(1)
		}

		// 3. 팔레트 색상 데이터 추출
		rawColors, labColors, err := processor.LoadPaletteColors(palettePath)
		if err != nil {
			fmt.Printf("[ERROR] %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("팔레트 '%s' (색상 %d개) 기반으로 변환을 시작합니다.\n", filepath.Base(palettePath), len(rawColors))

		// 4. 이미지 변환 실행
		common.RunParallel(*pattern, *suffix, *overwrite, *threads, func(in, out string) error {
			return processor.ConvertToPalette(in, out, rawColors, labColors)
		})

	default:
		printHelp()
	}
}

func printHelp() {
	fmt.Println("Sprite Tools - Pixel Art Command Line Utility")
	fmt.Println("\nUsage:")
	fmt.Println("  sprite-tools <command> [flags]")
	fmt.Println("\nAvailable Commands:")
	fmt.Println("  simplify   Reduce color complexity using DeltaE 2000 algorithm")
	fmt.Println("  split      Split sprite sheet into individual frames")
	fmt.Println("  downscale  Auto-detect pixel scale and downscale to 1:1")
	fmt.Println("  palette    Re-index palette setting index 0 to #ffffff00")
	fmt.Println("  palette-convert Convert sprite colors to a target Lospec palette")
	fmt.Println("\nRun 'sprite-tools <command> -h' for more information about a command.")
}