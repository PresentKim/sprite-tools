package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestTrashFileMovesAndRenamesOnCollision(t *testing.T) {
	dir := t.TempDir()
	a := NewApp()
	var trashed []string
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, "hero.png")
		writeFile(t, p, "v"+string(rune('0'+i)))
		r, err := a.TrashFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if exists(p) {
			t.Errorf("original still exists after trashing #%d", i)
		}
		if r.Original != p {
			t.Errorf("Original = %q, want %q", r.Original, p)
		}
		trashed = append(trashed, r.Trashed)
	}
	want := []string{"hero.png", "hero-1.png", "hero-2.png"}
	for i, tp := range trashed {
		if filepath.Base(filepath.Dir(tp)) != TrashDirName || filepath.Dir(filepath.Dir(tp)) != dir {
			t.Errorf("trashed %d is not in <dir>/%s: %s", i, TrashDirName, tp)
		}
		if filepath.Base(tp) != want[i] {
			t.Errorf("trashed name %d = %q, want %q", i, filepath.Base(tp), want[i])
		}
		data, err := os.ReadFile(tp)
		if err != nil || string(data) != "v"+string(rune('0'+i)) {
			t.Errorf("content of %s = %q, %v", tp, data, err)
		}
	}
}

func TestTrashFileRejects(t *testing.T) {
	dir := t.TempDir()
	a := NewApp()
	txt := filepath.Join(dir, "note.txt")
	writeFile(t, txt, "x")
	if _, err := a.TrashFile(txt); err == nil {
		t.Error("a non-PNG file must be refused")
	}
	if !exists(txt) {
		t.Error("refused file must stay in place")
	}
	if _, err := a.TrashFile(filepath.Join(dir, "missing.png")); err == nil {
		t.Error("a missing file must be refused")
	}
	if exists(filepath.Join(dir, TrashDirName)) {
		t.Error("a failed trash must not leave an empty trash folder")
	}
	sub := filepath.Join(dir, "folder.png")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := a.TrashFile(sub); err == nil {
		t.Error("a folder must be refused")
	}
}

func TestRestoreTrashed(t *testing.T) {
	dir := t.TempDir()
	a := NewApp()
	p := filepath.Join(dir, "a.png")
	writeFile(t, p, "data")
	r, err := a.TrashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RestoreTrashed(r.Trashed, r.Original); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(p); err != nil || string(data) != "data" {
		t.Errorf("restored content = %q, %v", data, err)
	}
	if exists(r.Trashed) || exists(filepath.Join(dir, TrashDirName)) {
		t.Error("the trash file and its now-empty folder must be gone")
	}
}

func TestRestoreTrashedRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	a := NewApp()
	p := filepath.Join(dir, "a.png")
	writeFile(t, p, "old")
	r, err := a.TrashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, "new") // a different file took the name meanwhile
	if err := a.RestoreTrashed(r.Trashed, r.Original); err == nil {
		t.Fatal("restore over an existing file must fail")
	}
	if data, _ := os.ReadFile(p); string(data) != "new" {
		t.Errorf("existing file was modified: %q", data)
	}
	if data, _ := os.ReadFile(r.Trashed); string(data) != "old" {
		t.Errorf("trashed file was lost: %q", data)
	}
}

