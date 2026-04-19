/**
 * `build/` is gitignored; CI must seed `build/appicon.png` before Wails / icon scripts.
 * Source of truth: tracked `docs/appicon.png` (same artwork as site/docs).
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(__dirname, '..')
const src = path.join(repoRoot, 'docs', 'appicon.png')
const dest = path.join(
  repoRoot,
  'apps',
  'sloth-clash-desktop',
  'build',
  'appicon.png',
)

if (!fs.existsSync(src)) {
  console.error('[copy-desktop-appicon] missing:', src)
  process.exit(1)
}
fs.mkdirSync(path.dirname(dest), { recursive: true })
fs.copyFileSync(src, dest)
console.log(
  '[copy-desktop-appicon]',
  path.relative(repoRoot, src),
  '→',
  path.relative(repoRoot, dest),
)
