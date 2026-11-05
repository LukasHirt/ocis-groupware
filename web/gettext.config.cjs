// Settings for vue3-gettext's extract and compile tools. scripts/l10n.mjs runs
// them; see docs/translations.md. Every directory l10n/<language>/ that
// holds an app.po file is a language.
const fs = require('node:fs')
const path = require('node:path')

const l10n = path.resolve(__dirname, 'l10n')
const languages = fs.existsSync(l10n)
  ? fs
      .readdirSync(l10n, { withFileTypes: true })
      .filter((d) => d.isDirectory() && fs.existsSync(path.join(l10n, d.name, 'app.po')))
      .map((d) => d.name)
      .sort()
  : []

module.exports = {
  input: { path: './src', include: ['**/*.ts', '**/*.vue'], exclude: ['l10n/**'] },
  output: {
    path: l10n,
    // The script points these at temporary files to check that the committed
    // ones are current.
    potPath: process.env.L10N_POT || path.join(l10n, 'template.pot'),
    jsonPath: process.env.L10N_JSON || path.resolve(__dirname, 'src/l10n/translations.json'),
    // Extraction must not merge into the .po files, which needs GNU gettext.
    locales: process.env.L10N_EXTRACT ? [] : languages,
    linguas: false
  }
}
