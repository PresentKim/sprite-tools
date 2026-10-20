package common

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"sprite-tools/pkg/i18n"
)

// Task processes one input file and returns a result summary message.
// When there is nothing to process, return an error made with Skip(...).
type Task func(inputPath, outputPath string) (string, error)

type skipError struct{ msg string }

func (e skipError) Error() string { return e.msg }

// Skip makes an error that is counted as "skipped" rather than a failure. key is a key in the i18n catalog (skip.*), and the message is fixed to the language active when it was created.
func Skip(key string, a ...any) error {
	return skipError{i18n.T(key, a...)}
}

func IsSkip(err error) bool {
	var s skipError
	return errors.As(err, &s)
}

type Options struct {
	Roots       []string // files or directories to process (".", if empty)
	Pattern     string   // file name pattern when walking directories (case-insensitive)
	Recursive   bool     // also walk subdirectories
	Suffix      string   // output file suffix (when not InPlace)
	InPlace     bool     // overwrite the original file directly
	OutDir      string   // if non-empty, save results in this folder (keeping paths relative to the root; cannot be combined with InPlace)
	NoOutput    bool     // the task decides its own output paths (slice, etc.)
	Threads     int
	ExcludeDirs []string // directories to exclude from the walk
	Backup      bool     // with InPlace, copy the original to <file folder>/BackupDirName/<run time>/ before overwriting
}

// BackupDirName is the name of the hidden folder that holds originals before they are overwritten. It starts with a dot, so directory walks skip it.
const BackupDirName = ".sprite-tools-backup"

// backupStampLayout is the format of the backup subfolder name, one per run.
const backupStampLayout = "20060102-150405"

// Input is one collected input file and the root folder it was found in.
// Root is empty if the file was given directly.
type Input struct {
	Path string
	Root string
}

type Status string

const (
	StatusDone    Status = "done"
	StatusSkipped Status = "skipped"
	StatusFailed  Status = "failed"
)

// Result is the processing result for one file.
type Result struct {
	Index     int    `json:"index"`     // position in the input list
	Completed int    `json:"completed"` // number of files finished so far
	Total     int    `json:"total"`
	Input     string `json:"input"`
	Output    string `json:"output"`
	Status    Status `json:"status"`
	Message   string `json:"message"`
	Backup    string `json:"backup,omitempty"` // path of the original copied before overwriting (only when backed up)
}

type Summary struct {
	Done       int           `json:"done"`
	Skipped    int           `json:"skipped"`
	Failed     int           `json:"failed"`
	Canceled   bool          `json:"canceled"`
	Elapsed    time.Duration `json:"elapsed"`
	Backups    int           `json:"backups"`    // number of backup files actually left
	BackupDirs []string      `json:"backupDirs"` // folders where backups were saved (sorted)
}

// OutputPath computes the output path for the options. The file is treated as one whose root is unknown (given directly).
func (o Options) OutputPath(in string) string {
	return o.OutputFor(Input{Path: in})
}

// OutputFor computes the output path of an input file.
// With OutDir, the path relative to the root is kept under OutDir (a directly given file goes right under OutDir);
// otherwise the suffix is appended next to the original.
func (o Options) OutputFor(in Input) string {
	if o.NoOutput {
		return ""
	}
	if o.InPlace {
		return in.Path
	}
	rel := in.Path
	if o.OutDir != "" {
		rel = filepath.Base(in.Path)
		if in.Root != "" {
			if r, err := filepath.Rel(in.Root, in.Path); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
	}
	ext := filepath.Ext(rel)
	rel = strings.TrimSuffix(rel, ext) + o.Suffix + ext
	if o.OutDir != "" {
		return filepath.Join(o.OutDir, rel)
	}
	return rel
}

// Validate checks that the output settings are valid.
// With an output folder (OutDir) the suffix may be empty, but files whose output would equal the original path fail at run time.
func (o Options) Validate() error {
	if o.InPlace && o.OutDir != "" {
		return i18n.Errorf("err.options.inplace_outdir")
	}
	if o.Backup && !o.InPlace {
		return i18n.Errorf("err.options.backup_needs_inplace")
	}
	if !o.InPlace && !o.NoOutput && o.Suffix == "" && o.OutDir == "" {
		return i18n.Errorf("err.options.empty_suffix")
	}
	return nil
}

// SamePath reports whether two paths point to the same file (case-insensitive).
func SamePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(aa, bb)
}

