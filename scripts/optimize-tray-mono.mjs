/**
 * Normalizes apps/sloth-clash-desktop/trayicons/mono.png for the macOS
 * status bar tray (template image).
 *
 * Why: drop a 2048x2048 source PNG with generous transparent margins in there
 * and macOS will scale it down on the fly. The subject reads tiny in the menu
 * bar unless you forcibly upscale the icon — which is what the old code did
 * (`tray_darwin_native.m::SlothTrayNormalisedIconSize` clamped embedded
 * mono to 30-34pt instead of the system-standard 18-22pt). That made the
 * icon visually "fat" compared to every other native menu bar item.
 *
 * What this does:
 *  1. Trims fully transparent margins (alpha threshold 1).
 *  2. Resizes the trimmed bitmap so the longest side is 44px (= 22pt @ 2x retina).
 *  3. Re-encodes as a small, compressed PNG.
 *
 * Run with `node scripts/optimize-tray-mono.mjs` after replacing the source
 * mono.png. Safe to re-run.
 */
import path from 'node:path'
import fs from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import sharp from 'sharp'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(__dirname, '..')
const target = path.join(
  repoRoot,
  'apps',
  'sloth-clash-desktop',
  'trayicons',
  'mono.png',
)

const MAX_SIDE = 44

async function main() {
  const stat = await fs.stat(target).catch(() => null)
  if (!stat) {
    console.error('[optimize-tray-mono] not found:', target)
    process.exit(1)
  }

  const before = await sharp(target).metadata()
  const buf = await sharp(target)
    .trim({ threshold: 1 })
    .resize({
      width: MAX_SIDE,
      height: MAX_SIDE,
      fit: 'inside',
      withoutEnlargement: true,
    })
    .png({ compressionLevel: 9, palette: false })
    .toBuffer()

  await fs.writeFile(target, buf)
  const after = await sharp(target).metadata()
  const beforeSize = stat.size
  const afterSize = (await fs.stat(target)).size

  console.log(
    `[optimize-tray-mono] ${before.width}x${before.height} (${beforeSize} B) → ${after.width}x${after.height} (${afterSize} B)`,
  )
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
