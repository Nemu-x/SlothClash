import { defineConfig, mergeConfig } from 'vitest/config'

import viteConfig from './vite.config'

// Two projects: pure-logic tests (*.test.ts) stay on the fast node environment,
// component render tests (*.test.tsx) get a jsdom DOM plus the shared setup
// (jest-dom matchers, cleanup, Wails binding mock). Both inherit the vite
// config (React plugin) via `extends: true`.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      projects: [
        {
          extends: true,
          test: {
            name: 'node',
            environment: 'node',
            include: ['src/**/*.test.ts'],
          },
        },
        {
          extends: true,
          test: {
            name: 'dom',
            environment: 'jsdom',
            include: ['src/**/*.test.tsx'],
            setupFiles: ['./src/test/setupDom.ts'],
          },
        },
      ],
    },
  }),
)
