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
	"sprite-tools/pkg/i18n"
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
	aliases []string // short aliases
	legacy  []string // former names (kept for compatibility, not shown in help)
	run     func(args []string) int
}

// Command descriptions live in the i18n catalog (cmd.<name>.summary / usage / details) and are read in the language active at call time.
func (c *command) summary() string { return i18n.T("cmd." + c.name + ".summary") } // one-line description
func (c *command) usage() string   { return i18n.T("cmd." + c.name + ".usage") }   // argument format
func (c *command) details() string { return i18n.T("cmd." + c.name + ".details") } // description and examples

var commands []command

func init() {
	commands = []command{
		{"reduce", []string{"rd"}, []string{"simplify"}, runReduce},
		{"recolor", []string{"rc"}, []string{"palette-convert"}, runRecolor},
		{"index", []string{"ix"}, []string{"palette"}, runIndex},
		{"extract", []string{"ex"}, nil, runExtract},
		{"downscale", []string{"ds"}, nil, runDownscale},
		{"upscale", []string{"us"}, nil, runUpscale},
		{"trim", []string{"tr"}, nil, runTrim},
		{"despeckle", []string{"dsp"}, nil, runDespeckle},
		{"slice", []string{"sl"}, []string{"split"}, runSlice},
		{"pack", []string{"pk"}, nil, runPack},
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// langEnv is the environment variable that selects the display language (--lang takes precedence).
const langEnv = "SPRITE_TOOLS_LANG"

// langList is a string of the available language codes, such as "en, ko". Only codes are shown so that native language names do not mix Hangul into English output.
func langList() string {
	var codes []string
	for _, l := range i18n.Languages() {
		codes = append(codes, l.Code)
	}
	return strings.Join(codes, ", ")
}

// extractLang removes --lang <code> / -lang <code> / --lang=<code> from the arguments wherever it appears around the command name. Anything after "--" is left alone.
func extractLang(args []string) (rest []string, lang string, given bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name, value, hasValue := strings.Cut(a, "=")
		if name != "-lang" && name != "--lang" {
			rest = append(rest, a)
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return nil, "", false, i18n.Errorf("cli.lang.missing", langList())
			}
			i++
			value = args[i]
		}
		lang, given = value, true
	}
	return rest, lang, given, nil
}

// setupLanguage picks the language in the order --lang > SPRITE_TOOLS_LANG > default (en) and returns the arguments without the flag.
// Flag help text is fixed when the flags are created, so this must be called before building the flag set.
func setupLanguage(args []string) ([]string, error) {
	_ = i18n.SetLanguage(i18n.DefaultLanguage)
	rest, lang, given, err := extractLang(args)
	if err != nil {
		return nil, err
	}
	if given {
		if err := i18n.SetLanguage(lang); err != nil {
			return nil, i18n.Errorf("cli.lang.invalid", lang, langList())
		}
	} else if env := strings.TrimSpace(os.Getenv(langEnv)); env != "" {
		if err := i18n.SetLanguage(env); err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("cli.lang.env_invalid", env, langList()))
		}
	}
	return rest, nil
}

func printError(err error) {
	fmt.Fprintln(os.Stderr, i18n.T("cli.error", err))
}

func run(args []string) int {
	args, err := setupLanguage(args)
	if err != nil {
		printError(err)
		return exitUsage
	}
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
		fmt.Fprintln(os.Stderr, i18n.T("cli.unknown_command", name))
		if len(candidates) > 1 {
			fmt.Fprintln(os.Stderr, i18n.T("cli.ambiguous", strings.Join(candidates, ", ")))
		} else if s := suggest(name); s != "" {
			fmt.Fprintln(os.Stderr, i18n.T("cli.suggest", s))
		}
		fmt.Fprintln(os.Stderr, i18n.T("cli.see_help", programName))
		return exitUsage
	}
	if slices.Contains(cmd.legacy, strings.ToLower(name)) {
		fmt.Fprintln(os.Stderr, i18n.T("cli.renamed", name, cmd.name))
	}
	return cmd.run(args[1:])
}

// resolveCommand finds a command by canonical name, alias, former name, or a non-ambiguous prefix (e.g. "red").
// If nothing is found and the prefix matches several commands, the candidates are returned as well.
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

