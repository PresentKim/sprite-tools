package main

import (
	"bytes"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// This guard enforces "all code comments are written in English" across the
// whole repository. User-facing text lives in the i18n catalogs (*.json, not
// scanned); everything else that can hold comments must be free of Hangul,
// except for the small allow-list below.

// isHangul reports whether r is a Hangul syllable, Jamo or compatibility Jamo.
func isHangul(r rune) bool {
	return r >= 0xAC00 && r <= 0xD7A3 || r >= 0x1100 && r <= 0x11FF || r >= 0x3130 && r <= 0x318F
}

func hasHangul(s string) bool {
	for _, r := range s {
		if isHangul(r) {
			return true
		}
	}
	return false
}

// Directories (slash-separated, relative to the repository root) that are not scanned.
var skipDirs = map[string]bool{
	".git":                      true,
	"node_modules":              true,
	"gui/build":                 true,
	"gui/frontend/dist":         true,
	"gui/frontend/wailsjs":      true,
	"docs/screenshots":          true,
	"gui/frontend/node_modules": true,
}

// Extensions that can hold code comments or documentation.
var scanExts = map[string]bool{
	".go": true, ".js": true, ".css": true, ".html": true,
	".yml": true, ".yaml": true, ".md": true,
}

// Whole files that are intentionally Korean.
var allowedFiles = map[string]string{
	"README.ko.md": "Korean translation of the README",
}

// Exact snippets that may appear inside otherwise English files.
var allowedSnippets = map[string][]string{
	"README.md":           {"[한국어](README.ko.md)", "(English · 한국어)"}, // language switch link; language selector label
	"docs/TRANSLATING.md": {"`한국어`"},                                  // example of a language's own name
	"pkg/i18n/README.md":  {"{ko 한국어}"},                               // sample output of i18n.Languages()
}

// In *_test.go files Hangul is allowed inside string/char literals only: it is
// test data that checks the Korean catalog (golden messages, Hangul detection).
// Comments, identifiers and everything else must still be English.

// hangulOutsideStrings returns the 1-based line numbers of Hangul found in src
// outside Go string and character literals.
func hangulOutsideStrings(t *testing.T, name string, src []byte) []int {
	t.Helper()
	fset := token.NewFileSet()
	file := fset.AddFile(name, fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, scanner.ScanComments)
	var lines []int
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.STRING || tok == token.CHAR {
			continue
		}
		text := lit
		if text == "" {
			text = tok.String()
		}
		if hasHangul(text) {
			lines = append(lines, fset.Position(pos).Line)
		}
	}
	return lines
}

func TestNoHangulOutsideAllowList(t *testing.T) {
	root := "."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(path)
		if d.IsDir() {
			if skipDirs[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		if !scanExts[strings.ToLower(filepath.Ext(rel))] {
			return nil
		}
		if _, ok := allowedFiles[rel]; ok {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			return nil // binary or not UTF-8 text
		}
		if !hasHangul(string(data)) {
			return nil
		}
		if strings.HasSuffix(rel, "_test.go") {
			for _, line := range hangulOutsideStrings(t, rel, data) {
				t.Errorf("%s:%d: Hangul outside a string literal (translate the comment/message to English)", rel, line)
			}
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, snip := range allowedSnippets[rel] {
				line = strings.ReplaceAll(line, snip, "")
			}
			if hasHangul(line) {
				t.Errorf("%s:%d: Hangul found (write comments and docs in English): %s", rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The guard itself must catch what it is meant to catch.
func TestHangulOutsideStrings(t *testing.T) {
	src := []byte("package x\n// 주석\nvar a = \"한글\"\nvar b = `한글`\nvar c = '가'\nfunc f() { /* 블록 */ }\n")
	got := hangulOutsideStrings(t, "x_test.go", src)
	if len(got) != 2 || got[0] != 2 || got[1] != 6 {
		t.Errorf("lines = %v, want [2 6]", got)
	}
}