// Plan is the output plan for one input. If Err is set, this file is not processed.
type Plan struct {
	Output string
	Err    error
}

// PlanOutputs computes the output paths of all inputs and fills Err for files that would overwrite the original or whose output collides with another input.
// Overwriting (InPlace) is intended, so it is not checked.
func (o Options) PlanOutputs(inputs []Input) []Plan {
	plans := make([]Plan, len(inputs))
	claimed := make(map[string]int)
	for i, in := range inputs {
		out := o.OutputFor(in)
		plans[i].Output = out
		if out == "" || o.InPlace {
			continue
		}
		if SamePath(in.Path, out) {
			plans[i].Err = i18n.Errorf("err.plan.same_as_input")
			continue
		}
		abs, err := filepath.Abs(out)
		if err != nil {
			abs = out
		}
		key := strings.ToLower(abs)
		if prev, ok := claimed[key]; ok {
			plans[i].Err = i18n.Errorf("err.plan.collision", inputs[prev].Path)
			continue
		}
		claimed[key] = i
	}
	return plans
}

func (o Options) isOwnOutput(path string) bool {
	if o.InPlace || o.NoOutput || o.Suffix == "" {
		return false
	}
	name := filepath.Base(path)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return strings.HasSuffix(strings.ToLower(stem), strings.ToLower(o.Suffix))
}

// CollectFiles returns the list of input file paths matching the options, sorted.
func CollectFiles(o Options) ([]string, error) {
	inputs, err := CollectInputs(o)
	if err != nil {
		return nil, err
	}
	files := make([]string, len(inputs))
	for i, in := range inputs {
		files[i] = in.Path
	}
	return files, nil
}

// CollectInputs is like CollectFiles but also returns the root each file was found in (needed to keep relative paths in the output folder).
// OutDir is not walked, so earlier outputs are not fed back in as inputs.
func CollectInputs(o Options) ([]Input, error) {
	roots := o.Roots
	if len(roots) == 0 {
		roots = []string{"."}
	}
	pattern := strings.ToLower(o.Pattern)
	if pattern == "" {
		pattern = "*.png"
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, i18n.Errorf("err.path.bad_pattern", o.Pattern, err)
	}

	excludeDirs := o.ExcludeDirs
	if o.OutDir != "" && !o.InPlace && !o.NoOutput {
		excludeDirs = append(slices.Clone(excludeDirs), o.OutDir)
	}
	excluded := make([]string, 0, len(excludeDirs))
	for _, d := range excludeDirs {
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
	var files []Input
	add := func(path, root string) {
		key := strings.ToLower(filepath.Clean(path))
		if !seen[key] {
			seen[key] = true
			files = append(files, Input{Path: path, Root: root})
		}
	}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return nil, i18n.Errorf("err.path.not_found", root)
		}
		if !info.IsDir() {
			// Directly given files are processed regardless of the pattern.
			add(root, "")
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
			add(path, root)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(files, func(a, b Input) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// RunFiles processes file paths with unknown roots via RunInputs.
func RunFiles(ctx context.Context, files []string, o Options, task Task, report func(Result)) Summary {
	inputs := make([]Input, len(files))
	for i, f := range files {
		inputs[i] = Input{Path: f}
	}
	return RunInputs(ctx, inputs, o, task, report)
}

// RunInputs processes the given files in parallel. report is called once per finished file (one at a time, in order).
// Once ctx is canceled, files that have not started yet are not processed.
// With an output folder, the needed subfolders are created before work starts, and files whose output collides with the original or other outputs are treated as failures.
func RunInputs(ctx context.Context, files []Input, o Options, task Task, report func(Result)) Summary {
	var sum Summary
	plans := o.PlanOutputs(files)
	start := time.Now()
	stamp := start.Format(backupStampLayout) // all backups of one run go into the same timestamp folder

	threads := o.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
	}
	threads = max(1, min(threads, len(files)))

	jobs := make(chan int)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for w := 0; w < threads; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				in, out := files[i].Path, plans[i].Output
				var msg string
				err := plans[i].Err
				if err == nil && o.OutDir != "" && out != "" {
					err = os.MkdirAll(filepath.Dir(out), 0o755)
				}
				// Copy the original first, and delete the copy if the task did not actually finish (skipped or failed).
				var backup string
				if err == nil && o.Backup && o.InPlace && out != "" {
					backup, err = backupFile(in, stamp)
				}
				if err == nil {
					msg, err = runTask(task, in, out)
				}
				if err != nil && backup != "" {
					os.Remove(backup)
					backup = ""
				}

				r := Result{Index: i, Total: len(files), Input: in, Output: out, Message: msg, Backup: backup}
				switch {
				case err == nil:
					r.Status = StatusDone
				case IsSkip(err):
					r.Status = StatusSkipped
					r.Message = err.Error()
				default:
					r.Status = StatusFailed
					r.Message = err.Error()
				}

				mu.Lock()
				switch r.Status {
				case StatusDone:
					sum.Done++
				case StatusSkipped:
					sum.Skipped++
				default:
					sum.Failed++
				}
				if r.Backup != "" {
					sum.Backups++
				}
				r.Completed = sum.Done + sum.Skipped + sum.Failed
				if report != nil {
					report(r)
				}
				mu.Unlock()
			}
		}()
	}

