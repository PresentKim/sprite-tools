# Adding a language

All user-facing text lives in language catalogs, so adding a language means adding JSON files and
no code changes. English (`en`) is the default and the fallback; the OS language is never detected.

A language needs **two** catalogs with the same language code `xx` (the lowercase base code, e.g. `fr`, `ja`, `de`):

| Catalog | Covers | Placeholders |
|---------|--------|--------------|
| `pkg/i18n/locales/xx.json` | CLI help and output, processing messages, errors, native GUI dialogs | Go `fmt` verbs: `%d`, `%s`, `%v` ... |
| `gui/frontend/src/i18n/locales/xx.json` | everything drawn by the GUI front end | `{name}` |

Both folders are discovered automatically (the Go catalogs are embedded, the front end loads
`locales/*.json` with `import.meta.glob`). Nothing has to be registered anywhere.

## Steps

1. Copy `pkg/i18n/locales/en.json` to `pkg/i18n/locales/xx.json` and translate the values.
2. Copy `gui/frontend/src/i18n/locales/en.json` to `gui/frontend/src/i18n/locales/xx.json` and translate the values.
3. Set `"_meta.name"` in **both** files to the language's own name, written in that language
   (`English`, `한국어`, `Français` ...). It is what the language selector shows, and it must be
   identical in the two files.
4. Keep every key, keep the keys **sorted** (byte order) and the files valid UTF-8 without a BOM.
5. Run the tests (see below), then try the language in the app.

Never rename or remove keys. If a key is missing in your language the English text is shown, but the
tests treat a missing key as an error.

## Go catalog (`pkg/i18n/locales/xx.json`)

- Values use Go `fmt` verbs: `"Colors %d → %d"`. **The verbs must appear in the same order as in English**,
  because the code passes the arguments once. If your grammar wants another order, reword the sentence
  around the verbs instead of reordering them. Use `%%` for a literal percent sign.
- Values must not end with a newline. Texts printed without arguments are used as they are (no `%%` unescaping).
- Some values are padded for column alignment in the console (`cli.run.label.*`) or contain aligned example
  blocks (`cmd.*.details`); keep that layout.
- More background (key prefixes, conventions) is in [pkg/i18n/README.md](../pkg/i18n/README.md).

## Front-end catalog (`gui/frontend/src/i18n/locales/xx.json`)

- Placeholders are `{name}`: `"Showing {shown} of {total}"`. They may appear in any order, but the set of
  placeholders must be the same as in English. Do not translate the names inside the braces.
- **Plural forms.** Keys ending in `.one` / `.other` come in pairs and are chosen with the language's
  plural rules (`Intl.PluralRules`); `{n}` is the number. Always provide both. A language with more
  plural categories may add `.zero`, `.two`, `.few` or `.many` forms next to them. A language without plurals
  (Korean, Japanese) simply uses the same text for both.
- **Help overlay (F1).** Keys under `help.*` may contain `<kbd>` tags (`<kbd>Ctrl</kbd>+wheel`). Keep the tags
  and translate only the words.
- Keys that mention a button or menu name (for example `help.batch.dblclick` mentions 'Show in folder') should use
  the same wording as your translation of that button.
- Numbers are not locale-formatted; the sentences simply contain `{n}`.
- Some texts are the name of a keyboard key or a file name (`Ctrl+Enter`, `.sprite-tools-backup`); leave those alone.

## What the tests check

```
go test ./pkg/i18n/                # Go catalogs: same keys as en.json, same verb sequence, sorted, valid UTF-8,
                                   # keys used in the Go sources exist
cd gui && go test .                # front-end catalogs (see below) and the language lists
go test ./...                      # everything in the root module
```

`gui/frontend_i18n_test.go` checks that

- every front-end catalog has the same key set as `en.json` (plural forms aside), the same `{placeholders}` per key,
  a `_meta.name`, sorted keys and valid UTF-8;
- every key used by `index.html` (`data-i18n*` attributes) and by the JavaScript (`t('...')`, `tn('...')` ...)
  exists in `en.json`, and no catalog key is unused;
- no Hangul is left in `index.html`, `src/**/*.js` or `src/**/*.css` outside comments (user-facing text must come
  from the catalogs). Comments are stripped by a small scanner that understands strings, template literals and
  regular expression literals; a regex literal directly after a keyword such as `return /x/` is the one case it
  cannot tell from a division;
- the front end and the Go side offer the same languages with the same `_meta.name`. A language that exists on
  one side only is an error, because it would be half translated.

## Trying it in the app

- CLI: `go run . --lang xx help`, or set `SPRITE_TOOLS_LANG=xx`.
- GUI: `cd gui && wails dev -noreload`, open the language selector in the top bar and pick your language.
  The selector lists the languages that have a front-end catalog. On selection the front end tells the Go side
  (`SetLanguage`) and re-renders; the preview messages, errors and native dialogs then come from the Go catalog
  of the same code. If the Go side has no catalog for the code, the Go messages stay English.
  Check both modes (Batch and Editor), the menus, the dialogs, the F1 help, the toasts, and look for text that
  is cut off or overflows, because translations are often longer than English.

## Known limitations

- Messages that were already produced by Go (for example a file row's result text) keep their old language
  until the file is processed again. The previews are recomputed on every language switch.
- An untitled editor document keeps the name it was created with ("Untitled.png") when the language changes.
- Backup folder names (`.sprite-tools-backup/<time>`) and file names are never translated.
