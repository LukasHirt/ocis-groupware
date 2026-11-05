// The SDK ships plain JavaScript without declarations. This covers the one
// function the build uses.
declare module '@ownclouders/extension-sdk' {
  import type { UserConfig } from 'vitest/config'

  export function defineConfig(
    overrides?: UserConfig & { name?: string }
  ): (env: { mode: string }) => UserConfig
}

// The icon is imported as a data URI, so no file has to be served for it.
declare module '*.svg?inline' {
  const uri: string
  export default uri
}
