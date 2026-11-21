package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sprite-tools/pkg/i18n"
)

type MoveResult struct {
	Original string `json:"original"`
	Moved    string `json:"moved"`
}

func (a *App) MoveToOutput(path, root, outDir string) (MoveResult, error) {
	return moveToOutput(path, root, outDir)
}

func (a *App) RestoreMoved(moved, original, outDir string) error {
	return restoreMoved(moved, original, outDir)
}

func moveToOutput(path, root, outDir string) (MoveResult, error) {
	if strings.TrimSpace(outDir) == "" {
		return MoveResult{}, i18n.Errorf("err.gui.move_no_output")
	}
	path, outDir = absPath(path), absPath(outDir)
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return MoveResult{}, i18n.Errorf("err.gui.move_png_only")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return MoveResult{}, i18n.Errorf("err.gui.move_failed", err)
	}
	if !st.Mode().IsRegular() {
		return MoveResult{}, i18n.Errorf("err.gui.move_png_only")
	}
	dst := filepath.Join(outDir, relativeTo(path, root))
	dir := filepath.Dir(dst)
	if strings.EqualFold(dir, filepath.Dir(path)) {
		return MoveResult{}, i18n.Errorf("err.gui.move_same_folder")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return MoveResult{}, i18n.Errorf("err.gui.move_failed", err)
	}
	name := filepath.Base(dst)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 0; n < 100000; n++ {
		cand := name
		if n > 0 {
			cand = stem + "-" + strconv.Itoa(n) + ext
		}
		target := filepath.Join(dir, cand)
		err := moveAcrossVolumes(path, target)
		if err == nil {
			return MoveResult{Original: path, Moved: target}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return MoveResult{}, i18n.Errorf("err.gui.move_failed", err)
		}
	}
	return MoveResult{}, i18n.Errorf("err.gui.move_failed", fs.ErrExist)
}

func relativeTo(path, root string) string {
	if root != "" {
		r := filepath.Clean(filepath.FromSlash(root))
		if len(path) > len(r) && strings.EqualFold(path[:len(r)], r) && os.IsPathSeparator(path[len(r)]) {
			return path[len(r)+1:]
		}
	}
	return filepath.Base(path)
}

func isInside(path, dir string) bool {
	return len(path) > len(dir) && strings.EqualFold(path[:len(dir)], dir) && os.IsPathSeparator(path[len(dir)])
}

func moveAcrossVolumes(src, dst string) error {
	err := moveNoClobber(src, dst)
	if err == nil || errors.Is(err, fs.ErrExist) {
		return err
	}
	return copyThenRemove(src, dst)
}

func copyThenRemove(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		in.Close()
		return err
	}
	_, err = io.Copy(out, in)
	in.Close()
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Remove(src)
	}
	if err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

func restoreMoved(moved, original, outDir string) error {
	moved, original, outDir = filepath.Clean(moved), filepath.Clean(original), filepath.Clean(outDir)
	if outDir == "." || !isInside(moved, outDir) ||
		!strings.EqualFold(filepath.Ext(original), ".png") ||
		!trashedNameMatches(moved, original) {
		return i18n.Errorf("err.gui.move_restore_mismatch", moved, original)
	}
	st, err := os.Lstat(moved)
	if err != nil {
		return i18n.Errorf("err.gui.restore_failed", err)
	}
	if !st.Mode().IsRegular() {
		return i18n.Errorf("err.gui.move_restore_mismatch", moved, original)
	}
	if err := moveAcrossVolumes(moved, original); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return i18n.Errorf("err.gui.restore_exists", original)
		}
		return i18n.Errorf("err.gui.restore_failed", err)
	}
	for d := filepath.Dir(moved); isInside(d, outDir); d = filepath.Dir(d) {
		if os.Remove(d) != nil {
			break
		}
	}
	return nil
}