// suggest recommends the most similar command for a likely typo.
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
	fmt.Println(i18n.T("cli.help.title"))
	fmt.Println()
	fmt.Println(i18n.T("cli.usage"))
	fmt.Println("  " + i18n.T("cli.help.usage_line", programName))
	fmt.Println()
	fmt.Println(i18n.T("cli.help.commands"))
	for _, c := range commands {
		fmt.Printf("  %-10s %-5s %s\n", c.name, strings.Join(c.aliases, ","), c.summary())
	}
	fmt.Println()
	fmt.Println(i18n.T("cli.help.common_title"))
	fmt.Println("  - " + i18n.T("cli.help.common.paths"))
	fmt.Println("  - " + i18n.T("cli.help.common.suffix"))
	fmt.Println("  - " + i18n.T("cli.help.common.backup", common.BackupDirName))
	fmt.Println("  - " + i18n.T("cli.help.common.outdir"))
	fmt.Println("  - " + i18n.T("cli.help.common.dryrun"))
	fmt.Println("  - " + i18n.T("cli.help.common.abbrev"))
	fmt.Println("  - " + i18n.T("cli.help.common.lang", langList()))
	fmt.Println()
	fmt.Println(i18n.T("cli.help.more", programName))
}

func newFlagSet(name string) (*flag.FlagSet, *command) {
	cmd, _ := resolveCommand(name)
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintf(out, "%s\n\n%s\n  %s %s %s\n", cmd.summary(), i18n.T("cli.usage"), programName, name, cmd.usage())
		if len(cmd.aliases) > 0 {
			fmt.Fprintf(out, "  %s\n", i18n.T("cli.cmd.aliases", strings.Join(cmd.aliases, ", ")))
		}
		fmt.Fprintf(out, "\n%s\n", i18n.T("cli.cmd.options"))
		fs.PrintDefaults()
		fmt.Fprintf(out, "\n%s\n", cmd.details())
	}
	return fs, cmd
}

// batchFlags are the options shared by commands that process multiple files.
type batchFlags struct {
	pattern   *string
	suffix    *string
	inPlace   *bool
	recursive *bool
	threads   *int
	dryRun    *bool
	outDir    *string
	backup    *bool
}

// outputMode is how a command produces its result files.
type outputMode int

const (
	modeNoOutput        outputMode = iota // the job decides its own output paths (slice, pack)
	modeOutput                            // suffix/-w/-o all apply
	modeOutputNoInPlace                   // the original cannot be overwritten (extract)
)

func addBatchFlags(fs *flag.FlagSet, defaultSuffix string, mode outputMode) *batchFlags {
	b := &batchFlags{
		pattern:   fs.String("i", "*.png", i18n.T("flag.pattern")),
		recursive: fs.Bool("r", false, i18n.T("flag.recursive")),
		threads:   fs.Int("j", 0, i18n.T("flag.threads")),
		dryRun:    fs.Bool("n", false, i18n.T("flag.dryrun")),
	}
	if mode != modeNoOutput {
		b.suffix = fs.String("s", defaultSuffix, i18n.T("flag.suffix"))
		b.outDir = fs.String("o", "", i18n.T("flag.outdir"))
		if mode == modeOutput {
			b.inPlace = fs.Bool("w", false, i18n.T("flag.inplace"))
			b.backup = fs.Bool("backup", false, i18n.T("flag.backup", common.BackupDirName))
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
			o.Backup = *b.backup
		}
	}
	// Exclude cached palette images so they are not mixed into the work targets.
	o.ExcludeDirs = append(o.ExcludeDirs, processor.PaletteDirs()...)
	return o
}

