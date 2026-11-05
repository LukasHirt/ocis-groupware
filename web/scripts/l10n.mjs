// Translation helper. Run it through npm:
//
//   npm run l10n:extract   writes l10n/template.pot from the source
//   npm run l10n:compile   writes src/l10n/translations.json from l10n/*/app.po
//   npm run l10n:check     fails when either file is out of date
//
// See docs/translations.md.
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('..', import.meta.url))
const pot = join(root, 'l10n/template.pot')
const json = join(root, 'src/l10n/translations.json')

function run(bin, env) {
  execFileSync(join(root, 'node_modules/.bin', bin), [], {
    cwd: root,
    env: { ...process.env, ...env },
    stdio: ['ignore', 'ignore', 'inherit']
  })
}

// extract writes the template to path. The source references that the tool
// adds (file and line) are removed, so that editing a file does not change
// the template unless a message changed. The messages are sorted by id because
// the tool lists them in the order it finds the files.
function extract(path) {
  run('vue-gettext-extract', { L10N_EXTRACT: '1', L10N_POT: path })
  const text = readFileSync(path, 'utf8')
    .split('\n')
    .filter((line) => !line.startsWith('#:'))
    .join('\n')
  const [header, ...entries] = text.trimEnd().split('\n\n')
  const id = (entry) => JSON.parse(entry.match(/^msgid (.*)$/m)[1])
  entries.sort((a, b) => id(a).localeCompare(id(b)))
  writeFileSync(path, [header, ...entries].join('\n\n') + '\n')
}

function compile(path) {
  run('vue-gettext-compile', { L10N_JSON: path })
  // Sorted keys keep the file stable, and a final newline keeps editors quiet.
  const data = JSON.parse(readFileSync(path, 'utf8'))
  const sorted = Object.fromEntries(
    Object.keys(data)
      .sort()
      .map((lang) => [lang, Object.fromEntries(Object.entries(data[lang]).sort(([a], [b]) => (a < b ? -1 : 1)))])
  )
  writeFileSync(path, JSON.stringify(sorted, null, 2) + '\n')
}

const command = process.argv[2]
if (command === 'extract') {
  extract(pot)
} else if (command === 'compile') {
  compile(json)
} else if (command === 'check') {
  const dir = mkdtempSync(join(tmpdir(), 'l10n-'))
  try {
    extract(join(dir, 'template.pot'))
    compile(join(dir, 'translations.json'))
    const stale = [
      [pot, join(dir, 'template.pot'), 'npm run l10n:extract'],
      [json, join(dir, 'translations.json'), 'npm run l10n:compile']
    ].filter(([committed, fresh]) => readFileSync(committed, 'utf8') !== readFileSync(fresh, 'utf8'))
    for (const [committed, , fix] of stale) {
      console.error(`${committed} is out of date. Run \`${fix}\`.`)
    }
    process.exitCode = stale.length ? 1 : 0
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
} else {
  console.error('usage: l10n.mjs extract|compile|check')
  process.exitCode = 2
}
