# Translating the web application

The web application ships in English. Every text the user sees goes through
`$gettext` from vue3-gettext, so adding a language means supplying a catalog
and compiling it. No code changes.

## How it fits together

- `web/l10n/template.pot` lists every English message. It is generated from
  the source and committed, without file and line references, so editing a
  file only changes it when a message changes.
- `web/l10n/<language>/app.po` is the catalog of one language.
- `web/src/l10n/translations.json` is the compiled form of all catalogs. It is
  committed, and `web/src/index.ts` hands it to oCIS Web, which merges it into
  its own catalog and picks the language the user chose in their account
  settings. A message without a translation shows the English text.

The three commands are npm scripts in `web/`:

```sh
npm run l10n:extract   # rewrites l10n/template.pot from the source
npm run l10n:compile   # rewrites src/l10n/translations.json from l10n/*/app.po
npm run l10n:check     # fails when either file is out of date
```

`npm run check`, which CI runs, includes `l10n:check`. A change that adds or
edits a message therefore needs `npm run l10n:extract` in the same commit.

## Adding a language

1. Pick the language code oCIS Web uses for it, such as `de` or `fr`. These
   are the codes in the language menu of the account page.
2. Copy the template: `mkdir web/l10n/de && cp web/l10n/template.pot web/l10n/de/app.po`.
3. Add a `Language` line to the header of the new file:

   ```
   "Content-Type: text/plain; charset=UTF-8\n"
   "Language: de\n"
   ```

4. Fill in the `msgstr` of each message. Keep placeholders such as `%{name}`
   unchanged. Tools like Poedit or Weblate read the file as it is.
5. Run `npm run l10n:compile` and commit the `.po` file together with the
   updated `translations.json`.

To see the result, rebuild the application, sign in to oCIS Web and choose the
language on the account page.

## Updating a language after the source changed

Run `npm run l10n:extract`, then merge the new template into the catalog with
`msgmerge --update l10n/de/app.po l10n/template.pot` (part of GNU gettext), or
let the translation tool do it. Messages that are not translated yet keep an
empty `msgstr`, and `l10n:compile` skips them. Fuzzy and obsolete entries are
skipped as well.

## Writing messages

- Use a literal string: `$gettext('Create')`. The extractor cannot read a
  message that is built at run time or passed in as a variable.
- Put values in placeholders and pass them as the second argument:
  `$gettext('Revoke %{name}', { name })`. Never join translated pieces.
- Plurals are not used yet. When one is needed, use `$ngettext`, which the
  extractor reads too.
