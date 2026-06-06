# Downloads and silently installs Microsoft Visual C++ 2015-2022 Redistributable.
#
# Invoked by the Sloth Clash NSIS installer when the runtime is missing on the
# user's machine. Lives as a separate file (rather than an inline NSIS
# `nsExec::ExecToStack 'powershell ...'` string) because NSIS quoting rules
# fight with PowerShell quoting rules: nesting double quotes, single quotes,
# `$\"` escapes and `;` separators inside one NSIS string turned out to be
# brittle and broke compilation in unexpected places.
#
# Usage:
#   powershell -ExecutionPolicy Bypass -NoProfile -File vc_install.ps1 -Url <url>
#
# Exit codes:
#   0  — installer ran (any inner VC++ installer exit code is preserved)
#   1  — download failed
#   2  — vc_redist.exe missing after download
param(
    [Parameter(Mandatory = $true)]
    [string]$Url
)

$ErrorActionPreference = 'Stop'

# Old Win10 builds still default to TLS 1.0/1.1 in .NET; aka.ms redirects to a
# CDN that only serves 1.2+. Force TLS 1.2 globally for this process.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$dst = Join-Path $env:TEMP 'sloth_vc_redist.exe'

try {
    (New-Object System.Net.WebClient).DownloadFile($Url, $dst)
}
catch {
    Write-Host "[sloth] vc_redist download failed: $($_.Exception.Message)"
    exit 1
}

if (-not (Test-Path -LiteralPath $dst)) {
    Write-Host "[sloth] vc_redist.exe missing after download"
    exit 2
}

# /passive (not /quiet) so Microsoft's own redist installer shows its progress
# UI while it runs — otherwise the NSIS installer's progress bar just sits frozen
# during the install and looks hung to the user.
$p = Start-Process -FilePath $dst -ArgumentList '/install', '/passive', '/norestart' -Wait -PassThru
exit $p.ExitCode
