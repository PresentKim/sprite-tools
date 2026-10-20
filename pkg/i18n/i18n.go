// Package i18n holds the language catalogs of sprite-tools and the lookup helpers.
//
// Catalogs are flat key -> string JSON files embedded from locales/*.json.
// English ("en") is the default language and the fallback for missing keys.
// See README.md in this folder for how to add a language.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync/atomic"
)

// DefaultLanguage is used until SetLanguage is called. The OS locale is deliberately not consulted.
const DefaultLanguage = "en"

// MetaName is the reserved catalog key that holds a language's own name (e.g. "한국어").
const MetaName = "_meta.name"

//go:embed locales/*.json
var localeFS embed.FS

// LangInfo describes an available language. Name is written in that language itself.
type LangInfo struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type catalog map[string]string

type state struct {
	code string
	cat  catalog
}

var (
	catalogs = loadCatalogs()
	current  atomic.Pointer[state]
)

func init() {
	current.Store(&state{code: DefaultLanguage, cat: catalogs[DefaultLanguage]})
}

func loadCatalogs() map[string]catalog {
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		panic("i18n: " + err.Error())
	}
	out := make(map[string]catalog, len(entries))
	for _, e := range entries {
		data, err := localeFS.ReadFile(path.Join("locales", e.Name()))
		if err != nil {
			panic("i18n: " + err.Error())
		}
		var c catalog
		if err := json.Unmarshal(data, &c); err != nil {
			panic(fmt.Sprintf("i18n: %s: %v", e.Name(), err))
		}
		out[strings.TrimSuffix(e.Name(), ".json")] = c
	}
	if out[DefaultLanguage] == nil {
		panic("i18n: missing default catalog " + DefaultLanguage + ".json")
	}
	return out
}

// Normalize turns a language tag such as "ko", "ko-KR", "ko_KR.UTF-8" or "en_US@euro" into its
// lowercase base tag ("ko"). It reports false when no catalog exists for it.
func Normalize(tag string) (string, bool) {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, ".@"); i >= 0 {
		tag = tag[:i]
	}
	if i := strings.IndexAny(tag, "-_"); i >= 0 {
		tag = tag[:i]
	}
	if tag == "" {
		return "", false
	}
	_, ok := catalogs[tag]
	return tag, ok
}

// SetLanguage selects the language used by T and Errorf. Unknown tags return an error and keep the current language.
func SetLanguage(tag string) error {
	code, ok := Normalize(tag)
	if !ok {
		return fmt.Errorf("unsupported language %q (available: %s)", tag, strings.Join(codes(), ", "))
	}
	current.Store(&state{code: code, cat: catalogs[code]})
	return nil
}

// Language returns the code of the current language.
func Language() string { return current.Load().code }

// Languages lists the available languages sorted by code.
func Languages() []LangInfo {
	list := make([]LangInfo, 0, len(catalogs))
	for _, code := range codes() {
		name := catalogs[code][MetaName]
		if name == "" {
			name = code
		}
		list = append(list, LangInfo{Code: code, Name: name})
	}
	return list
}

func codes() []string {
	list := make([]string, 0, len(catalogs))
	for code := range catalogs {
		list = append(list, code)
	}
	sort.Strings(list)
	return list
}

// T returns the text for key in the current language, falling back to English and then to the key itself.
// When args are given the text is used as a fmt format string; the verbs of every language
// keep the same order, so callers pass the same arguments for all languages.
func T(key string, args ...any) string {
	s, ok := current.Load().cat[key]
	if !ok {
		if s, ok = catalogs[DefaultLanguage][key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Error is a localised error. The message is rendered when the error is created.
type Error struct {
	Key   string // catalog key
	msg   string
	cause error
}

func (e *Error) Error() string { return e.msg }

// Unwrap returns the error given to Wrap (nil for Errorf).
func (e *Error) Unwrap() error { return e.cause }

// Errorf creates an error whose message is T(key, args...) in the current language.
func Errorf(key string, args ...any) error {
	return &Error{Key: key, msg: T(key, args...)}
}

// Wrap is like Errorf but the returned error also unwraps to cause (for errors.Is / errors.As).
func Wrap(cause error, key string, args ...any) error {
	return &Error{Key: key, msg: T(key, args...), cause: cause}
}
