/**
 * Build multi-size Windows icon.ico from build/appicon.png (Wails reads build/windows/icon.ico).
 * Used for: window title bar / .exe syso resources / NSIS installer header icon (MUI_ICON).
 * macOS .icns and Linux icons are produced by Wails from the same build/appicon.png at package time.
 * Tray: see scripts/sync-desktop-packaging.mjs (copies appicon → trayicons/sloth.png for future TrayMenu).
 */
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import fs from 'node:fs/promises'

import sharp from 'sharp'
import toIco from 'to-ico'

import { log_info, log_success } from './utils.mjs'

const SIZES = [16, 20, 24, 32, 40, 48, 64, 128, 256]

export async function generateWindowsIcon() {
  const root = path.join(process.cwd(), 'apps', 'sloth-clash-desktop', 'build')
  const srcPng = path.join(root, 'appicon.png')
  const outIco = path.join(root, 'windows', 'icon.ico')

  try {
    await fs.access(srcPng)
  } catch {
    log_info(
      '[icon] skip: apps/sloth-clash-desktop/build/appicon.png not found',
    )
    return
  }

  await fs.mkdir(path.dirname(outIco), { recursive: true })

  const bufs = await Promise.all(
    SIZES.map((s) =>
      sharp(srcPng)
        .resize(s, s, {
          fit: 'contain',
          background: { r: 0, g: 0, b: 0, alpha: 0 },
        })
        .png()
        .toBuffer(),
    ),
  )

  const ico = await toIco(bufs)
  await fs.writeFile(outIco, ico)
  log_success(
    `[icon] wrote ${outIco} (${SIZES.join(', ')} px from appicon.png)`,
  )
}

const selfName = path.basename(fileURLToPath(import.meta.url))
const entryName = path.basename(path.resolve(process.argv[1] || ''))
if (entryName === selfName && selfName === 'generate-windows-icon.mjs') {
  generateWindowsIcon().catch((err) => {
    console.error('[icon] failed:', err)
    process.exit(1)
  })
}