// parseArgs parses flags and paths so that they can be mixed in any order.
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
		printError(err)
		return exitUsage
	}
	files, err := common.CollectInputs(o)
	if err != nil {
		printError(err)
		return exitUsage
	}
	if len(files) == 0 {
		fmt.Println(i18n.T("cli.no_files", o.Pattern))
		return exitOK
	}

	if *b.dryRun {
		fmt.Println(i18n.T("cli.dryrun.header", len(files)))
		if o.Backup {
			fmt.Println(i18n.T("cli.dryrun.backup", common.BackupDirName))
		}
		failed := 0
		for i, plan := range o.PlanOutputs(files) {
			f, out := files[i].Path, plan.Output
			switch {
			case plan.Err != nil:
				failed++
				fmt.Println(i18n.T("cli.dryrun.will_fail", f, out, plan.Err))
			case out == "":
				fmt.Printf("  %s\n", f)
			case out == f:
				fmt.Println(i18n.T("cli.dryrun.overwrite", f))
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
	fmt.Println(i18n.T("cli.run.start", len(files), min(threads, len(files))))

	labels := map[common.Status]string{
		common.StatusDone:    i18n.T("cli.run.label.done"),
		common.StatusSkipped: i18n.T("cli.run.label.skipped"),
		common.StatusFailed:  i18n.T("cli.run.label.failed"),
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

	fmt.Println()
	fmt.Println(i18n.T("cli.run.summary", sum.Done, sum.Skipped, sum.Failed, sum.Elapsed.Seconds()))
	if sum.Backups > 0 {
		fmt.Println(i18n.T("cli.backup.summary", sum.Backups))
		for i, d := range sum.BackupDirs {
			if i == 5 {
				fmt.Println(i18n.T("cli.backup.more", len(sum.BackupDirs)-i))
				break
			}
			fmt.Printf("  %s\n", d)
		}
	}
	if sum.Failed > 0 {
		return exitFailed
	}
	return exitOK
}

// usageError shows the usage together with an error built from a catalog key (err.cli.*).
func usageError(fs *flag.FlagSet, key string, a ...any) int {
	fmt.Fprintf(os.Stderr, "%s\n\n", i18n.T("cli.error", i18n.T(key, a...)))
	fs.Usage()
	return exitUsage
}

func runReduce(args []string) int {
	fs, _ := newFlagSet("reduce")
	threshold := fs.Float64("t", 0.05, i18n.T("flag.threshold"))
	maxColors := fs.Int("k", 0, i18n.T("flag.max_colors"))
	b := addBatchFlags(fs, "-reduced", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *maxColors < 0 {
		return usageError(fs, "err.cli.k_negative")
	}
	if *maxColors > 0 {
		return execute(b, roots, func(in, out string) (string, error) {
			return processor.ReduceMax(in, out, *maxColors)
		})
	}
	if *threshold <= 0 {
		return usageError(fs, "err.cli.t_positive")
	}
	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Reduce(in, out, *threshold)
	})
}

func runDownscale(args []string) int {
	fs, _ := newFlagSet("downscale")
	scale := fs.Int("scale", 0, i18n.T("flag.scale_down"))
	b := addBatchFlags(fs, "-downscaled", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	if *scale < 0 || *scale == 1 {
		return usageError(fs, "err.cli.scale_down")
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

// parseSize parses a frame size in the form "16" or "16x24".
func parseSize(s string) (int, int, error) {
	parts := strings.Split(strings.ToLower(s), "x")
	if len(parts) > 2 {
		return 0, 0, i18n.Errorf("err.cli.size_format")
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, i18n.Errorf("err.cli.size_format")
	}
	h := w
	if len(parts) == 2 {
		if h, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
			return 0, 0, i18n.Errorf("err.cli.size_format")
		}
	}
	if w <= 0 || h <= 0 {
		return 0, 0, i18n.Errorf("err.cli.size_positive")
	}
	return w, h, nil
}

func runSlice(args []string) int {
	fs, _ := newFlagSet("slice")
	size := fs.String("size", "16", i18n.T("flag.frame_size"))
	outDir := fs.String("o", "", i18n.T("flag.slice_outdir"))
	keepEmpty := fs.Bool("keep-empty", false, i18n.T("flag.keep_empty"))
	b := addBatchFlags(fs, "", modeNoOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}
	w, h, err := parseSize(*size)
	if err != nil {
		return usageError(fs, "err.cli.size", *size, err)
	}
	return execute(b, roots, func(in, _ string) (string, error) {
		return processor.SliceFile(in, w, h, *outDir, *keepEmpty)
	})
}

func runRecolor(args []string) int {
	fs, _ := newFlagSet("recolor")
	paletteSpec := fs.String("p", "", i18n.T("flag.palette"))
	list := fs.Bool("list", false, i18n.T("flag.list"))
	b := addBatchFlags(fs, "-recolored", modeOutput)
	roots, code, ok := parseOrExit(fs, args)
	if !ok {
		return code
	}

	if *list {
		palettes, err := processor.ListLocalPalettes()
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("cli.palette.list_failed", err))
			return exitFailed
		}
		fmt.Println(i18n.T("cli.palette.local", strings.Join(processor.PaletteDirs(), ", ")))
		if len(palettes) == 0 {
			fmt.Println(i18n.T("cli.palette.none"))
		}
		for _, p := range palettes {
			fmt.Printf("  - %s\n", p)
		}
		return exitOK
	}

	if *paletteSpec == "" {
		return usageError(fs, "err.cli.palette_required")
	}

	palettePath, err := processor.ResolvePalette(*paletteSpec)
	if err != nil {
		printError(err)
		return exitFailed
	}
	rawColors, labColors, err := processor.LoadPaletteColors(palettePath)
	if err != nil {
		printError(err)
		return exitFailed
	}
	fmt.Println(i18n.T("cli.palette.using", palettePath, len(rawColors)))

	return execute(b, roots, func(in, out string) (string, error) {
		return processor.Recolor(in, out, rawColors, labColors)
	})
}
