package i18n

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func resetLanguage(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _ = SetLanguage(DefaultLanguage) })
}

// verbs: fmt 동사(%[flags][width][.prec]verb)를 순서대로 뽑습니다. "%%" 는 무시합니다.
var verbRe = regexp.MustCompile(`%([#+\- 0]*)(\*|\d+)?(\.(\*|\d+)?)?([a-zA-Z%])`)

func verbs(s string) []string {
	var out []string
	for _, m := range verbRe.FindAllStringSubmatch(s, -1) {
		if m[5] == "%" {
			continue
		}
		out = append(out, m[0])
	}
	return out
}

func verbKinds(s string) []string {
	var out []string
	for _, m := range verbRe.FindAllStringSubmatch(s, -1) {
		if m[5] != "%" {
			out = append(out, m[5])
		}
	}
	return out
}

func TestCatalogsMatchEnglish(t *testing.T) {
	en := catalogs[DefaultLanguage]
	if len(en) == 0 {
		t.Fatal("empty en catalog")
	}
	for code, c := range catalogs {
		if name := c[MetaName]; strings.TrimSpace(name) == "" {
			t.Errorf("%s: missing %s", code, MetaName)
		}
		if code == DefaultLanguage {
			continue
		}
		for k, v := range en {
			got, ok := c[k]
			if !ok {
				t.Errorf("%s: missing key %q", code, k)
				continue
			}
			if k == MetaName {
				continue
			}
			if !slices.Equal(verbKinds(v), verbKinds(got)) {
				t.Errorf("%s: %q verbs differ: en %v, %s %v", code, k, verbs(v), code, verbs(got))
			}
		}
		for k := range c {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: extra key %q (not in en.json)", code, k)
			}
		}
	}
}

func TestCatalogFiles(t *testing.T) {
	entries, err := os.ReadDir("locales")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("locales", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.Valid(data) {
			t.Errorf("%s: invalid UTF-8", e.Name())
		}
		if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
			t.Errorf("%s: has a BOM", e.Name())
		}
		// 키가 정렬되어 있고 중복이 없는지 파일 순서대로 확인합니다.
		dec := json.NewDecoder(bytes.NewReader(data))
		if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
			t.Fatalf("%s: not a JSON object", e.Name())
		}
		var keys []string
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				t.Fatal(err)
			}
			var v string
			if err := dec.Decode(&v); err != nil {
				t.Fatalf("%s: %v: value is not a string", e.Name(), k)
			}
			keys = append(keys, k.(string))
		}
		if !sort.StringsAreSorted(keys) {
			t.Errorf("%s: keys are not sorted", e.Name())
		}
		for i := 1; i < len(keys); i++ {
			if keys[i] == keys[i-1] {
				t.Errorf("%s: duplicate key %q", e.Name(), keys[i])
			}
		}
		if code := strings.TrimSuffix(e.Name(), ".json"); code != strings.ToLower(code) || strings.ContainsAny(code, "-_.") {
			t.Errorf("%s: file name must be a lowercase base language code", e.Name())
		}
	}
}

func TestSetLanguage(t *testing.T) {
	resetLanguage(t)
	if Language() != "en" {
		t.Fatalf("default = %q, want en", Language())
	}
	for _, tag := range []string{"ko", "ko-KR", "ko_KR", "KO", " ko ", "ko_KR.UTF-8", "ko-KR@x"} {
		if err := SetLanguage("en"); err != nil {
			t.Fatal(err)
		}
		if err := SetLanguage(tag); err != nil || Language() != "ko" {
			t.Errorf("SetLanguage(%q): %v, now %q", tag, err, Language())
		}
	}
	for _, tag := range []string{"en", "en_US.UTF-8", "en-GB"} {
		_ = SetLanguage("ko")
		if err := SetLanguage(tag); err != nil || Language() != "en" {
			t.Errorf("SetLanguage(%q): %v, now %q", tag, err, Language())
		}
	}
	_ = SetLanguage("ko")
	for _, tag := range []string{"", "xx", "C", "POSIX", "zz-ZZ"} {
		if err := SetLanguage(tag); err == nil {
			t.Errorf("SetLanguage(%q) should fail", tag)
		}
		if Language() != "ko" {
			t.Errorf("failed SetLanguage(%q) changed language to %q", tag, Language())
		}
	}
}

