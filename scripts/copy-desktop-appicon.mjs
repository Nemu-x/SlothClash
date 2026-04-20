/**
 * `build/` is gitignored; CI must seed `build/appicon.png` before Wails / icon scripts.
 * Source of truth: tracked `docs/appicon-main.png` (fallback: `docs/appicon.png`).
 */
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(__dirname, '..')
const envSrc = process.env.SLOTH_APPICON_SOURCE
const preferred = path.join(repoRoot, 'docs', 'appicon-main.png')
const fallback = path.join(repoRoot, 'docs', 'appicon.png')
const src = envSrc
  ? path.isAbsolute(envSrc)
    ? envSrc
    : path.join(repoRoot, envSrc)
  : fs.existsSync(preferred)
    ? preferred
    : fallback
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

let copiedRaw = true
try {
  const sharpMod = await import('sharp')
  const sharp = sharpMod.default
  // Keep original composition (no forced zoom/crop), only normalize size for macOS pipeline.
  await sharp(src).resize(1024, 1024, { fit: 'contain' }).png().toFile(dest)
  copiedRaw = false
} catch {
  // Fallback for environments where sharp native binding is unavailable.
  // On macOS we still enforce 1024px source to satisfy app-icon pipeline expectations.
  try {
    if (process.platform === 'darwin') {
      execFileSync('sips', ['-z', '1024', '1024', src, '--out', dest], {
        stdio: 'ignore',
      })
      copiedRaw = false
    } else {
      fs.copyFileSync(src, dest)
    }
  } catch {
    fs.copyFileSync(src, dest)
  }
}

console.log(
  '[copy-desktop-appicon]',
  path.relative(repoRoot, src),
  '→',
  path.relative(repoRoot, dest),
  copiedRaw ? '(raw copy)' : '(trimmed)',
)
