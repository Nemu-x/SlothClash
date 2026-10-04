/**
 * Builds the patched mihomo core described by core/mihomo/manifest.json: the
 * upstream tag (commit-verified) + core/patches/mihomo/*.patch, cross-compiled
 * with the flags upstream release builds use. The release workflow builds every
 * target with it; prebuild uses it to build the host target for local builds.
 *
 * usage: node scripts/core-build.mjs <out-dir> [goos-goarch ...]   (default: every manifest target)
 *        node scripts/core-build.mjs --test-only                   (apply the series, vet + test, no binaries)
 * flags: --skip-tests   skip go vet + the patched packages' tests
 * env:   MIHOMO_UPSTREAM  clone source override (e.g. a local mirror); the commit check still applies
 *
 * Output: one raw binary per target, SHA256SUMS, BUILDINFO.txt. Fails — naming
 * the patch — when a patch no longer applies or its hash drifted from the
 * manifest, so a core bump can never ship an unpatched core.
 *
 * Written in Node rather than bash: macOS ships bash 3.2 and `bash` on Windows
 * may resolve to WSL; node, git and go behave the same on every desktop OS.
 */
import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const ROOT = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '..',
)
const MANIFEST = path.join(ROOT, 'core', 'mihomo', 'manifest.json')
const PATCH_DIR = path.join(ROOT, 'core', 'patches', 'mihomo')
const SELF = fileURLToPath(import.meta.url)

export function readManifest() {
  return JSON.parse(fs.readFileSync(MANIFEST, 'utf8'))
}

export function sha256File(file) {
  return createHash('sha256').update(fs.readFileSync(file)).digest('hex')
}

function run(cmd, args, opts = {}) {
  const res = spawnSync(cmd, args, {
    stdio: opts.capture ? ['ignore', 'pipe', 'pipe'] : 'inherit',
    encoding: 'utf8',
    ...opts,
  })
  if (res.error) {
    throw new Error(
      res.error.code === 'ENOENT'
        ? `${cmd} not found in PATH (the core build needs git and Go)`
        : `${cmd}: ${res.error.message}`,
    )
  }
  if (res.status !== 0) {
    const detail = opts.capture ? `\n${res.stderr || res.stdout}` : ''
    throw new Error(
      `${cmd} ${args.join(' ')} exited with ${res.status}${detail}`,
    )
  }
  return opts.capture ? res.stdout.trim() : ''
}

/**
 * The patch directory and the manifest must list exactly the same series, and
 * every patch must hash to what the manifest records. Returns the patch files
 * in series order.
 */
export function verifyPatchSeries(m = readManifest()) {
  const listed = m.patches.map((p) => p.file)
  const onDisk = fs
    .readdirSync(PATCH_DIR)
    .filter((f) => f.endsWith('.patch'))
    .sort()
  if (listed.join('\n') !== onDisk.join('\n')) {
    throw new Error(
      `core/patches/mihomo does not match the manifest patch list ` +
        `(manifest: ${listed.join(' ')}; on disk: ${onDisk.join(' ')})`,
    )
  }
  for (const p of m.patches) {
    const got = sha256File(path.join(PATCH_DIR, p.file))
    if (got !== p.sha256) {
      throw new Error(
        `patch ${p.file}: sha256 ${got} does not match manifest ${p.sha256} ` +
          '(update core/mihomo/manifest.json)',
      )
    }
  }
  return listed
}

/**
 * The Go release every core is built with, from core/mihomo/.tool-versions
 * (also read by setup-go in CI and bumped by Renovate). Forced through
 * GOTOOLCHAIN, so a local build fetches that exact toolchain when the installed
 * one differs (checksum-verified by the go command) and produces the same bytes
 * as the release. Changing it changes the core hash, so it moves deliberately.
 */
export function coreGoToolchain() {
  const text = fs.readFileSync(
    path.join(ROOT, 'core', 'mihomo', '.tool-versions'),
    'utf8',
  )
  const v = text.match(/^golang\s+(\d+\.\d+\.\d+)\s*$/m)?.[1]
  if (!v) throw new Error('core/mihomo/.tool-versions: expected "golang x.y.z"')
  return `go${v}`
}

function goEnv() {
  const env = {
    ...process.env,
    CGO_ENABLED: '0',
    GOTOOLCHAIN: coreGoToolchain(),
    GOFLAGS: '-mod=readonly',
  }
  delete env.GOOS
  delete env.GOARCH
  delete env.GOAMD64
  return env
}

