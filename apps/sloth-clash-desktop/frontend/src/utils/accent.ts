// Custom accent color: validation, per-theme derivation of the --accent
// variable family, and a WCAG-based contrast guard. Pure functions only —
// DOM application lives in App.tsx.

export type AccentTheme = 'dark' | 'light'

// Alphas mirror the stock stylesheet recipe (App.css --accent-dim/--accent-muted).
const ACCENT_ALPHAS: Record<AccentTheme, { dim: number; muted: number }> = {
  dark: { dim: 0.24, muted: 0.42 },
  light: { dim: 0.18, muted: 0.34 },
}

// Mid-stop of each theme's background gradient in App.css — the surface most
// accent-colored elements sit on, used as the contrast reference.
const THEME_BG: Record<AccentTheme, string> = {
  dark: '#1a1918',
  light: '#e9dec8',
}

// Below this ratio against the theme background the accent is auto-adjusted.
export const MIN_ACCENT_CONTRAST = 3

// Built-in bronze accents from App.css — used as picker/preview fallbacks.
export const STOCK_ACCENTS: Record<AccentTheme, string> = {
  dark: '#c9a86c',
  light: '#936b38',
}

/** `#rgb`/`#rrggbb` (case-insensitive) → normalized `#rrggbb`, else null. */
export function normalizeHex(input: string | null | undefined): string | null {
  if (typeof input !== 'string') return null
  const v = input.trim().toLowerCase()
  if (/^#[0-9a-f]{6}$/.test(v)) return v
  if (/^#[0-9a-f]{3}$/.test(v)) {
    return `#${v[1]}${v[1]}${v[2]}${v[2]}${v[3]}${v[3]}`
  }
  return null
}

type Rgb = { r: number; g: number; b: number }

function hexToRgb(hex: string): Rgb {
  return {
    r: Number.parseInt(hex.slice(1, 3), 16),
    g: Number.parseInt(hex.slice(3, 5), 16),
    b: Number.parseInt(hex.slice(5, 7), 16),
  }
}

function rgbToHex({ r, g, b }: Rgb): string {
  const to2 = (n: number) =>
    Math.round(Math.min(255, Math.max(0, n)))
      .toString(16)
      .padStart(2, '0')
  return `#${to2(r)}${to2(g)}${to2(b)}`
}

type Hsl = { h: number; s: number; l: number }

function rgbToHsl({ r, g, b }: Rgb): Hsl {
  const rn = r / 255
  const gn = g / 255
  const bn = b / 255
  const max = Math.max(rn, gn, bn)
  const min = Math.min(rn, gn, bn)
  const l = (max + min) / 2
  if (max === min) return { h: 0, s: 0, l }
  const d = max - min
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  let h: number
  if (max === rn) h = ((gn - bn) / d + (gn < bn ? 6 : 0)) / 6
  else if (max === gn) h = ((bn - rn) / d + 2) / 6
  else h = ((rn - gn) / d + 4) / 6
  return { h, s, l }
}

function hslToRgb({ h, s, l }: Hsl): Rgb {
  if (s === 0) {
    const v = l * 255
    return { r: v, g: v, b: v }
  }
  const hue = (p: number, q: number, t0: number) => {
    let t = t0
    if (t < 0) t += 1
    if (t > 1) t -= 1
    if (t < 1 / 6) return p + (q - p) * 6 * t
    if (t < 1 / 2) return q
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6
    return p
  }
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s
  const p = 2 * l - q
  return {
    r: hue(p, q, h + 1 / 3) * 255,
    g: hue(p, q, h) * 255,
    b: hue(p, q, h - 1 / 3) * 255,
  }
}

/** WCAG relative luminance of an `#rrggbb` color, 0..1. */
export function relativeLuminance(hex: string): number {
  const chan = (v255: number) => {
    const v = v255 / 255
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
  }
  const { r, g, b } = hexToRgb(hex)
  return 0.2126 * chan(r) + 0.7152 * chan(g) + 0.0722 * chan(b)
}

/** WCAG contrast ratio between two `#rrggbb` colors, 1..21. */
export function contrastRatio(hexA: string, hexB: string): number {
  const la = relativeLuminance(hexA)
  const lb = relativeLuminance(hexB)
  const [hi, lo] = la >= lb ? [la, lb] : [lb, la]
  return (hi + 0.05) / (lo + 0.05)
}

/**
 * Contrast guard: returns `hex` unchanged when it already reads against the
 * theme background, otherwise nudges HSL lightness (lighter in dark theme,
 * darker in light theme) until MIN_ACCENT_CONTRAST is met. The stored user
 * value stays raw — only the applied color per theme is adjusted.
 */
export function ensureReadableAccent(hex: string, theme: AccentTheme): string {
  const bg = THEME_BG[theme]
  if (contrastRatio(hex, bg) >= MIN_ACCENT_CONTRAST) return hex
  const step = theme === 'dark' ? 0.02 : -0.02
  const hsl = rgbToHsl(hexToRgb(hex))
  let l = hsl.l
  let best = hex
  for (let i = 0; i < 50; i++) {
    l += step
    if (l <= 0 || l >= 1) break
    best = rgbToHex(hslToRgb({ h: hsl.h, s: hsl.s, l }))
    if (contrastRatio(best, bg) >= MIN_ACCENT_CONTRAST) return best
  }
  // Degenerate hues that never reach the ratio inside 0..1 clamp to the most
  // readable value found.
  return best
}

export type AccentVars = {
  accent: string
  dim: string
  muted: string
}

/**
 * Full variable family for one theme from a single normalized hex. Applies the
 * contrast guard, then derives dim/muted with the stock alpha recipe.
 */
export function deriveAccentVars(hex: string, theme: AccentTheme): AccentVars {
  const applied = ensureReadableAccent(hex, theme)
  const { r, g, b } = hexToRgb(applied)
  const { dim, muted } = ACCENT_ALPHAS[theme]
  return {
    accent: applied,
    dim: `rgba(${r}, ${g}, ${b}, ${dim})`,
    muted: `rgba(${r}, ${g}, ${b}, ${muted})`,
  }
}

/**
 * Single accent source resolver — the priority contract shared with the
 * upcoming brand-headers change: user custom hex > brand hex > null (null =
 * stylesheet defaults). Inputs are normalized; invalid values act as absent.
 */
export function resolveAccent(sources: {
  userHex?: string | null
  brandHex?: string | null
}): string | null {
  return normalizeHex(sources.userHex) ?? normalizeHex(sources.brandHex)
}
