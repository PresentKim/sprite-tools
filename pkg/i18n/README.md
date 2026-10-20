# i18n

All user-facing text of the Go side (CLI, shared packages, GUI backend messages) lives in
language catalogs under `locales/`. English (`en`) is the default language and the fallback.
The OS locale is never auto-detected.

## Using it

```go
i18n.T("msg.reduce.result", 29, 20)      // "Colors 29 → 20"
i18n.Errorf("err.trim.pad", pad)         // error whose text is localised when it is created
i18n.Wrap(cause, "err.pack.output_exists", path) // same, and errors.Is(err, cause) works
common.Skip("skip.upscale.empty")        // a "skipped" error (not a failure)
i18n.SetLanguage("ko")                   // also accepts "ko-KR", "ko_KR.UTF-8"; unknown -> error
i18n.Language()                          // "ko"
i18n.Languages()                         // [{en English} {ko 한국어}]
```

Lookup order: current language, then `en`, then the key itself.
The CLI picks the language with `--lang <code>` (before or after the command) or the
`SPRITE_TOOLS_LANG` environment variable; the flag wins over the variable, the default is `en`.

## Key conventions

Keys are flat and namespaced; the catalogs are sorted by key.

| Prefix  | Used for |
|---------|----------|
| `cli.*` | CLI help, progress and summary lines |
| `cmd.<name>.summary/usage/details` | one-line summary, argument form and long help of each command |
| `flag.*` | help text of command-line flags |
| `msg.*` | per-file result messages and progress output |
| `skip.*` | reasons a file was skipped (`common.Skip`) |
| `err.*` | error messages (`err.cli.*` usage errors, `err.gui.*` GUI backend errors) |
| `gui.*` | native GUI dialogs (titles, file filters, close confirmation) |
| `_meta.name` | the language's own name, shown in language lists (required) |

## Placeholders

Values use Go `fmt` verbs (`"Colors %d → %d"`). **The verbs must appear in the same order in
every language**, because the code passes the arguments once. Reword the sentence around them
instead of reordering (e.g. English `"Source colors: %d, replaced: %d"` instead of
"Replaced %d of %d"). Use `%%` for a literal percent sign. Values must not end with a newline.
Texts printed without arguments are used as-is (no `%%` unescaping).

## Adding a language

1. Copy `locales/en.json` to `locales/xx.json` (`xx` = lowercase base language code, e.g. `fr`).
2. Translate the values. Keep every key, keep the verb order, and set `_meta.name` to the
   language's name written in that language.
3. Keep the keys sorted. Run `go test ./pkg/i18n/` (and `go test ./...`): the tests check that
   the key set equals `en.json`, that the verb sequence of every value matches English, that
   the file is sorted valid UTF-8, and that every key used in the Go sources exists.
4. Nothing else to register: files in `locales/` are embedded automatically.

Some values are padded for column alignment in the console (`cli.run.label.*`) or contain
aligned example blocks (`cmd.*.details`); keep that layout.

TODO (phase 2): the GUI front end has its own catalog; document how to add a language there
once it exists.
