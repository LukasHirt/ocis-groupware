import owncloudersConfig from '@ownclouders/eslint-config'

export default [
  ...owncloudersConfig,
  {
    ignores: ['dist/', 'coverage/', 'node_modules/']
  },
  {
    files: ['**/*.{ts,vue}'],
    rules: {
      // oCIS Web provides vue, pinia, luxon, vue3-gettext, web-pkg and
      // web-client to the application at run time, but not vue-router. A
      // value import from it would bundle a second router; only its types
      // may be used. Routing goes through the composables of web-pkg.
      '@typescript-eslint/no-restricted-imports': [
        'error',
        {
          paths: [
            {
              name: 'vue-router',
              message: 'Import types only; use the router composables of @ownclouders/web-pkg.',
              allowTypeImports: true
            }
          ]
        }
      ]
    }
  }
]
