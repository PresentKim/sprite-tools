package main

import "sprite-tools/pkg/i18n"

// Display language: Go-side messages (errors, processing results, native dialogs) follow the pkg/i18n catalog.
// The default language is en; the frontend calls SetLanguage when it starts with its saved settings.

// SetLanguage changes the display language ("en", "ko", "ko-KR", etc.). An unsupported code returns an error and leaves the language unchanged.
func (a *App) SetLanguage(code string) error {
	return i18n.SetLanguage(code)
}

// Language returns the code of the current display language ("en", "ko", ...).
func (a *App) Language() string {
	return i18n.Language()
}

// Languages lists the available languages. Name is each language's own spelling (e.g. "English").
func (a *App) Languages() []i18n.LangInfo {
	return i18n.Languages()
}