func TestTAndFallback(t *testing.T) {
	resetLanguage(t)
	_ = SetLanguage("en")
	if got := T("msg.reduce.result", 5, 3); got != "Colors 5 → 3" {
		t.Errorf("en: %q", got)
	}
	_ = SetLanguage("ko")
	if got := T("msg.reduce.result", 5, 3); got != "색상 5 → 3" {
		t.Errorf("ko: %q", got)
	}
	if got := T("no.such.key", 1); got != "no.such.key" {
		t.Errorf("missing key: %q", got)
	}
	// 현재 언어에 없는 키는 영어로 넘어갑니다.
	const key = "test.only.in.en"
	catalogs[DefaultLanguage][key] = "English only %d"
	defer delete(catalogs[DefaultLanguage], key)
	if got := T(key, 7); got != "English only 7" {
		t.Errorf("fallback: %q", got)
	}
}

func TestErrorf(t *testing.T) {
	resetLanguage(t)
	_ = SetLanguage("ko")
	err := Errorf("err.trim.pad", 3)
	_ = SetLanguage("en")
	// 에러는 만들어진 시점의 언어로 고정됩니다.
	if err.Error() != "여백은 0 이상이어야 합니다: 3" {
		t.Errorf("got %q", err.Error())
	}
	cause := errors.New("cause")
	w := Wrap(cause, "err.trim.pad", 1)
	if !errors.Is(w, cause) || w.Error() != "Padding must be 0 or greater: 1" {
		t.Errorf("Wrap: %v", w)
	}
	var e *Error
	if !errors.As(w, &e) || e.Key != "err.trim.pad" {
		t.Errorf("errors.As: %+v", e)
	}
}

func TestLanguages(t *testing.T) {
	got := Languages()
	if len(got) < 2 || got[0] != (LangInfo{"en", "English"}) {
		t.Fatalf("Languages() = %+v", got)
	}
	if !slices.Contains(got, LangInfo{"ko", "한국어"}) {
		t.Errorf("ko missing: %+v", got)
	}
}

// ---------- 소스 코드가 쓰는 키가 모두 en.json 에 있는지 검사 ----------

// keyArg: 첫 인자가 아닌 곳에 키가 있는 함수의 위치를 지정합니다. (recv 가 비어 있으면 recv 없는 호출/아무 recv)
type keyFunc struct {
	recv string // 패키지/수신자 식별자 ("" 이면 아무거나, 단 "-" 이면 수신자 없는 호출만)
	arg  int
}

var keyFuncs = map[string][]keyFunc{
	"T":          {{"i18n", 0}},
	"Errorf":     {{"i18n", 0}},
	"Wrap":       {{"i18n", 1}},
	"Skip":       {{"common", 0}, {"-", 0}},
	"usageError": {{"-", 1}},
}

// 키처럼 생긴 문자열 리터럴(네임스페이스 접두사 + 점)은 호출 형태와 상관없이 en.json 에 있어야 합니다.
var keyLiteralRe = regexp.MustCompile(`^(cli|cmd|flag|msg|err|skip|gui)\.[a-z0-9_.]+$`)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("repo root not found at %s", dir)
	}
	return dir
}

func TestSourceKeysExist(t *testing.T) {
	root := repoRoot(t)
	en := catalogs[DefaultLanguage]
	fset := token.NewFileSet()
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "frontend", "build", ".git", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		check := func(pos token.Pos, key string) {
			checked++
			if _, ok := en[key]; !ok {
				t.Errorf("%s:%d: key %q is not in en.json", rel, fset.Position(pos).Line, key)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				name, recv := "", "-"
				switch fn := x.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					name = fn.Sel.Name
					recv = ""
					if id, ok := fn.X.(*ast.Ident); ok {
						recv = id.Name
					}
				}
				for _, kf := range keyFuncs[name] {
					if kf.recv != recv || kf.arg >= len(x.Args) {
						continue
					}
					if lit, ok := x.Args[kf.arg].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						key, _ := strconv.Unquote(lit.Value)
						check(lit.Pos(), key)
					}
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					if s, err := strconv.Unquote(x.Value); err == nil && keyLiteralRe.MatchString(s) {
						check(x.Pos(), s)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Errorf("only %d key references found; scanner is probably broken", checked)
	}
}
