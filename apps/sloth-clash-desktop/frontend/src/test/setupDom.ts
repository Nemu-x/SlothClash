// Setup for the `dom` vitest project (*.test.tsx, jsdom environment).
import '@testing-library/jest-dom/vitest'

import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

import i18n from '../i18n'

import { installWailsMock, wailsApp } from './wailsMock'

installWailsMock()

// Tests assert English strings regardless of the host's locale.
await i18n.changeLanguage('en')

afterEach(() => {
  cleanup()
  wailsApp.reset()
})
