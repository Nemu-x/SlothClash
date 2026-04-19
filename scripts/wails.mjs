import { spawn } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { cwd } from 'node:process'

const appDir = `${cwd()}/apps/sloth-clash-desktop`
const args = process.argv.slice(2)
const commandArgs = args.length > 0 ? args : ['dev']

const resourcesDir = path.join(appDir, 'build', 'resources')
const hasServiceInstaller = () => {
  try {
    const files = fs.readdirSync(resourcesDir)
    return files.some((f) => {
      const x = f.toLowerCase()
      return x.includes('sloth-clash-service-install')
    })
  } catch {
    return false
  }
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

const child = spawn(
  'go',
  ['run', 'github.com/wailsapp/wails/v2/cmd/wails@latest', ...commandArgs],
  {
    cwd: appDir,
    stdio: 'inherit',
    shell: false,
  },
)

child.on('exit', (code) => {
  process.exit(code ?? 1)
})