export function goVersion() {
  return run('go', ['version'], { capture: true, env: goEnv(), cwd: ROOT })
}

/**
 * Cache key for one built target: everything that determines its bytes — the
 * upstream commit, the patch hashes, the Go version and this script (flags).
 */
export function coreBuildKey(m, target, goVer = goVersion()) {
  return createHash('sha256')
    .update(
      JSON.stringify({
        commit: m.commit,
        patches: m.patches.map((p) => p.sha256),
        target,
        go: goVer,
        script: sha256File(SELF),
      }),
    )
    .digest('hex')
    .slice(0, 16)
}

// Upstream stamps BuildTime with `date`; the tag's commit time keeps a rebuild
// with the same Go version byte-identical. Format: "Sat Oct 04 12:00:00 UTC 2026".
function buildTimeOf(unixSeconds) {
  const d = new Date(unixSeconds * 1000)
  const pad = (n) => String(n).padStart(2, '0')
  const day = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][d.getUTCDay()]
  const mon = [
    'Jan',
    'Feb',
    'Mar',
    'Apr',
    'May',
    'Jun',
    'Jul',
    'Aug',
    'Sep',
    'Oct',
    'Nov',
    'Dec',
  ][d.getUTCMonth()]
  return (
    `${day} ${mon} ${pad(d.getUTCDate())} ` +
    `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())} ` +
    `UTC ${d.getUTCFullYear()}`
  )
}

/**
 * Clone the pinned tag, check its commit, apply the series, optionally vet +
 * test, then build `targets` into `outDir`. With no outDir only the checks and
 * tests run.
 */
export function buildCore({
  outDir = '',
  targets = [],
  skipTests = false,
  log = console.log,
} = {}) {
  const m = readManifest()
  const patches = verifyPatchSeries(m)
  const upstream = (process.env.MIHOMO_UPSTREAM || '').trim() || m.upstream
  if (outDir && targets.length === 0) targets = Object.keys(m.assets)
  for (const t of targets) {
    if (!m.assets[t])
      throw new Error(`target ${t} is not in the manifest assets`)
  }

  let out = ''
  if (outDir) {
    fs.mkdirSync(outDir, { recursive: true })
    out = path.resolve(outDir)
    // Never leave binaries of an earlier run next to a failed one.
    for (const f of fs.readdirSync(out)) {
      if (
        f.startsWith('mihomo-') ||
        f === 'SHA256SUMS' ||
        f === 'BUILDINFO.txt'
      ) {
        fs.rmSync(path.join(out, f), { force: true })
      }
    }
  }

  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'sloth-core-'))
  try {
    const src = path.join(work, 'mihomo')
    log(`==> clone ${upstream} @ ${m.tag}`)
    // LF checkout on every OS: the patches carry LF context, and CRLF in Go
    // sources would leak into raw string literals.
    run('git', [
      '-c',
      'advice.detachedHead=false',
      'clone',
      '--quiet',
      '--depth',
      '1',
      '--config',
      'core.autocrlf=false',
      '--config',
      'core.eol=lf',
      '--branch',
      m.tag,
      upstream,
      src,
    ])
    const head = run('git', ['-C', src, 'rev-parse', 'HEAD'], { capture: true })
    if (head !== m.commit) {
      throw new Error(
        `tag ${m.tag} resolves to ${head}, manifest pins ${m.commit} (tag moved?)`,
      )
    }

    for (const p of patches) {
      const file = path.join(PATCH_DIR, p)
      try {
        run('git', ['-C', src, 'apply', '--check', file], { capture: true })
      } catch (e) {
        throw new Error(
          `patch ${p} does not apply to mihomo ${m.tag}; rebase it before bumping the core\n${e.message}`,
          { cause: e },
        )
      }
      run('git', ['-C', src, 'apply', file])
      log(`==> applied ${p}`)
    }

    const env = goEnv()
    const goVer = goVersion()
    log(`==> ${goVer}`)

    if (!skipTests) {
      log('==> go vet + tests of the patched packages')
      run('go', ['vet', './component/tls/', './config/', './hub/executor/'], {
        cwd: src,
        env,
      })
      run('go', ['test', '-count=1', './component/tls/', './config/'], {
        cwd: src,
        env,
      })
    }
    if (!out) return { goVersion: goVer }

    const commitTime = Number(
      run('git', ['-C', src, 'log', '-1', '--format=%ct'], { capture: true }),
    )
    const ldflags =
      `-extldflags --static ` +
      `-X 'github.com/metacubex/mihomo/constant.Version=${m.tag}' ` +
      `-X 'github.com/metacubex/mihomo/constant.BuildTime=${buildTimeOf(commitTime)}' ` +
      `-w -s -buildid=`

    const sums = []
    for (const t of targets) {
      const file = m.assets[t].file
      const [goos, goarch] = t.split('-')
      log(`==> build ${t} -> ${file}`)
      run(
        'go',
        [
          'build',
          '-tags',
          'with_gvisor',
          '-trimpath',
          '-ldflags',
          ldflags,
          '-o',
          path.join(out, file),
          '.',
        ],
        {
          cwd: src,
          // amd64-v2 matches the upstream builds shipped before the patch series.
          env: {
            ...env,
            GOOS: goos,
            GOARCH: goarch,
            ...(goarch === 'amd64' ? { GOAMD64: 'v2' } : {}),
          },
        },
      )
      sums.push(`${sha256File(path.join(out, file))}  ${file}`)
    }
    fs.writeFileSync(path.join(out, 'SHA256SUMS'), `${sums.join('\n')}\n`)
    fs.writeFileSync(
      path.join(out, 'BUILDINFO.txt'),
      [
        `core:      mihomo ${m.tag} + ${patches.length} SlothClash patches`,
        `upstream:  ${upstream}`,
        `tag:       ${m.tag}`,
        `commit:    ${m.commit}`,
        `go:        ${goVer}`,
        'flags:     CGO_ENABLED=0 GOAMD64=v2(amd64) -tags with_gvisor -trimpath',
        `ldflags:   ${ldflags}`,
        'patches:',
        ...m.patches.map((p) => `  ${p.sha256}  ${p.file}`),
        '',
      ].join('\n'),
    )
    log(`==> done: mihomo ${m.tag} + ${patches.length} patches`)
    log(sums.join('\n'))
    return { goVersion: goVer, sums }
  } finally {
    fs.rmSync(work, { recursive: true, force: true, maxRetries: 3 })
  }
}

