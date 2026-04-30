import { spawn } from 'node:child_process'
import fs from 'node:fs'
import fsp from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const repoRoot = path.resolve(__dirname, '..', '..')

function run(command, args, cwd = repoRoot) {
  let cmd = command
  let cmdArgs = args
  if (process.platform === 'win32' && command.toLowerCase().endsWith('.cmd')) {
    // Execute .cmd via cmd.exe without enabling shell mode.
    cmd = 'cmd.exe'
    cmdArgs = ['/d', '/s', '/c', command, ...args]
  }
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, cmdArgs, {
      cwd,
      stdio: 'inherit',
      shell: false,
    })
    child.on('exit', (code) => {
      if (code === 0) resolve()
      else
        reject(
          new Error(
            `${command} ${args.join(' ')} failed with exit code ${code}`,
          ),
        )
    })
    child.on('error', reject)
  })
}

function hasEnv(name) {
  const v = process.env[name]
  return typeof v === 'string' && v.trim().length > 0
}

async function step(title, command, args, cwd) {
  console.log(`\n=== ${title} ===`)
  try {
    await run(command, args, cwd)
    console.log(`PASS: ${title}`)
  } catch (err) {
    const msg = err?.message || String(err)
    console.error(`ERROR: ${title}`)
    throw new Error(
      `${title} failed.\nFix this locally and rerun: pnpm run test:required\nDetails: ${msg}`,
      { cause: err },
    )
  }
}

/**
 * `main.go` embeds `all:build/resources` and `all:build/sidecar`. Go requires at least one
 * file per directory. This gate runs before `prebuild`, so CI/fresh clones need tiny
 * placeholders until prebuild seeds real service binaries.
 */
async function ensureDesktopEmbedDirsForGoTest(desktopDir) {
  for (const rel of ['build/resources', 'build/sidecar']) {
    const dir = path.join(desktopDir, rel)
    await fsp.mkdir(dir, { recursive: true })
    const entries = await fsp.readdir(dir)
    if (entries.length === 0) {
      await fsp.writeFile(
        path.join(dir, '_embed_placeholder.txt'),
        'Placeholder for go:embed until prebuild adds service binaries.\n',
      )
    }
  }
}

async function optionalStep(title, command, args, cwd) {
  console.log(`\n=== ${title} ===`)
  try {
    await run(command, args, cwd)
    console.log(`PASS: ${title}`)
  } catch (err) {
    const msg = err?.message || String(err)
    // Local-only stress test may be absent in CI/dev machines.
    if (
      msg.includes('not found in Downloads') ||
      msg.includes(
        'set SLOTH_MIHOMO_BIN to run real-core preflight integration',
      )
    ) {
      if (msg.includes('not found in Downloads')) {
        console.log(`SKIP: ${title} (stress.yaml not found in Downloads)`)
      } else {
        console.log(`SKIP: ${title} (SLOTH_MIHOMO_BIN is not set)`)
      }
      return
    }
    console.error(`ERROR: ${title}`)
    throw new Error(
      `${title} failed.\nFix this locally and rerun: pnpm run test:required\nDetails: ${msg}`,
      { cause: err },
    )
  }
}

async function main() {
  const goBin = process.platform === 'win32' ? 'go.exe' : 'go'
  const pnpmBin = process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm'
  const desktopDir = path.join(repoRoot, 'apps', 'sloth-clash-desktop')
  const frontendDir = path.join(desktopDir, 'frontend')
  // Required smoke+build baseline before PR merge.
  await ensureDesktopEmbedDirsForGoTest(desktopDir)
  const appiconPng = path.join(desktopDir, 'build', 'appicon.png')
  if (!fs.existsSync(appiconPng)) {
    await step(
      'Copy desktop app icon',
      pnpmBin,
      ['run', 'copy:desktop-appicon'],
      repoRoot,
    )
  }
  const winTrayIco = path.join(desktopDir, 'build', 'windows', 'icon.ico')
  if (process.platform === 'win32' && !fs.existsSync(winTrayIco)) {
    await step(
      'Windows icon (ico) for go:embed',
      pnpmBin,
      ['run', 'icons:windows'],
      repoRoot,
    )
  }
  const indexHtml = path.join(frontendDir, 'dist', 'index.html')
  if (!fs.existsSync(indexHtml)) {
    await step(
      'Desktop frontend production build',
      pnpmBin,
      ['--dir', 'apps/sloth-clash-desktop/frontend', 'run', 'build'],
      repoRoot,
    )
  }
  await step(
    'Config corpus tests',
    goBin,
    ['test', '-count=1', '-v', '-run', 'TestMihomoCorpus', './...'],
    desktopDir,
  )
  await step(
    'Runtime pipeline smoke tests',
    goBin,
    ['test', '-count=1', '-v', '-run', 'TestRuntimeSmoke', './...'],
    desktopDir,
  )
  await step(
    'Runtime E2E scenarios',
    goBin,
    ['test', '-count=1', '-v', '-run', 'TestE2ERuntime', './...'],
    desktopDir,
  )
  await optionalStep(
    'Local stress.yaml test',
    goBin,
    [
      'test',
      '-v',
      '-run',
      'TestLocalStressYamlFromDownloadsIfPresent',
      './...',
    ],
    desktopDir,
  )
  if (!hasEnv('SLOTH_MIHOMO_BIN')) {
    console.log('\n=== Real-core preflight integration ===')
    console.log(
      'SKIP: Real-core preflight integration (SLOTH_MIHOMO_BIN is not set)',
    )
  } else {
    await optionalStep(
      'Real-core preflight integration',
      goBin,
      [
        'test',
        '-count=1',
        '-v',
        '-run',
        'TestIntegrationPreflightWithRealCoreBinary',
        './...',
      ],
      desktopDir,
    )
  }
  await step('Backend unit/smoke tests', goBin, ['test', './...'], desktopDir)
  await step('Backend compile check', goBin, ['build', './...'], desktopDir)
  await step(
    'Frontend type check',
    pnpmBin,
    [
      'exec',
      'tsc',
      '--noEmit',
      '-p',
      'apps/sloth-clash-desktop/frontend/tsconfig.json',
    ],
    repoRoot,
  )
  console.log('\nPASS: required test gate')
}

main().catch((err) => {
  console.error(err.message || err)
  process.exit(1)
})
