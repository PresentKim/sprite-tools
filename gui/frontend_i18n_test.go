package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"sprite-tools/pkg/i18n"
)

// Guards for the frontend catalogs (gui/frontend/src/i18n/locales/*.json):
//   - every catalog has the same keys and {placeholders} as en.json, is sorted valid UTF-8 and has _meta.name
//   - every key used by index.html (data-i18n*) and by the sources (t('...'), tn('...'), lt('...'), ...) exists in en.json
//   - no catalog key is unused
//   - no Hangul is left in index.html / src/**/*.js / src/**/*.css outside comments
//   - the frontend and the Go catalogs offer the same languages

const (
	feRoot    = "frontend"
	feLocales = "frontend/src/i18n/locales"
)

var (
	placeholderRe = regexp.MustCompile(`\{(\w+)\}`)
	pluralSuffix  = []string{".zero", ".one", ".two", ".few", ".many", ".other"}
	// Hangul syllables, Jamo, compatibility Jamo, Jamo extended A/B and halfwidth Hangul.
	hangulRe = regexp.MustCompile("[ᄀ-ᇿ㄰-㆏ꥠ-꥿가-힣ힰ-퟿ﾠ-ￜ]")
)

type catalog struct {
	code string
	keys []string // file order
	m    map[string]string
}

// loadFrontendCatalogs reads and validates the structure of every frontend catalog.
func loadFrontendCatalogs(t *testing.T) map[string]*catalog {
	t.Helper()
	entries, err := os.ReadDir(feLocales)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*catalog{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("%s: only <code>.json files belong in the locales folder", e.Name())
			continue
		}
		code := strings.TrimSuffix(e.Name(), ".json")
		if code != strings.ToLower(code) || strings.ContainsAny(code, "-_.") {
			t.Errorf("%s: file name must be a lowercase base language code", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(feLocales, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.Valid(data) {
			t.Errorf("%s: invalid UTF-8", e.Name())
		}
		if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
			t.Errorf("%s: has a BOM", e.Name())
		}
		c := &catalog{code: code, m: map[string]string{}}
		dec := json.NewDecoder(bytes.NewReader(data))
		if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
			t.Fatalf("%s: not a JSON object", e.Name())
		}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				t.Fatalf("%s: %v", e.Name(), err)
			}
			var v string
			if err := dec.Decode(&v); err != nil {
				t.Fatalf("%s: %v: value is not a string", e.Name(), k)
			}
			key := k.(string)
			if _, dup := c.m[key]; dup {
				t.Errorf("%s: duplicate key %q", e.Name(), key)
			}
			c.keys = append(c.keys, key)
			c.m[key] = v
		}
		if !sort.StringsAreSorted(c.keys) {
			t.Errorf("%s: keys are not sorted", e.Name())
		}
		if strings.TrimSpace(c.m[i18n.MetaName]) == "" {
			t.Errorf("%s: missing or empty %q", e.Name(), i18n.MetaName)
		}
		out[code] = c
	}
	if out[i18n.DefaultLanguage] == nil {
		t.Fatal("en.json is missing")
	}
	return out
}