/**
 * Checks a core directory produced by buildCore (a CI artifact) against the
 * manifest: BUILDINFO names the pinned tag, commit and patch hashes, and the
 * target binary hashes to its SHA256SUMS line. Returns the binary's path and hash.
 */
export function verifyBuiltCore(dir, target, m = readManifest()) {
  const asset = m.assets[target]
  if (!asset) throw new Error(`target ${target} is not in the manifest assets`)
  const info = fs.readFileSync(path.join(dir, 'BUILDINFO.txt'), 'utf8')
  const expect = [
    `tag:       ${m.tag}`,
    `commit:    ${m.commit}`,
    ...m.patches.map((p) => `  ${p.sha256}  ${p.file}`),
  ]
  for (const line of expect) {
    if (!info.split(/\r?\n/).includes(line)) {
      throw new Error(
        `${dir}/BUILDINFO.txt was not built from this manifest (missing "${line.trim()}")`,
      )
    }
  }
  const sums = fs.readFileSync(path.join(dir, 'SHA256SUMS'), 'utf8')
  const want = sums
    .split(/\r?\n/)
    .map((l) => l.match(/^([0-9a-f]{64})\s+\*?(.+)$/))
    .find((mm) => mm && mm[2] === asset.file)?.[1]
  if (!want) throw new Error(`${dir}/SHA256SUMS has no line for ${asset.file}`)
  const file = path.join(dir, asset.file)
  const got = sha256File(file)
  if (got !== want) {
    throw new Error(
      `${asset.file}: sha256 ${got} does not match SHA256SUMS ${want}`,
    )
  }
  return { file, sha256: got }
}

if (process.argv[1] && path.resolve(process.argv[1]) === SELF) {
  const args = process.argv.slice(2)
  const testOnly = args.includes('--test-only')
  const skipTests = args.includes('--skip-tests')
  const positional = args.filter((a) => !a.startsWith('--'))
  try {
    if (testOnly) {
      buildCore({})
    } else {
      if (positional.length === 0) {
        console.error(
          'usage: node scripts/core-build.mjs <out-dir> [goos-goarch ...] | --test-only',
        )
        process.exit(2)
      }
      const [outDir, ...targets] = positional
      buildCore({ outDir, targets, skipTests })
    }
  } catch (e) {
    if (process.env.GITHUB_ACTIONS)
      console.log(`::error::${e.message.split('\n')[0]}`)
    console.error(`ERROR: ${e.message}`)
    process.exit(1)
  }
}