feed:
	for i := range files {
		select {
		case <-ctx.Done():
			sum.Canceled = true
			break feed
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()

	if o.Backup && o.InPlace {
		sum.BackupDirs = finishBackupDirs(files, stamp)
	}
	sum.Elapsed = time.Since(start)
	return sum
}

// backupFile copies the original to <folder>/BackupDirName/<stamp>/<name> and returns that path.
// If the original does not exist there is nothing to copy, so it returns an empty path and lets the task fail on its own.
func backupFile(in, stamp string) (string, error) {
	data, err := os.ReadFile(in)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", i18n.Errorf("err.backup.failed", err)
	}
	dir := filepath.Join(filepath.Dir(in), BackupDirName, stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", i18n.Errorf("err.backup.failed", err)
	}
	dst := filepath.Join(dir, filepath.Base(in))
	if err := WriteFileAtomic(dst, data); err != nil {
		return "", i18n.Errorf("err.backup.failed", err)
	}
	return dst, nil
}

// finishBackupDirs cleans up empty backup folders after the run ends and returns the folders that still hold backups.
// Doing this during the run could remove a folder another thread is about to write to, so it waits until everything is done.
func finishBackupDirs(files []Input, stamp string) []string {
	seen := make(map[string]bool)
	var kept []string
	for _, f := range files {
		root := filepath.Join(filepath.Dir(f.Path), BackupDirName)
		dir := filepath.Join(root, stamp)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if err := os.Remove(dir); err != nil { // removed only when empty
			if _, statErr := os.Stat(dir); statErr == nil {
				kept = append(kept, dir)
			}
			continue
		}
		os.Remove(root) // removed only when there are no other backups
	}
	slices.Sort(kept)
	return kept
}

// RestoreBackup puts a backed-up original back at its original location (dest). The backup file is left in place.
// Only files inside the BackupDirName folder are accepted for restore.
func RestoreBackup(backup, dest string) error {
	// Backups live at <folder>/.sprite-tools-backup/<time>/<name>, so restore is allowed only to <folder>/<name>.
	backup, dest = filepath.Clean(backup), filepath.Clean(dest)
	runDir := filepath.Dir(backup)
	backupRoot := filepath.Dir(runDir)
	if filepath.Base(backupRoot) != BackupDirName ||
		!strings.EqualFold(filepath.Dir(backupRoot), filepath.Dir(dest)) ||
		!strings.EqualFold(filepath.Base(backup), filepath.Base(dest)) {
		return i18n.Errorf("err.backup.mismatch", backup, dest)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return i18n.Errorf("err.backup.read", err)
	}
	if err := WriteFileAtomic(dest, data); err != nil {
		return i18n.Errorf("err.backup.restore_failed", err)
	}
	return nil
}

// runTask guards against a panic in a task so it does not abort the whole run.
func runTask(task Task, in, out string) (msg string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = i18n.Errorf("err.internal", r)
		}
	}()
	return task(in, out)
}

// BackupFileNow copies one file into a backup folder named for the current time (<folder>/BackupDirName/<time>/<name>) and returns that path.
// If the original does not exist there is nothing to copy, so it returns an empty path and nil.
func BackupFileNow(path string) (string, error) {
	return backupFile(path, time.Now().Format(backupStampLayout))
}
