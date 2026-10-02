# winget (Windows Package Manager)

Package identifier: **`Nemu-x.SlothClash`** → `winget install Nemu-x.SlothClash`

Once the package exists in [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs), the
`winget` job in `.github/workflows/desktop-artifacts.yml` opens the update PR for every new tag
automatically (via [vedantmgoyal9/winget-releaser](https://github.com/vedantmgoyal9/winget-releaser)).
The job is skipped while the `WINGET_TOKEN` secret is missing, so releases keep working without it.

The releaser **only updates packages that already exist**. The first version is submitted by hand.

## 1. One-time: fork + token

1. The fork `Nemu-x/winget-pkgs` already exists (shared with SwissKnife). The releaser looks for
   `<repository owner>/winget-pkgs`.
2. The classic personal access token used for SwissKnife (scopes `public_repo` + `workflow`) works
   here too. Add it to **this** repository as the secret **`WINGET_TOKEN`**
   (Settings → Secrets and variables → Actions → New repository secret).

## 2. One-time: first submission

Ready manifests live in `packaging/winget/manifests/n/Nemu-x/SlothClash/<version>/`, hashes taken
from the release `SHA256SUMS`.

```powershell
# a) Tooling (Windows 10/11)
winget install Microsoft.WingetCreate

# b) Validate + test-install locally (enabling LocalManifestFiles needs an elevated shell, once)
$dir = "packaging\winget\manifests\n\Nemu-x\SlothClash\0.9.4"
winget validate --manifest $dir
winget settings --enable LocalManifestFiles
winget install --manifest $dir
winget uninstall Nemu-x.SlothClash

# c) Submit: pushes a branch to the Nemu-x/winget-pkgs fork and opens the PR against microsoft/winget-pkgs
wingetcreate submit --token $env:WINGET_TOKEN $dir
```

The PR goes through Microsoft's automated validation (installs the package silently in a VM) plus a
human moderator look; expect one to a few days for a first submission. Later versions from the
workflow are usually merged within hours.

## Manifest notes

- Schema `1.10.0`; `InstallerType: nullsoft`, `Scope: machine`, switches `/S` for both silent modes
  (the in-app updater uses the same switch).
- `ProductCode` is the NSIS uninstall registry key name: `Nemu-xSloth Clash`
  (Wails: `INFO_COMPANYNAME + INFO_PRODUCTNAME`). Keep `companyName` / `productName` in
  `apps/sloth-clash-desktop/wails.json` stable — changing them changes the ProductCode and breaks
  `winget upgrade`.
- The installer downloads the VC++ 2015-2022 redistributable from aka.ms when it is missing and
  installs it `/passive /norestart`; the validation VM has network, so this is fine under `/S`.
- To bump by hand: `wingetcreate update Nemu-x.SlothClash --version X.Y.Z --urls <x64 url> <arm64 url> --submit --token …`.