func TestRestoreTrashedValidation(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	a := NewApp()
	trash := filepath.Join(dir, TrashDirName)
	writeFile(t, filepath.Join(trash, "a.png"), "1")
	writeFile(t, filepath.Join(trash, "a-3.png"), "2")
	writeFile(t, filepath.Join(trash, "ab.png"), "3")
	writeFile(t, filepath.Join(trash, "a-x.png"), "4")
	writeFile(t, filepath.Join(dir, "plain.png"), "5")
	writeFile(t, filepath.Join(other, TrashDirName, "a.png"), "6")
	writeFile(t, filepath.Join(dir, "sub", TrashDirName, "a.png"), "7")

	bad := []struct{ name, trashed, original string }{
		{"not in a trash folder", filepath.Join(dir, "plain.png"), filepath.Join(dir, "z.png")},
		{"name mismatch", filepath.Join(trash, "ab.png"), filepath.Join(dir, "a.png")},
		{"non numeric suffix", filepath.Join(trash, "a-x.png"), filepath.Join(dir, "a.png")},
		{"other folder", filepath.Join(other, TrashDirName, "a.png"), filepath.Join(dir, "a.png")},
		{"trash of a subfolder", filepath.Join(dir, "sub", TrashDirName, "a.png"), filepath.Join(dir, "a.png")},
		{"escape via dotdot", filepath.Join(trash, "..", "plain.png"), filepath.Join(dir, "plain.png")},
		{"non PNG original", filepath.Join(trash, "a.png"), filepath.Join(dir, "a.txt")},
	}
	for _, c := range bad {
		if err := a.RestoreTrashed(c.trashed, c.original); err == nil {
			t.Errorf("%s: restore must be refused", c.name)
		}
	}
	if err := a.RestoreTrashed(filepath.Join(trash, "a-3.png"), filepath.Join(dir, "a.png")); err != nil {
		t.Errorf("a-3.png -> a.png must be accepted: %v", err)
	}
}

func TestTrashSummary(t *testing.T) {
	dir := t.TempDir()
	a := NewApp()
	trash := filepath.Join(dir, TrashDirName)
	writeFile(t, filepath.Join(trash, "a.png"), "12345")
	writeFile(t, filepath.Join(trash, "b.png"), "123")
	writeFile(t, filepath.Join(trash, "nested", "c.png"), "999") // nested files are not part of the trash
	notTrash := filepath.Join(dir, "images")
	writeFile(t, filepath.Join(notTrash, "d.png"), "1")
	got := a.TrashSummary([]string{trash, trash, notTrash, filepath.Join(dir, "missing", TrashDirName)})
	if got.Count != 2 || got.Bytes != 8 {
		t.Errorf("TrashSummary = %+v, want 2 files / 8 bytes", got)
	}
}

func TestEmptyTrashIgnoresForeignFolders(t *testing.T) {
	dir := t.TempDir()
	a := NewApp()
	notTrash := filepath.Join(dir, "images")
	writeFile(t, filepath.Join(notTrash, "d.png"), "1")
	n, err := a.EmptyTrashToRecycleBin([]string{notTrash})
	if err != nil || n != 0 {
		t.Errorf("EmptyTrashToRecycleBin(non trash) = %d, %v", n, err)
	}
	if !exists(filepath.Join(notTrash, "d.png")) {
		t.Error("a folder that is not a trash folder must never be emptied")
	}
}

// The real Recycle Bin call is not part of regular test runs (it would fill the developer's bin).
// Set SPRITE_TOOLS_TEST_RECYCLE=1 on Windows to run it.
func TestEmptyTrashToRecycleBin(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("SPRITE_TOOLS_TEST_RECYCLE") != "1" {
		t.Skip("set SPRITE_TOOLS_TEST_RECYCLE=1 on Windows to move scratch files to the Recycle Bin")
	}
	dir := t.TempDir()
	a := NewApp()
	var dirs []string
	for _, name := range []string{"recycle-test-a.png", "recycle-test-b.png"} {
		p := filepath.Join(dir, name)
		writeFile(t, p, "scratch "+name)
		r, err := a.TrashFile(p)
		if err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, filepath.Dir(r.Trashed))
	}
	n, err := a.EmptyTrashToRecycleBin(dirs)
	if err != nil || n != 2 {
		t.Fatalf("EmptyTrashToRecycleBin = %d, %v", n, err)
	}
	if exists(filepath.Join(dir, TrashDirName)) {
		t.Error("the trash folder must be removed once empty")
	}
}
