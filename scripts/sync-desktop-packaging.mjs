/**
 * Copies versioned packaging files into Wails `build/` (which is gitignored).
 * Run before `wails build` (see scripts/wails.mjs and pnpm desktop:resources).
 *
 * - NSIS: multilingual project.nsi for Windows installer strings + language picker.
 * - trayicons: Wails reads tray bitmaps from `<app>/trayicons/*.png` at build time when you use TrayMenu.
 *   We mirror build/appicon.png → trayicons/sloth.png (gitignored). The native macOS tray embeds
 *   tracked trayicons/mono.png via go:embed (see darwin_tray_mono_embed.go).
 */
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import fs from 'node:fs'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

function findRepoRoot() {
  let d = process.cwd()
  for (let i = 0; i < 8; i++) {
    const marker = path.join(d, 'apps', 'sloth-clash-desktop', 'wails.json')
    if (fs.existsSync(marker)) return d
    const p = path.dirname(d)
    if (p === d) break
    d = p
  }
  return path.resolve(__dirname, '..')
}

const repoRoot = findRepoRoot()
const appDir = path.join(repoRoot, 'apps', 'sloth-clash-desktop')
const srcNsi = path.join(appDir, 'packaging', 'windows', 'project.nsi')
const destNsi = path.join(
  appDir,
  'build',
  'windows',
  'installer',
  'project.nsi',
)
// PowerShell helper invoked by sloth.vcRedistRuntime macro. Lives next to
// project.nsi so the NSIS `File` directive picks it up at compile time.
const srcVcPs1 = path.join(appDir, 'packaging', 'windows', 'vc_install.ps1')
const destVcPs1 = path.join(
  appDir,
  'build',
  'windows',
  'installer',
  'vc_install.ps1',
)
const appIcon = path.join(appDir, 'build', 'appicon.png')
const trayDir = path.join(appDir, 'trayicons')
const trayPng = path.join(trayDir, 'sloth.png')

function main() {
  if (!fs.existsSync(srcNsi)) {
    console.error('[sync-desktop-packaging] missing:', srcNsi)
    process.exit(1)
  }
  fs.mkdirSync(path.dirname(destNsi), { recursive: true })
  // NSIS + non-ASCII strings (RU/ZH): write BOM to avoid mojibake on some setups.
  const nsiText = fs.readFileSync(srcNsi, 'utf8')
  const bom = '\uFEFF'
  fs.writeFileSync(destNsi, `${bom}${nsiText}`, 'utf8')
  console.log(
    '[sync-desktop-packaging] copied project.nsi →',
    path.relative(repoRoot, destNsi),
  )

  if (fs.existsSync(srcVcPs1)) {
    fs.copyFileSync(srcVcPs1, destVcPs1)
    console.log(
      '[sync-desktop-packaging] copied vc_install.ps1 →',
      path.relative(repoRoot, destVcPs1),
    )
  } else {
    console.error(
      '[sync-desktop-packaging] missing vc_install.ps1 — VC runtime gate in installer will not work:',
      srcVcPs1,
    )
  }

  if (fs.existsSync(appIcon)) {
    fs.mkdirSync(trayDir, { recursive: true })
    fs.copyFileSync(appIcon, trayPng)
    console.log(
      '[sync-desktop-packaging] tray icon →',
      path.relative(repoRoot, trayPng),
      '(for future Wails TrayMenu Image: "sloth")',
    )
  } else {
    console.log(
      '[sync-desktop-packaging] skip trayicons (no',
      path.relative(repoRoot, appIcon),
      'yet — run prebuild first)',
    )
  }
}

main()
