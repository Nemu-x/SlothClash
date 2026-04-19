import fs from 'fs'
import fsp from 'fs/promises'
import path from 'path'
import { glob } from 'glob'

const cwd = process.cwd()
const destDir = path.join(
  cwd,
  'apps',
  'sloth-clash-desktop',
  'build',
  'resources',
)

/** Only Sloth on-disk names; legacy Verge files are migrated once if present. */
const patterns = [
  'sloth-clash-service*.exe',
  'sloth-clash-service',
  'sloth-clash-service-install*.exe',
  'sloth-clash-service-install',
  'sloth-clash-service-uninstall*.exe',
  'sloth-clash-service-uninstall',
]

async function migrateLegacyVergeAliases() {
  if (process.platform !== 'win32') return
  const pairs = [
    ['clash-verge-service.exe', 'sloth-clash-service.exe'],
    ['clash-verge-service-install.exe', 'sloth-clash-service-install.exe'],
    ['clash-verge-service-uninstall.exe', 'sloth-clash-service-uninstall.exe'],
  ]
  for (const [legacy, next] of pairs) {
    const from = path.join(destDir, legacy)
    const to = path.join(destDir, next)
    if (fs.existsSync(from) && !fs.existsSync(to)) {
      await fsp.copyFile(from, to)
      console.log(
        `[wails-prepare] copied ${legacy} → ${next} (you can delete ${legacy})`,
      )
    }
  }
}

async function main() {
  await fsp.mkdir(destDir, { recursive: true })

  await migrateLegacyVergeAliases()

  const matched = new Set()
  for (const pattern of patterns) {
    for (const f of glob.sync(pattern, { cwd: destDir, nodir: true })) {
      matched.add(f)
    }
  }
  const present = matched.size

  if (present === 0) {
    console.error(
      `[wails-prepare] No sloth-clash-service bundle in ${destDir}. Run: pnpm run prebuild`,
    )
    process.exit(1)
  }

  console.log(`[wails-prepare] OK (${present} service file(s) in ${destDir})`)
}

main().catch((err) => {
  console.error('[wails-prepare] failed:', err)
  process.exit(1)
})
