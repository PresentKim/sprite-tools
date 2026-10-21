package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"sprite-tools/pkg/i18n"
)

// TrashDirName is the hidden folder (next to the images) that receives files deleted in Review mode.
// It starts with a dot, so folder scans skip it. Nothing is ever deleted permanently: files are moved here first
// (instant and reversible) and only later handed to the system Recycle Bin.
const TrashDirName = ".sprite-tools-trash"

// TrashResult describes a file moved to the trash folder.
type TrashResult struct {
	Original string `json:"original"`
	Trashed  string `json:"trashed"`
}

// TrashInfo summarizes the files waiting in trash folders.
type TrashInfo struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

// trashSuffixRe matches the collision suffix TrashFile appends to a stem ("stem.ext" -> "stem-N.ext").
var trashSuffixRe = regexp.MustCompile(`^-[0-9]+$`)

// moveNoClobber moves src to dst without ever replacing an existing file (os.Rename replaces on Windows).
// dst is reserved with an exclusive create first, so a concurrent writer cannot be overwritten either.
func moveNoClobber(src, dst string) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	f.Close()
	if err := os.Rename(src, dst); err != nil {
		os.Remove(dst) // remove only the empty placeholder we just made
		return err
	}
	return nil
}

// TrashFile moves a PNG into <its folder>/.sprite-tools-trash/ (renaming to name-1.png, name-2.png ... on a name clash).
func (a *App) TrashFile(path string) (TrashResult, error) {
	return trashFile(path)
}

func trashFile(path string) (TrashResult, error) {
	path = absPath(path)
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return TrashResult{}, i18n.Errorf("err.gui.trash_png_only")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return TrashResult{}, i18n.Errorf("err.gui.trash_failed", err)
	}
	if !st.Mode().IsRegular() {
		return TrashResult{}, i18n.Errorf("err.gui.trash_png_only")
	}
	dir := filepath.Join(filepath.Dir(path), TrashDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return TrashResult{}, i18n.Errorf("err.gui.trash_failed", err)
	}
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 0; n < 100000; n++ {
		cand := name
		if n > 0 {
			cand = stem + "-" + strconv.Itoa(n) + ext
		}
		dst := filepath.Join(dir, cand)
		err := moveNoClobber(path, dst)
		if err == nil {
			return TrashResult{Original: path, Trashed: dst}, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue // the name is taken: try the next suffix
		}
		os.Remove(dir) // removed only when empty
		return TrashResult{}, i18n.Errorf("err.gui.trash_failed", err)
	}
	return TrashResult{}, i18n.Errorf("err.gui.trash_failed", fs.ErrExist)
}

// trashedNameMatches tells whether trashed could have been produced by trashing a file called original.
func trashedNameMatches(trashed, original string) bool {
	tn, on := filepath.Base(trashed), filepath.Base(original)
	if strings.EqualFold(tn, on) {
		return true
	}
	oext := filepath.Ext(on)
	if !strings.EqualFold(filepath.Ext(tn), oext) {
		return false
	}
	ostem := strings.TrimSuffix(on, oext)
	tstem := strings.TrimSuffix(tn, filepath.Ext(tn))
	if len(tstem) <= len(ostem) || !strings.EqualFold(tstem[:len(ostem)], ostem) {
		return false
	}
	return trashSuffixRe.MatchString(tstem[len(ostem):])
}

// RestoreTrashed puts a trashed file back at its original location. It refuses to overwrite an existing file, and only
// accepts a file inside <original's folder>/.sprite-tools-trash/ whose name could have come from the original.
func (a *App) RestoreTrashed(trashed, original string) error {
	return restoreTrashed(trashed, original)
}

func restoreTrashed(trashed, original string) error {
	trashed, original = filepath.Clean(trashed), filepath.Clean(original)
	trashDir := filepath.Dir(trashed)
	if !strings.EqualFold(filepath.Base(trashDir), TrashDirName) ||
		!strings.EqualFold(filepath.Dir(trashDir), filepath.Dir(original)) ||
		!strings.EqualFold(filepath.Ext(original), ".png") ||
		!trashedNameMatches(trashed, original) {
		return i18n.Errorf("err.gui.restore_mismatch", trashed, original)
	}
	st, err := os.Lstat(trashed)
	if err != nil {
		return i18n.Errorf("err.gui.restore_failed", err)
	}
	if !st.Mode().IsRegular() {
		return i18n.Errorf("err.gui.restore_mismatch", trashed, original)
	}
	if err := moveNoClobber(trashed, original); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return i18n.Errorf("err.gui.restore_exists", original)
		}
		return i18n.Errorf("err.gui.restore_failed", err)
	}
	os.Remove(trashDir) // removed only when empty
	return nil
}

// trashFiles lists the regular files directly inside the given trash folders (anything else is ignored).
func trashFiles(dirs []string) (files []string, valid []string) {
	seen := map[string]bool{}
	for _, d := range dirs {
		d = filepath.Clean(d)
		key := strings.ToLower(d)
		if seen[key] || !strings.EqualFold(filepath.Base(d), TrashDirName) {
			continue
		}
		seen[key] = true
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		valid = append(valid, d)
		for _, e := range entries {
			if e.Type().IsRegular() {
				files = append(files, filepath.Join(d, e.Name()))
			}
		}
	}
	return files, valid
}

// TrashSummary counts the files (and their total size) waiting in the given .sprite-tools-trash folders.
func (a *App) TrashSummary(dirs []string) TrashInfo {
	files, _ := trashFiles(dirs)
	info := TrashInfo{Count: len(files)}
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			info.Bytes += st.Size()
		}
	}
	return info
}

// EmptyTrashToRecycleBin hands everything in the given .sprite-tools-trash folders to the Windows Recycle Bin (so it can
// still be restored from there) and removes the folders once empty. It returns how many files left the trash folders.
// Files are never deleted permanently by this app: if Windows cannot recycle a file it asks before destroying it.
func (a *App) EmptyTrashToRecycleBin(dirs []string) (int, error) {
	files, valid := trashFiles(dirs)
	if len(files) == 0 {
		for _, d := range valid {
			os.Remove(d)
		}
		return 0, nil
	}
	callErr := recycleFiles(files)
	moved := 0
	for _, f := range files {
		if _, err := os.Lstat(f); errors.Is(err, fs.ErrNotExist) {
			moved++
		}
	}
	for _, d := range valid {
		os.Remove(d) // removed only when empty
	}
	if callErr != nil && moved < len(files) {
		return moved, callErr
	}
	return moved, nil
}
