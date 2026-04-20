import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(__dirname, '..')
const appDir = path.join(repoRoot, 'apps', 'sloth-clash-desktop')
const binDir = path.join(appDir, 'build', 'bin')
const dmgBg = path.join(repoRoot, 'docs', 'dmg-background.png')

function findBuiltApp() {
  if (!fs.existsSync(binDir)) {
    throw new Error(`build output folder missing: ${binDir}`)
  }
  const items = fs.readdirSync(binDir).filter((x) => x.endsWith('.app'))
  if (items.length === 0) {
    throw new Error(
      `no .app found under ${binDir}; run "pnpm run wails:build" first`,
    )
  }
  return path.join(binDir, items[0])
}

function writeInstallNotes(stageDir, appName) {
  const readme = `Sloth Clash macOS Install
========================

1) Drag "${appName}" to "Applications".
2) Launch it from Applications.

If macOS blocks startup (Gatekeeper quarantine), run:
  sudo xattr -r -d com.apple.quarantine "/Applications/${appName}"

You can also double-click "Fix Quarantine.command" in this DMG after drag&drop.
`
  fs.writeFileSync(path.join(stageDir, 'README.txt'), readme, 'utf8')

  const command = `#!/bin/bash
set -euo pipefail
APP="/Applications/${appName}"
if [ ! -d "$APP" ]; then
  osascript -e 'display dialog "Install the app to /Applications first." buttons {"OK"} default button "OK" with icon caution'
  exit 1
fi
sudo xattr -r -d com.apple.quarantine "$APP" || true
osascript -e 'display dialog "Done. You can now open Sloth Clash." buttons {"OK"} default button "OK"'
`
  const cmdPath = path.join(stageDir, 'Fix Quarantine.command')
  fs.writeFileSync(cmdPath, command, 'utf8')
  fs.chmodSync(cmdPath, 0o755)
}

function styleMountedDmg(volumeName, appName, useBackground) {
  const bgCmd = useBackground
    ? `set background picture of opts to file ".background:dmg-background.png"`
    : 'set background color of opts to {65535, 65535, 65535}'
  const script = `
tell application "Finder"
  tell disk "${volumeName}"
    open
    set current view of container window to icon view
    set toolbar visible of container window to false
    set statusbar visible of container window to false
    set the bounds of container window to {120, 120, 980, 650}
    set opts to the icon view options of container window
    set arrangement of opts to not arranged
    set icon size of opts to 120
    ${bgCmd}
    delay 0.2
    set position of item "${appName}" of container window to {230, 250}
    set position of item "Applications" of container window to {700, 250}
    try
      set position of item "README.txt" of container window to {230, 500}
    end try
    try
      set position of item "Fix Quarantine.command" of container window to {450, 500}
    end try
    close
    open
    update without registering applications
    delay 0.2
    close
  end tell
end tell
`
  execFileSync('osascript', ['-e', script], { stdio: 'inherit' })
}

function main() {
  const appPath = findBuiltApp()
  const appName = path.basename(appPath)
  const arch = os.arch() === 'arm64' ? 'arm64' : os.arch()
  const outName = `SlothClash-macOS-${arch}.dmg`
  const outPath = path.join(binDir, outName)
  const tempDmg = path.join(binDir, `SlothClash-macOS-${arch}-tmp.dmg`)
  const stageDir = path.join(appDir, 'build', 'dmg-stage')
  const volumeName = 'Sloth Clash'

  fs.rmSync(stageDir, { recursive: true, force: true })
  fs.mkdirSync(stageDir, { recursive: true })
  execFileSync('cp', ['-R', appPath, path.join(stageDir, appName)])

  const appsLink = path.join(stageDir, 'Applications')
  try {
    fs.symlinkSync('/Applications', appsLink)
  } catch {
    /* already exists */
  }

  writeInstallNotes(stageDir, appName)
  if (fs.existsSync(dmgBg)) {
    const bgDir = path.join(stageDir, '.background')
    fs.mkdirSync(bgDir, { recursive: true })
    fs.copyFileSync(dmgBg, path.join(bgDir, 'dmg-background.png'))
  }
  fs.rmSync(outPath, { force: true })
  fs.rmSync(tempDmg, { force: true })
  execFileSync(
    'hdiutil',
    [
      'create',
      '-volname',
      volumeName,
      '-srcfolder',
      stageDir,
      '-ov',
      '-format',
      'UDRW',
      tempDmg,
    ],
    { stdio: 'inherit' },
  )

  const attachOut = execFileSync(
    'hdiutil',
    ['attach', '-readwrite', '-noverify', '-noautoopen', tempDmg],
    { encoding: 'utf8' },
  )
  const deviceLine = attachOut
    .split('\n')
    .map((x) => x.trim())
    .find((x) => x.startsWith('/dev/'))
  const device = deviceLine ? deviceLine.split(/\s+/)[0] : ''
  if (!device) {
    throw new Error(
      `failed to parse mounted device from hdiutil output:\n${attachOut}`,
    )
  }
  styleMountedDmg(volumeName, appName, fs.existsSync(dmgBg))
  execFileSync('hdiutil', ['detach', device], { stdio: 'inherit' })
  execFileSync(
    'hdiutil',
    [
      'convert',
      tempDmg,
      '-format',
      'UDZO',
      '-imagekey',
      'zlib-level=9',
      '-o',
      outPath,
    ],
    { stdio: 'inherit' },
  )
  fs.rmSync(tempDmg, { force: true })
  fs.rmSync(stageDir, { recursive: true, force: true })
  console.log('[create-macos-dmg] wrote', outPath)
}

main()
