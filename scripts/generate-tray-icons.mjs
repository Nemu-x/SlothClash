/**
 * Build the state-aware tray icons (idle / proxy / tun × colorful / mono).
 *
 * The tray shows which traffic path is live, like the other Clash clients do:
 *   idle   → plain icon (disconnected / connecting)
 *   proxy  → green dot badge   (connected, system proxy set)
 *   tun    → blue ring badge   (connected, TUN adapter up)
 * Dot vs ring is deliberate: on macOS the mono variant is a template image
 * (alpha only, colour is ignored), so the two connected states must differ
 * in *shape*, not just colour. The colourful variant keeps both cues.
 *
 * Outputs (all generated, all gitignored, embedded via go:embed):
 *   apps/sloth-clash-desktop/build/windows/tray/<style>-<state>.ico   (Windows, 16…64 px)
 *   apps/sloth-clash-desktop/trayicons/state/<style>-<state>.png      (macOS, 44 px = 22 pt @2x)
 *
 * Sources: build/appicon.png (colourful, seeded by copy:desktop-appicon) and
 * the tracked trayicons/mono.png (mono glyph, see optimize-tray-mono.mjs).
 * Runs as part of `pnpm run icons:windows` (generate-windows-icon.mjs) and the
 * required-tests gate, so any Go build that embeds these has them.
 */
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import fs from 'node:fs/promises'

import sharp from 'sharp'
import toIco from 'to-ico'

import { log_info, log_success } from './utils.mjs'

const ICO_SIZES = [16, 20, 24, 32, 48, 64]
const MAC_MAX_SIDE = 44

const STATES = ['idle', 'proxy', 'tun']
const STYLES = ['colorful', 'mono']

const COLOR_PROXY = '#34c759'
const COLOR_TUN = '#0a84ff'
// Outline that separates a coloured badge from the dark app icon.
const COLOR_BADGE_OUTLINE = '#1a1917'

function circleSvg(w, h, cx, cy, r, fill) {
  return Buffer.from(
    `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><circle cx="${cx}" cy="${cy}" r="${r}" fill="${fill}"/></svg>`,
  )
}

/**
 * Badge geometry for a canvas of `w`×`h` where the badge sits bottom-right.
 * Scaled from the longest side so 16 px Windows icons and 44 px macOS PNGs get
 * the same proportions.
 */
function badgeGeometry(w, h) {
  const s = Math.max(w, h)
  const r = Math.max(3, Math.round(s * 0.21))
  const inset = Math.max(0, Math.round(s * 0.02))
  const cx = w - r - inset
  const cy = h - r - inset
  const gap = Math.max(1, Math.round(s * 0.06))
  const hole = Math.max(1, Math.round(r * 0.45))
  return { r, cx, cy, gap, hole }
}

/**
 * Compose the state badge onto a square-or-not base PNG buffer.
 * - knock a transparent gap around the badge (reads on template images and on
 *   busy backgrounds alike)
 * - draw the badge (dot for proxy, ring for tun)
 */
async function applyBadge(basePng, state, style) {
  if (state === 'idle') return basePng
  const meta = await sharp(basePng).metadata()
  const w = meta.width
  const h = meta.height
  const { r, cx, cy, gap, hole } = badgeGeometry(w, h)

  const fill =
    style === 'mono' ? '#ffffff' : state === 'tun' ? COLOR_TUN : COLOR_PROXY

  const layers = [
    // Transparent gap around the badge.
    { input: circleSvg(w, h, cx, cy, r + gap, '#000000'), blend: 'dest-out' },
  ]
  if (style === 'colorful') {
    // Thin dark outline under the coloured badge so it never fuses with the
    // sloth's fur or the rounded-square background.
    layers.push({
      input: circleSvg(
        w,
        h,
        cx,
        cy,
        r + Math.max(1, Math.round(gap / 2)),
        COLOR_BADGE_OUTLINE,
      ),
      blend: 'over',
    })
  }
  layers.push({ input: circleSvg(w, h, cx, cy, r, fill), blend: 'over' })
  if (state === 'tun') {
    layers.push({
      input: circleSvg(w, h, cx, cy, hole, '#000000'),
      blend: 'dest-out',
    })
  }

  // SVG layers are rendered at exactly the base dimensions (sharp refuses a
  // composite larger than its canvas, and the mono glyph is not square).
  return sharp(basePng)
    .composite(layers.map((l) => ({ ...l, left: 0, top: 0 })))
    .png()
    .toBuffer()
}

async function squareResize(srcPng, size) {
  return sharp(srcPng)
    .resize(size, size, {
      fit: 'contain',
      background: { r: 0, g: 0, b: 0, alpha: 0 },
    })
    .png()
    .toBuffer()
}

export async function generateTrayIcons() {
  const appDir = path.join(process.cwd(), 'apps', 'sloth-clash-desktop')
  const colorSrc = path.join(appDir, 'build', 'appicon.png')
  const monoSrc = path.join(appDir, 'trayicons', 'mono.png')
  const outWin = path.join(appDir, 'build', 'windows', 'tray')
  const outMac = path.join(appDir, 'trayicons', 'state')

  const sources = { colorful: colorSrc, mono: monoSrc }
  for (const [style, p] of Object.entries(sources)) {
    try {
      await fs.access(p)
    } catch {
      throw new Error(
        `[tray-icons] missing ${style} source: ${path.relative(process.cwd(), p)}`,
      )
    }
  }

  await fs.mkdir(outWin, { recursive: true })
  await fs.mkdir(outMac, { recursive: true })

  for (const style of STYLES) {
    const src = await fs.readFile(sources[style])
    for (const state of STATES) {
      // Windows: multi-size .ico, every layer square.
      const layers = []
      for (const s of ICO_SIZES) {
        const base = await squareResize(src, s)
        layers.push(await applyBadge(base, state, style))
      }
      await fs.writeFile(
        path.join(outWin, `${style}-${state}.ico`),
        await toIco(layers),
      )

      // macOS: one PNG at 44 px max side (22 pt @2x), aspect preserved so the
      // mono glyph keeps the metrics optimize-tray-mono.mjs established.
      const macBase = await sharp(src)
        .resize({
          width: MAC_MAX_SIDE,
          height: MAC_MAX_SIDE,
          fit: 'inside',
          withoutEnlargement: false,
        })
        .png()
        .toBuffer()
      await fs.writeFile(
        path.join(outMac, `${style}-${state}.png`),
        await applyBadge(macBase, state, style),
      )
    }
  }
  log_success(
    `[tray-icons] wrote ${STYLES.length * STATES.length} .ico → ${path.relative(process.cwd(), outWin)} and ${STYLES.length * STATES.length} .png → ${path.relative(process.cwd(), outMac)}`,
  )
}

const selfName = path.basename(fileURLToPath(import.meta.url))
const entryName = path.basename(path.resolve(process.argv[1] || ''))
if (entryName === selfName && selfName === 'generate-tray-icons.mjs') {
  generateTrayIcons().catch((err) => {
    log_info(String(err?.stack || err))
    process.exit(1)
  })
}
