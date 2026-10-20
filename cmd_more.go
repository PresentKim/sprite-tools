package main

import (
	"fmt"
	"strings"

	"sprite-tools/pkg/common"
	"sprite-tools/pkg/i18n"
	"sprite-tools/pkg/processor"
)

func runUpscale(args []string) int {
	fs, _ := newFlagSet("upscale")
	scale := fs.Int("scale", 2, i18n.T("flag.scale_up", processor.MinUpscale, processor.MaxUpscale))
	b := addBatchFlags(fs, "-upscaled", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *scale < processor.MinUpscale || *scale > processor.MaxUpscale {
		return usageError(fs, "err.cli.scale_up", processor.MinUpscale, processor.MaxUpscale)
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Upscale(in, out, *scale)
	})
}

func runTrim(args []string) int {
	fs, _ := newFlagSet("trim")
	pad := fs.Int("pad", 0, i18n.T("flag.pad"))
	b := addBatchFlags(fs, "-trimmed", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *pad < 0 {
		return usageError(fs, "err.cli.pad_negative")
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Trim(in, out, *pad)
	})
}

func runDespeckle(args []string) int {
	fs, _ := newFlagSet("despeckle")
	minSize := fs.Int("min", 4, i18n.T("flag.min_size"))
	diagonal := fs.Bool("diagonal", true, i18n.T("flag.diagonal"))
	b := addBatchFlags(fs, "-clean", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *minSize < 1 {
		return usageError(fs, "err.cli.min_size")
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Despeckle(in, out, *minSize, *diagonal)
	})
}

func runExtract(args []string) int {
	fs, _ := newFlagSet("extract")
	save := fs.String("save", "", i18n.T("flag.save"))
	force := fs.Bool("force", false, i18n.T("flag.force_save"))
	b := addBatchFlags(fs, "-palette", modeOutputNoInPlace)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *save != "" {
		if _, err := processor.NormalizePaletteName(*save); err != nil {
			return usageError(fs, "err.cli.save_name", err)
		}
		files, err := common.CollectFiles(b.options(roots))
		if err != nil {
			printError(err)
			return exitUsage
		}
		if len(files) > 1 {
			return usageError(fs, "err.cli.save_single", len(files))
		}
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Extract(in, out, *save, *force)
	})
}

func runPack(args []string) int {
	fs, _ := newFlagSet("pack")
	out := fs.String("o", "", i18n.T("flag.pack_out"))
	cols := fs.Int("cols", 0, i18n.T("flag.cols"))
	gap := fs.Int("gap", 0, i18n.T("flag.gap"))
	align := fs.String("align", "topleft", i18n.T("flag.align"))
	force := fs.Bool("force", false, i18n.T("flag.force_pack"))
	pattern := fs.String("i", "*.png", i18n.T("flag.pattern"))
	recursive := fs.Bool("r", false, i18n.T("flag.recursive"))
	dryRun := fs.Bool("n", false, i18n.T("flag.pack_dryrun"))
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *out == "" {
		return usageError(fs, "err.cli.out_required")
	}
	if *cols < 0 || *gap < 0 {
		return usageError(fs, "err.cli.cols_gap")
	}
	var center bool
	switch strings.ToLower(*align) {
	case "topleft", "top-left", "tl":
	case "center":
		center = true
	default:
		return usageError(fs, "err.cli.align", *align)
	}

	o := common.Options{Roots: roots, Pattern: *pattern, Recursive: *recursive, NoOutput: true,
		ExcludeDirs: processor.PaletteDirs()}
	inputs, err := common.CollectInputs(o)
	if err != nil {
		printError(err)
		return exitUsage
	}
	// While walking a folder, drop previously generated sheets (output files) from the input. Explicitly given files are rejected by the check below.
	var files []string
	for _, in := range inputs {
		if in.Root != "" && common.SamePath(in.Path, *out) {
			continue
		}
		files = append(files, in.Path)
	}
	if len(files) == 0 {
		fmt.Println(i18n.T("cli.pack.no_files", *pattern))
		return exitOK
	}
	files = processor.SortedPaths(files)

	if err := processor.CheckPackOutput(files, *out, *force); err != nil {
		printError(err)
		return exitFailed
	}
	if *dryRun {
		fmt.Println(i18n.T("cli.pack.dryrun", len(files), *out))
		for i, f := range files {
			fmt.Printf("  %3d. %s\n", i+1, f)
		}
		return exitOK
	}
	msg, err := processor.PackFiles(files, *out, *cols, *gap, center, *force)
	if err != nil {
		printError(err)
		return exitFailed
	}
	fmt.Printf("%s  (%s)\n", msg, *out)
	return exitOK
}