func placeholders(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// placeholdersOf lists the placeholders of a catalog value. In plural forms {n} is optional (the English singular
// may say "1 file" instead of "{n} file"), so it is ignored there.
func placeholdersOf(key, value string) []string {
	ph := placeholders(value)
	if _, plural := pluralBase(key); !plural {
		return ph
	}
	out := ph[:0:0]
	for _, p := range ph {
		if p != "n" {
			out = append(out, p)
		}
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// pluralBase returns the key without a plural suffix and whether it had one.
func pluralBase(key string) (string, bool) {
	for _, s := range pluralSuffix {
		if strings.HasSuffix(key, s) {
			return strings.TrimSuffix(key, s), true
		}
	}
	return key, false
}

func TestFrontendCatalogs(t *testing.T) {
	cats := loadFrontendCatalogs(t)
	en := cats[i18n.DefaultLanguage]

	// Plural keys: every base that has one form needs .one and .other in English.
	bases := map[string]map[string]bool{}
	for _, k := range en.keys {
		if b, ok := pluralBase(k); ok {
			if bases[b] == nil {
				bases[b] = map[string]bool{}
			}
			bases[b][k[len(b):]] = true
		}
	}
	for b, forms := range bases {
		if !forms[".one"] || !forms[".other"] {
			t.Errorf("en.json: plural key %q needs both .one and .other", b)
		}
		if _, exists := en.m[b]; exists {
			t.Errorf("en.json: %q is used both as a plain key and as a plural base", b)
		}
	}

	for code, c := range cats {
		if code == i18n.DefaultLanguage {
			continue
		}
		for _, k := range en.keys {
			v, ok := c.m[k]
			if !ok {
				t.Errorf("%s.json: missing key %q", code, k)
				continue
			}
			if k == i18n.MetaName {
				continue
			}
			if !sameStrings(placeholdersOf(k, en.m[k]), placeholdersOf(k, v)) {
				t.Errorf("%s.json: %q placeholders differ: en %v, %s %v", code, k, placeholdersOf(k, en.m[k]), code, placeholdersOf(k, v))
			}
		}
		for _, k := range c.keys {
			if _, ok := en.m[k]; ok {
				continue
			}
			// Languages with more plural categories (few, many ...) may add forms of an existing plural key.
			if b, isPlural := pluralBase(k); isPlural && bases[b] != nil {
				if !sameStrings(placeholdersOf(k, en.m[b+".other"]), placeholdersOf(k, c.m[k])) {
					t.Errorf("%s.json: %q placeholders differ from %q", code, k, b+".other")
				}
				continue
			}
			t.Errorf("%s.json: extra key %q (not in en.json)", code, k)
		}
	}
}

// Frontend and Go catalogs must offer the same languages (a language in only one of them is a half-finished translation).
func TestFrontendAndGoLanguagesMatch(t *testing.T) {
	cats := loadFrontendCatalogs(t)
	goLangs := map[string]string{}
	for _, l := range i18n.Languages() {
		goLangs[l.Code] = l.Name
	}
	for code, c := range cats {
		name, ok := goLangs[code]
		if !ok {
			t.Errorf("language %q has a frontend catalog but no Go catalog (pkg/i18n/locales/%s.json)", code, code)
			continue
		}
		if name != c.m[i18n.MetaName] {
			t.Errorf("language %q: _meta.name differs: frontend %q, Go %q", code, c.m[i18n.MetaName], name)
		}
	}
	for code := range goLangs {
		if cats[code] == nil {
			t.Errorf("language %q has a Go catalog but no frontend catalog (%s/%s.json)", code, feLocales, code)
		}
	}
}

// ---- source scanning ----

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(data) {
		t.Errorf("%s: invalid UTF-8", path)
	}
	return string(data)
}

func frontendSources(t *testing.T, exts ...string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(filepath.Join(feRoot, "src"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "locales" {
				return filepath.SkipDir
			}
			return nil
		}
		for _, e := range exts {
			if strings.HasSuffix(path, e) {
				files = append(files, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

// stripJSComments removes // and /* */ comments from JavaScript, keeping everything else (and line breaks).
// It tracks quoted strings, template literals (with ${} nesting) and regular expression literals, so a "//" inside a string,
// a URL or a regex is not mistaken for a comment. Limitation: a regex literal after a keyword such as
// `return /x/` is read as a division; ok is false when the scanner ends inside a string or comment.
func stripJSComments(src string) (out string, ok bool) {
	var b strings.Builder
	rs := []rune(src)
	n := len(rs)
	// stack of template-literal brace depths: when non-empty and top == 0 we are inside a template's text
	type frame struct{ depth int }
	var tmpl []frame
	prev := rune(0) // last significant character in code state
	inTemplateText := false
	i := 0
	emit := func(r rune) { b.WriteRune(r) }
	for i < n {
		c := rs[i]
		if inTemplateText {
			switch {
			case c == '\\' && i+1 < n:
				emit(c)
				emit(rs[i+1])
				i += 2
			case c == '`':
				emit(c)
				inTemplateText = false
				prev = '`'
				i++
			case c == '$' && i+1 < n && rs[i+1] == '{':
				emit(c)
				emit('{')
				tmpl = append(tmpl, frame{0})
				inTemplateText = false
				prev = '{'
				i += 2
			default:
				emit(c)
				i++
			}
			continue
		}
		switch {
		case c == '/' && i+1 < n && rs[i+1] == '/':
			for i < n && rs[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && rs[i+1] == '*':
			i += 2
			closed := false
			for i < n {
				if rs[i] == '*' && i+1 < n && rs[i+1] == '/' {
					i += 2
					closed = true
					break
				}
				if rs[i] == '\n' {
					emit('\n')
				}
				i++
			}
			if !closed {
				return b.String(), false
			}
			emit(' ')
		case c == '\'' || c == '"':
			q := c
			emit(c)
			i++
			closed := false
			for i < n {
				if rs[i] == '\\' && i+1 < n {
					emit(rs[i])
					emit(rs[i+1])
					i += 2
					continue
				}
				if rs[i] == '\n' {
					break
				}
				emit(rs[i])
				if rs[i] == q {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return b.String(), false
			}
			prev = q
		case c == '`':
			emit(c)
			inTemplateText = true
			i++
		case c == '/' && (prev == 0 || strings.ContainsRune("(,=:[!&|?{};+-*%<>~^", prev)):
			// regular expression literal
			emit(c)
			i++
			inClass := false
			closed := false
			for i < n {
				ch := rs[i]
				if ch == '\n' {
					break
				}
				emit(ch)
				i++
				if ch == '\\' && i < n {
					emit(rs[i])
					i++
					continue
				}
				if ch == '[' {
					inClass = true
				} else if ch == ']' {
					inClass = false
				} else if ch == '/' && !inClass {
					closed = true
					break
				}
			}
			if !closed {
				return b.String(), false
			}
			for i < n && (rs[i] >= 'a' && rs[i] <= 'z') { // flags
				emit(rs[i])
				i++
			}
			prev = '/'
		default:
			if c == '{' && len(tmpl) > 0 {
				tmpl[len(tmpl)-1].depth++
			} else if c == '}' && len(tmpl) > 0 {
				if tmpl[len(tmpl)-1].depth == 0 {
					tmpl = tmpl[:len(tmpl)-1]
					emit(c)
					inTemplateText = true
					i++
					continue
				}
				tmpl[len(tmpl)-1].depth--
			}
			emit(c)
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				prev = c
			}
			i++
		}
	}
	return b.String(), !inTemplateText && len(tmpl) == 0
}

var cssCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// stripCSSComments removes /* */ comments (CSS has no // comments, so URLs are safe).
func stripCSSComments(src string) string { return cssCommentRe.ReplaceAllString(src, " ") }

func stripHTMLComments(src string) string { return htmlCommentRe.ReplaceAllString(src, " ") }

func TestStripJSComments(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a // c\nb", "a \nb"},
		{"a /* c\n */ b", "a \n  b"},
		{`s = 'x // y'`, `s = 'x // y'`},
		{`u = "http://x" // c`, `u = "http://x" `},
		{"t = `a // ${ 'b' + `c // d` } e` // c", "t = `a // ${ 'b' + `c // d` } e` "},
		{`r = /[/]+\/\//g.test(s) // c`, `r = /[/]+\/\//g.test(s) `},
		{`x = a / b // c`, `x = a / b `},
		{`q = "'" // c`, `q = "'" `},
	}
	for _, c := range cases {
		got, ok := stripJSComments(c.in)
		if !ok || got != c.want {
			t.Errorf("strip(%q) = %q (ok=%v), want %q", c.in, got, ok, c.want)
		}
	}
}

// No Hangul may remain in the UI sources outside comments: user-facing text lives in the catalogs.
func TestNoHangulInFrontendSources(t *testing.T) {
	check := func(name, text string) {
		for n, line := range strings.Split(text, "\n") {
			if loc := hangulRe.FindStringIndex(line); loc != nil {
				t.Errorf("%s:%d: Hangul outside a comment: %s", name, n+1, strings.TrimSpace(line))
			}
		}
	}
	html := filepath.Join(feRoot, "index.html")
	check(html, stripHTMLComments(readFile(t, html)))
	for _, f := range frontendSources(t, ".js") {
		text, ok := stripJSComments(readFile(t, f))
		if !ok {
			t.Errorf("%s: the comment scanner lost sync (unterminated string/comment/template, or an unusual regex literal)", f)
		}
		check(f, text)
	}
	for _, f := range frontendSources(t, ".css") {
		check(f, stripCSSComments(readFile(t, f)))
	}
}

var (
	attrKeyRe = regexp.MustCompile(`data-i18n(?:-html|-title|-placeholder|-aria-label)?="([^"]+)"`)
	// any string literal that looks like a catalog key (namespaces below); covers t('x'), tn(cond ? 'a' : 'b'), setStatus('x') ...
	literalKeyRe  = regexp.MustCompile("['\"`]((?:app|batch|common|dialog|editor|help|review|status|toast)\\.[a-z0-9_.]*[a-z0-9_])['\"`]")
	templateKeyRe = regexp.MustCompile("`((?:app|batch|common|dialog|editor|help|review|status|toast)\\.[a-z0-9_.]*\\$\\{[^`]*)`")
	plainTCallRe  = regexp.MustCompile(`\b(?:t|lt)\(\s*['"]([a-z0-9_.]+)['"]`)
)

// usedKeys collects the keys referenced by index.html and the JS sources.
// Template keys such as `batch.cmd.${id}.label` are returned as regular expressions.
func usedKeys(t *testing.T) (literals map[string][]string, templates map[string][]string, plainCalls map[string][]string) {
	t.Helper()
	literals, templates, plainCalls = map[string][]string{}, map[string][]string{}, map[string][]string{}
	html := filepath.Join(feRoot, "index.html")
	for _, m := range attrKeyRe.FindAllStringSubmatch(stripHTMLComments(readFile(t, html)), -1) {
		literals[m[1]] = append(literals[m[1]], html)
	}
	for _, f := range frontendSources(t, ".js") {
		text, _ := stripJSComments(readFile(t, f))
		for _, m := range literalKeyRe.FindAllStringSubmatch(text, -1) {
			if !strings.Contains(m[1], "${") {
				literals[m[1]] = append(literals[m[1]], f)
			}
		}
		for _, m := range templateKeyRe.FindAllStringSubmatch(text, -1) {
			parts := regexp.MustCompile(`\$\{[^}]*\}`).Split(m[1], -1)
			for i := range parts {
				parts[i] = regexp.QuoteMeta(parts[i])
			}
			templates["^"+strings.Join(parts, "[a-z0-9_]+")+"(?:\\.(?:zero|one|two|few|many|other))?$"] = append(templates["^"], f)
		}
		for _, m := range plainTCallRe.FindAllStringSubmatch(text, -1) {
			plainCalls[m[1]] = append(plainCalls[m[1]], f)
		}
	}
	return
}

func TestFrontendKeysExistAndAreUsed(t *testing.T) {
	cats := loadFrontendCatalogs(t)
	en := cats[i18n.DefaultLanguage]
	literals, templates, plainCalls := usedKeys(t)
	used := map[string]bool{}

	for key, files := range literals {
		if _, ok := en.m[key]; ok {
			used[key] = true
			continue
		}
		if _, ok := en.m[key+".other"]; ok { // plural base, used with tn()
			used[key+".one"], used[key+".other"] = true, true
			continue
		}
		t.Errorf("key %q (%s) is not in en.json", key, files[0])
	}
	// t('plural.base') would print the key itself: plural keys need tn().
	for key, files := range plainCalls {
		if _, ok := en.m[key]; !ok {
			if _, plural := en.m[key+".other"]; plural {
				t.Errorf("key %q (%s) is a plural key: use tn()", key, files[0])
			}
		}
	}
	for pattern := range templates {
		re := regexp.MustCompile(pattern)
		found := false
		for _, k := range en.keys {
			if re.MatchString(k) {
				used[k] = true
				found = true
			}
		}
		if !found {
			t.Errorf("template key %s matches no key in en.json", pattern)
		}
	}
	for _, k := range en.keys {
		if k != i18n.MetaName && !used[k] {
			t.Errorf("en.json: key %q is not used by index.html or the sources", k)
		}
	}
}
