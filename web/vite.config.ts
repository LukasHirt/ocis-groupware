import { writeFileSync } from 'node:fs'
import { defineConfig } from '@ownclouders/extension-sdk'

// dist is embedded by the Go build and must exist in git, so it holds a
// placeholder. The build empties the directory first; this puts the
// placeholder back, so a local build never shows up as a deleted file.
const keepPlaceholder = {
  name: 'keep-placeholder',
  closeBundle() {
    writeFileSync('dist/.gitkeep', '')
  }
}

// The SDK builds an AMD module with vue, pinia, luxon, vue3-gettext, web-pkg
// and web-client as externals, which oCIS Web provides at run time. The
// output names are overridden so that the entry keeps a fixed name, which
// the server serves with no-cache, while chunks and assets carry a content
// hash and are cached for good. The runtime takes the module's value as the
// application script, so the entry must have only a default export, which
// exports: 'default' enforces at build time.
export default defineConfig({
  name: 'groupware',
  plugins: [keepPlaceholder],
  build: {
    rollupOptions: {
      output: {
        exports: 'default',
        entryFileNames: 'groupware.js',
        chunkFileNames: 'chunks/[name]-[hash].mjs',
        assetFileNames: 'assets/[name]-[hash][extname]'
      }
    }
  },
  test: {
    include: ['tests/unit/**/*.spec.ts']
  }
})
