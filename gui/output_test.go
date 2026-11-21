package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveToOutputKeepsSubfolders(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	a := NewApp()
	p := filepath.Join(src, "chars", "hero.png")
	writeFile(t, p, "hero")
	r, err := a.MoveToOutput(p, filepath.ToSlash(src), out)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(out, "chars", "hero.png")
	if r.Moved != want || r.Original != p {
		t.Errorf("MoveToOutput = %+v, want moved %s", r, want)
	}
	if exists(p) {
		t.Error("original still exists")
	}
	if data, _ := os.ReadFile(want); string(data) != "hero" {
		t.Errorf("moved content = %q", data)
	}
}

func TestMoveToOutputRenamesOnCollision(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	a := NewApp()
	writeFile(t, filepath.Join(out, "hero.png"), "taken")
	p := filepath.Join(src, "hero.png")
	writeFile(t, p, "new")
	r, err := a.MoveToOutput(p, src, out)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(r.Moved) != "hero-1.png" {
		t.Errorf("moved name = %s, want hero-1.png", filepath.Base(r.Moved))
	}
	if data, _ := os.ReadFile(filepath.Join(out, "hero.png")); string(data) != "taken" {
		t.Errorf("existing output file changed: %q", data)
	}
}

func TestMoveToOutputOutsideRootUsesBaseName(t *testing.T) {
	src, other, out := t.TempDir(), t.TempDir(), t.TempDir()
	p := filepath.Join(other, "deep", "a.png")
	writeFile(t, p, "a")
	r, err := NewApp().MoveToOutput(p, src, out)
	if err != nil {
		t.Fatal(err)
	}
	if r.Moved != filepath.Join(out, "a.png") {
		t.Errorf("moved = %s", r.Moved)
	}
}

func TestMoveToOutputRejects(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	a := NewApp()
	p := filepath.Join(src, "a.png")
	writeFile(t, p, "a")
	txt := filepath.Join(src, "a.txt")
	writeFile(t, txt, "x")
	cases := []struct{ name, path, outDir string }{
		{"no output folder", p, ""},
		{"same folder", p, src},
		{"non PNG", txt, out},
		{"missing file", filepath.Join(src, "missing.png"), out},
	}
	for _, c := range cases {
		if _, err := a.MoveToOutput(c.path, src, c.outDir); err == nil {
			t.Errorf("%s: move accepted", c.name)
		}
	}
	if !exists(p) || !exists(txt) {
		t.Error("refused files left their folder")
	}
}

func TestRestoreMoved(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	a := NewApp()
	p := filepath.Join(src, "sub", "a.png")
	writeFile(t, p, "data")
	r, err := a.MoveToOutput(p, src, out)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RestoreMoved(r.Moved, r.Original, out); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != "data" {
		t.Errorf("restored content = %q", data)
	}
	if exists(filepath.Join(out, "sub")) {
		t.Error("empty output subfolder left behind")
	}
	if !exists(out) {
		t.Error("output folder removed")
	}
}

func TestRestoreMovedRejects(t *testing.T) {
	src, out, other := t.TempDir(), t.TempDir(), t.TempDir()
	a := NewApp()
	writeFile(t, filepath.Join(out, "a.png"), "1")
	writeFile(t, filepath.Join(other, "a.png"), "2")
	writeFile(t, filepath.Join(src, "a.png"), "3")
	cases := []struct{ name, moved, original string }{
		{"outside output folder", filepath.Join(other, "a.png"), filepath.Join(src, "b.png")},
		{"name mismatch", filepath.Join(out, "a.png"), filepath.Join(src, "b.png")},
		{"existing original", filepath.Join(out, "a.png"), filepath.Join(src, "a.png")},
	}
	for _, c := range cases {
		if err := a.RestoreMoved(c.moved, c.original, out); err == nil {
			t.Errorf("%s: restore accepted", c.name)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(src, "a.png")); string(data) != "3" {
		t.Errorf("existing original changed: %q", data)
	}
}

func TestCopyThenRemove(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	writeFile(t, src, "data")
	if err := copyThenRemove(src, dst); err != nil {
		t.Fatal(err)
	}
	if exists(src) {
		t.Error("source still exists")
	}
	if data, _ := os.ReadFile(dst); string(data) != "data" {
		t.Errorf("copied content = %q", data)
	}
	writeFile(t, src, "again")
	if err := copyThenRemove(src, dst); err == nil {
		t.Error("copy over an existing file accepted")
	}
	if !exists(src) {
		t.Error("source removed after a failed copy")
	}
}
