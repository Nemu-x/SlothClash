import { execFileSync, spawn } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { cwd } from 'node:process'

function findRepoRoot() {
  let d = path.resolve(cwd())
  for (let i = 0; i < 10; i++) {
    const marker = path.join(d, 'apps', 'sloth-clash-desktop', 'wails.json')
    if (fs.existsSync(marker)) return d
    const p = path.dirname(d)
    if (p === d) break
    d = p
  }
  throw new Error(
    `[wails] Could not find SlothClash repo root (expected apps/sloth-clash-desktop/wails.json). cwd=${cwd()}`,
  )
}

const repoRoot = findRepoRoot()
const appDir = path.join(repoRoot, 'apps', 'sloth-clash-desktop')
const args = process.argv.slice(2)
const commandArgs = args.length > 0 ? args : ['dev']

/** IDE terminals often have a minimal PATH; `spawn('go')` then fails with ENOENT on Windows. */
function resolveGoExe() {
  if (process.platform !== 'win32') return 'go'
  try {
    const out = execFileSync('where.exe', ['go'], { encoding: 'utf8' }).trim()
    const first = out
      .split(/\r?\n/)
      .map((l) => l.trim())
      .find((l) => l && !l.startsWith('INFO:'))
    if (first && fs.existsSync(first)) return first
  } catch {
    /* where failed */
  }
  const candidates = [
    process.env.GOROOT && path.join(process.env.GOROOT, 'bin', 'go.exe'),
    'C:\\Program Files\\Go\\bin\\go.exe',
    path.join(
      process.env.LOCALAPPDATA || '',
      'Programs',
      'Go',
      'bin',
      'go.exe',
    ),
  ].filter(Boolean)
  for (const c of candidates) {
    if (fs.existsSync(c)) return c
  }
  return 'go'
}

const goExe = resolveGoExe()
if (process.platform === 'win32' && goExe === 'go') {
  console.error(
    '[wails] go.exe not found (not in PATH and not under Program Files\\Go). Install Go: https://go.dev/dl/',
  )
  process.exit(1)
}

const resourcesDir = path.join(appDir, 'build', 'resources')
const sidecarDir = path.join(appDir, 'build', 'sidecar')

const hasServiceInstaller = () => {
  const checkDir = (dir) => {
    try {
      const files = fs.readdirSync(dir)
      return files.some((f) => {
        const x = f.toLowerCase()
        return x.includes('sloth-clash-service-install')
      })
    } catch {
      return false
    }
  }
  // Linux prebuild puts service binaries under sidecar; Windows/macOS under resources.
  return checkDir(resourcesDir) || checkDir(sidecarDir)
}

if (!hasServiceInstaller()) {
  const msg =
    '[wails] Service installer bundle not found. Run: pnpm run prebuild && pnpm run prepare:wails'
  if (commandArgs[0] === 'build') {
    console.error(msg)
    process.exit(1)
  }
  console.warn(`${msg} (continuing dev)`)
}

if (commandArgs[0] === 'build') {
  const syncScript = path.join(
    repoRoot,
    'scripts',
    'sync-desktop-packaging.mjs',
  )
  if (fs.existsSync(syncScript)) {
    execFileSync(process.execPath, [syncScript], {
      stdio: 'inherit',
      cwd: repoRoot,
    })
  }
}

/** Prepend Go + NSIS dirs so Wails child processes (e.g. makensis) resolve reliably on Windows. */
function spawnEnvForWails() {
  const env = { ...process.env }
  if (process.platform !== 'win32') {
    return env
  }
  const sep = path.delimiter
  // Windows stores the variable as "Path"; only "PATH" is often undefined → was wiping PATH for children.
  let p = env.PATH || env.Path || ''
  if (goExe !== 'go' && fs.existsSync(goExe)) {
    const goBin = path.dirname(goExe)
    p = `${goBin}${sep}${p}`
  }
  if (commandArgs[0] === 'build') {
    const nsisDirs = [
      path.join(process.env['ProgramFiles(x86)'] || '', 'NSIS'),
      path.join(process.env.ProgramFiles || '', 'NSIS'),
      path.join(process.env['ProgramFiles(x86)'] || '', 'NSIS', 'Bin'),
      path.join(process.env.ProgramFiles || '', 'NSIS', 'Bin'),
    ]
    for (const dir of nsisDirs) {
      if (fs.existsSync(path.join(dir, 'makensis.exe'))) {
        p = `${dir}${sep}${p}`
        console.log(`[wails] NSIS found, prepended to PATH: ${dir}`)
        break
      }
    }
  }
  env.PATH = p
  env.Path = p
  return env
}

if (goExe !== 'go') {
  console.log(`[wails] using Go: ${goExe}`)
}

const child = spawn(
  goExe,
  ['run', 'github.com/wailsapp/wails/v2/cmd/wails@latest', ...commandArgs],
  {
    cwd: appDir,
    stdio: 'inherit',
    shell: false,
    env: spawnEnvForWails(),
  },
)

child.on('exit', (code) => {
  process.exit(code ?? 1)
})
