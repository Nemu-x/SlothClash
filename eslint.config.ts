import fs from 'node:fs'
import path from 'node:path'

import eslintJS from '@eslint/js'
import eslintReact from '@eslint-react/eslint-plugin'
import { defineConfig } from 'eslint/config'
import { createTypeScriptImportResolver } from 'eslint-import-resolver-typescript'
import pluginImportX from 'eslint-plugin-import-x'
import pluginReactCompiler from 'eslint-plugin-react-compiler'
import pluginReactHooks from 'eslint-plugin-react-hooks'
import pluginReactRefresh from 'eslint-plugin-react-refresh'
import pluginUnusedImports from 'eslint-plugin-unused-imports'
import globals from 'globals'
import tseslint from 'typescript-eslint'

const importResolverProjects = ['./tsconfig.json']
const desktopFrontendTsconfig = path.join(
  process.cwd(),
  'apps/sloth-clash-desktop/frontend/tsconfig.json',
)
if (fs.existsSync(desktopFrontendTsconfig)) {
  importResolverProjects.push(
    './apps/sloth-clash-desktop/frontend/tsconfig.json',
  )
}

export default defineConfig([
  {
    name: 'global-ignores',
    ignores: [
      '**/node_modules/**',
      '**/dist/**',
      '**/build/bin/**',
      '**/wailsjs/**',
      '**/.eslintcache',
    ],
  },
  {
    name: 'vite-config-no-react',
    files: [
      'vite.config.mts',
      'apps/sloth-clash-desktop/frontend/vite.config.ts',
    ],
    plugins: {
      js: eslintJS,
      'import-x': pluginImportX,
    },
    extends: [
      eslintJS.configs.recommended,
      tseslint.configs.disableTypeChecked,
    ],
    languageOptions: {
      globals: globals.node,
      parserOptions: {
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
    rules: {
      'import-x/no-unresolved': 'off',
    },
  },
  {
    files: ['**/*.{js,mjs,cjs,ts,mts,cts,jsx,tsx}'],
    ignores: [
      'scripts/**/*.mjs',
      'tests/required/**/*.mjs',
      'vite.config.mts',
      'apps/sloth-clash-desktop/frontend/vite.config.ts',
    ],

    plugins: {
      js: eslintJS,
      // @ts-expect-error -- https://github.com/typescript-eslint/typescript-eslint/issues/11543
      'react-hooks': pluginReactHooks,
      'react-compiler': pluginReactCompiler,
      'import-x': pluginImportX,
      'react-refresh': pluginReactRefresh,
      'unused-imports': pluginUnusedImports,
    },

    extends: [
      eslintJS.configs.recommended,
      tseslint.configs.recommended,
      eslintReact.configs['recommended-typescript'],
    ],

    languageOptions: {
      globals: globals.browser,
      parserOptions: {
        projectService: {
          allowDefaultProject: ['src/polyfills/*.js', 'tests/required/*.mjs'],
          noWarnOnMultipleProjects: true,
        },
      },
    },

    settings: {
      react: {
        version: 'detect',
      },
      'import-x/resolver-next': [
        createTypeScriptImportResolver({
          project: importResolverProjects,
        }),
      ],
    },

    rules: {
      // React
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'error',
      'react-compiler/react-compiler': 'error',
      'react-refresh/only-export-components': [
        'warn',
        { allowConstantExport: true },
      ],

      '@eslint-react/no-forward-ref': 'off',

      // React performance and production quality rules
      '@eslint-react/no-array-index-key': 'warn',
      '@eslint-react/no-children-count': 'error',
      '@eslint-react/no-children-for-each': 'error',
      '@eslint-react/no-children-map': 'error',
      '@eslint-react/no-children-only': 'error',
      '@eslint-react/jsx-no-children-prop': 'error',
      '@eslint-react/no-children-to-array': 'error',
      '@eslint-react/no-class-component': 'error',
      '@eslint-react/no-clone-element': 'error',
      '@eslint-react/no-create-ref': 'error',
      '@eslint-react/no-direct-mutation-state': 'error',
      '@eslint-react/no-implicit-key': 'error',
      '@eslint-react/no-set-state-in-component-did-mount': 'error',
      '@eslint-react/no-set-state-in-component-did-update': 'error',
      '@eslint-react/no-set-state-in-component-will-update': 'error',
      '@eslint-react/no-unstable-context-value': 'warn',
      '@eslint-react/no-unstable-default-props': 'warn',
      '@eslint-react/no-unused-class-component-members': 'error',
      '@eslint-react/no-unused-state': 'error',
      '@eslint-react/jsx-no-useless-fragment': 'warn',
      '@eslint-react/prefer-destructuring-assignment': 'warn',

      // TypeScript
      '@typescript-eslint/no-explicit-any': 'off',

      // unused-imports 代替 no-unused-vars
      '@typescript-eslint/no-unused-vars': 'off',
      'unused-imports/no-unused-imports': 'error',
      'unused-imports/no-unused-vars': [
        'warn',
        {
          vars: 'all',
          varsIgnorePattern: '^_',
          args: 'after-used',
          argsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^ignore',
        },
      ],

      // Import
      'import-x/no-unresolved': 'error',
      'import-x/order': [
        'warn',
        {
          groups: [
            'builtin',
            'external',
            'internal',
            'parent',
            'sibling',
            'index',
          ],
          'newlines-between': 'always',
          alphabetize: {
            order: 'asc',
            caseInsensitive: true,
          },
        },
      ],

      // 其他常见
      'prefer-const': 'warn',
      'no-case-declarations': 'error',
      'no-fallthrough': 'error',
      'no-empty': ['warn', { allowEmptyCatch: true }],
    },
  },
  {
    files: ['apps/sloth-clash-desktop/frontend/src/App.tsx'],
    rules: {
      // Large UI shell: relax until split/refactor (blocks lint-staged otherwise).
      'import-x/no-unresolved': 'off',
      'react-compiler/react-compiler': 'off',
      '@eslint-react/unsupported-syntax': 'off',
      '@eslint-react/set-state-in-effect': 'off',
      '@eslint-react/exhaustive-deps': 'off',
      '@eslint-react/web-api-no-leaked-timeout': 'off',
      'react-hooks/exhaustive-deps': 'off',
    },
  },
  {
    files: ['scripts/**/*.mjs'],
    extends: [eslintJS.configs.recommended],
    languageOptions: {
      globals: globals.node,
      parserOptions: {
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
  },
  {
    files: ['tests/required/**/*.mjs'],
    extends: [eslintJS.configs.recommended],
    languageOptions: {
      globals: globals.node,
      parserOptions: {
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
  },
])
