/**
 * Fills core/mihomo/manifest.json asset hashes from a published core release
 * (core-<tag>-sloth.<series>, built by .github/workflows/core-build.yml).
 *
 * Trust chain: SHA256SUMS must carry a valid minisign signature by a key the
 * app's own updater trusts (read from app_update_verify.go — one source), and
 * every downloaded asset must hash to its SHA256SUMS line. Only then are the
 * hashes written; prebuild later refuses any core that does not match them.
 *
 * usage: node scripts/core-manifest.mjs [--check]
 *   --check  verify the manifest against the release without writing
 */
import { createHash, createPublicKey, verify } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const MANIFEST = path.join(ROOT, 'core', 'mihomo', 'manifest.json')
const VERIFY_GO = path.join(
  ROOT,
  'apps',
  'sloth-clash-desktop',
  'app_update_verify.go',
)
const REPO = process.env.GITHUB_REPOSITORY || 'Nemu-x/SlothClash'
const CHECK = process.argv.includes('--check')

export const coreRelease = (m) => `core-${m.tag}-sloth.${m.series}`

export function trustedKeys() {
  const src = fs.readFileSync(VERIFY_GO, 'utf8')
  const block = src.match(/var trustedUpdateKeys = \[\]string\{([\s\S]*?)\n\}/)
  if (!block)
    throw new Error('trustedUpdateKeys not found in app_update_verify.go')
  return [...block[1].matchAll(/"(RW[A-Za-z0-9+/=]+)"/g)].map((x) => x[1])
}

// minisign verification, same rules as app_update_verify.go verifyMinisign.
export function verifyMinisign(message, sigText, keys) {
  const lines = sigText.replace(/\r\n/g, '\n').trimEnd().split('\n')
  if (lines.length < 4) throw new Error('signature truncated')
  const sig = Buffer.from(lines[1].trim(), 'base64')
  if (sig.length !== 74) throw new Error('bad signature length')
  if (!lines[2].startsWith('trusted comment:'))
    throw new Error('missing trusted comment')
  const comment = lines[2].slice('trusted comment:'.length).trim()
  const global = Buffer.from(lines[3].trim(), 'base64')
  const algo = sig.subarray(0, 2).toString('latin1')
  const signed =
    algo === 'ED'
      ? createHash('blake2b512').update(message).digest()
      : algo === 'Ed'
        ? message
        : null
  if (!signed) throw new Error(`unsupported signature algorithm ${algo}`)
  for (const k of keys) {
    const raw = Buffer.from(k, 'base64')
    if (raw.length !== 42 || !raw.subarray(2, 10).equals(sig.subarray(2, 10)))
      continue
    const pub = createPublicKey({
      key: Buffer.concat([
        Buffer.from('302a300506032b6570032100', 'hex'),
        raw.subarray(10),
      ]),
      format: 'der',
      type: 'spki',
    })
    const body = sig.subarray(10)
    if (!verify(null, signed, pub, body))
      throw new Error('SHA256SUMS signature does not verify')
    if (!verify(null, Buffer.concat([body, Buffer.from(comment)]), pub, global))
      throw new Error('trusted comment signature does not verify')
    return
  }
  throw new Error('SHA256SUMS is not signed by a trusted key')
}

async function get(url) {
  const res = await fetch(url, { redirect: 'follow' })
  if (!res.ok) throw new Error(`GET ${url}: HTTP ${res.status}`)
  return Buffer.from(await res.arrayBuffer())
}

async function main() {
  const m = JSON.parse(fs.readFileSync(MANIFEST, 'utf8'))
  const release = coreRelease(m)
  const base = `https://github.com/${REPO}/releases/download/${release}`
  console.log(`[core-manifest] ${release}`)

  const sums = await get(`${base}/SHA256SUMS`)
  verifyMinisign(
    sums,
    (await get(`${base}/SHA256SUMS.minisig`)).toString('utf8'),
    trustedKeys(),
  )
  console.log('[core-manifest] SHA256SUMS signature OK')
  const listed = new Map(
    sums
      .toString('utf8')
      .split('\n')
      .map((l) => l.trim().match(/^([0-9a-f]{64})\s+\*?(.+)$/))
      .filter(Boolean)
      .map((x) => [x[2], x[1]]),
  )

  let drift = false
  for (const [target, asset] of Object.entries(m.assets)) {
    const want = listed.get(asset.file)
    if (!want)
      throw new Error(`${asset.file} (${target}) is missing from SHA256SUMS`)
    const got = createHash('sha256')
      .update(await get(`${base}/${asset.file}`))
      .digest('hex')
    if (got !== want)
      throw new Error(
        `${asset.file}: downloaded ${got}, SHA256SUMS says ${want}`,
      )
    if (asset.sha256 !== got) {
      drift = true
      console.log(
        `[core-manifest] ${target}: ${asset.sha256 || '(empty)'} -> ${got}`,
      )
      asset.sha256 = got
    } else {
      console.log(`[core-manifest] ${target}: ok`)
    }
  }
  if (CHECK) {
    if (drift)
      throw new Error(
        'manifest hashes do not match the release (run without --check)',
      )
    return
  }
  fs.writeFileSync(MANIFEST, JSON.stringify(m, null, 2) + '\n')
  console.log(
    drift
      ? '[core-manifest] manifest updated'
      : '[core-manifest] manifest already current',
  )
}

const invoked =
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
if (invoked) {
  main().catch((e) => {
    console.error(`[core-manifest] ${e.message}`)
    process.exit(1)
  })
}
