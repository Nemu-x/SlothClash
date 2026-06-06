Unicode true

####
## Sloth Clash — NSIS installer (Wails-compatible).
## Synced into build/windows/installer/project.nsi before `wails build` (see scripts/sync-desktop-packaging.mjs).
##
## Adds English / Russian / Simplified Chinese and shows the Modern UI language dialog so the
## default follows the OS language while the user can pick another.
####
!include "wails_tools.nsh"

# INFO_PRODUCTVERSION comes from wails.json info.productVersion — must be numeric X.Y.Z only
# (no pre-release suffixes): Wails appends ".0" for NSIS VI*Version, which must look like X.X.X.X.
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "$(SL_LAUNCH_APP)"
!define MUI_FINISHPAGE_RUN_NOTCHECKED
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchAppAsShellUser
!define MUI_ABORTWARNING

# Remember installer language; LangDLL picks a sensible default from the OS UI language.
!define MUI_LANGDLL_REGISTRY_ROOT HKCU
!define MUI_LANGDLL_REGISTRY_KEY "Software\Nemu-x\SlothClashDesktop\Installer"
!define MUI_LANGDLL_REGISTRY_VALUENAME "InstallerLanguage"

!insertmacro MUI_RESERVEFILE_LANGDLL

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_COMPONENTS
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "Russian"
!insertmacro MUI_LANGUAGE "SimpChinese"

LangString SL_DESKTOP_SHORTCUT ${LANG_ENGLISH} "Desktop shortcut"
LangString SL_DESKTOP_SHORTCUT ${LANG_RUSSIAN} "Ярлык на рабочем столе"
LangString SL_DESKTOP_SHORTCUT ${LANG_SIMPCHINESE} "桌面快捷方式"
LangString SL_UNINSTALL_CORE ${LANG_ENGLISH} "Remove application files"
LangString SL_UNINSTALL_CORE ${LANG_RUSSIAN} "Удалить файлы приложения"
LangString SL_UNINSTALL_CORE ${LANG_SIMPCHINESE} "删除应用程序文件"
LangString SL_UNINSTALL_DATA ${LANG_ENGLISH} "Remove user data (profiles, runtime, logs)"
LangString SL_UNINSTALL_DATA ${LANG_RUSSIAN} "Удалить пользовательские данные (профили, runtime, логи)"
LangString SL_UNINSTALL_DATA ${LANG_SIMPCHINESE} "删除用户数据（配置、运行时、日志）"
LangString SL_LAUNCH_APP ${LANG_ENGLISH} "Launch SlothClash"
LangString SL_LAUNCH_APP ${LANG_RUSSIAN} "Запустить SlothClash"
LangString SL_LAUNCH_APP ${LANG_SIMPCHINESE} "启动 SlothClash"
LangString SL_VCREDIST_INSTALLING ${LANG_ENGLISH} "Installing required runtime: Visual C++ 2015-2022 (one-time, please wait)..."
LangString SL_VCREDIST_INSTALLING ${LANG_RUSSIAN} "Установка необходимого компонента: Visual C++ 2015-2022 (один раз, подождите)..."
LangString SL_VCREDIST_INSTALLING ${LANG_SIMPCHINESE} "正在安装所需运行库: Visual C++ 2015-2022（一次性，请稍候）..."
LangString SL_VCREDIST_DOWNLOAD_FAIL ${LANG_ENGLISH} "Could not download Visual C++ 2015-2022 Redistributable. The app may fail to start. Install it manually from https://aka.ms/vs/17/release/vc_redist.x64.exe"
LangString SL_VCREDIST_DOWNLOAD_FAIL ${LANG_RUSSIAN} "Не удалось скачать Visual C++ 2015-2022 Redistributable. Без него приложение может не запуститься. Установите вручную: https://aka.ms/vs/17/release/vc_redist.x64.exe"
LangString SL_VCREDIST_DOWNLOAD_FAIL ${LANG_SIMPCHINESE} "无法下载 Visual C++ 2015-2022 Redistributable，应用可能无法启动。请手动安装: https://aka.ms/vs/17/release/vc_redist.x64.exe"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show

# Microsoft Visual C++ 2015-2022 Redistributable runtime check.
# Required by bundled Rust executables (mihomo sidecar, sloth-clash-service).
# Without it the core silently fails to start on user machines that never had
# Visual Studio runtimes installed (common on fresh Windows installs).
!macro sloth.vcRedistRuntime
    SetRegView 64
    StrCpy $1 ""

    !ifdef SUPPORTS_AMD64
        ${If} ${IsNativeAMD64}
            ReadRegDWORD $0 HKLM "SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\X64" "Installed"
            ${If} $0 == "1"
                Goto sloth_vcredist_ok
            ${EndIf}
            StrCpy $1 "https://aka.ms/vs/17/release/vc_redist.x64.exe"
        ${EndIf}
    !endif

    !ifdef SUPPORTS_ARM64
        ${If} ${IsNativeARM64}
            ReadRegDWORD $0 HKLM "SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\ARM64" "Installed"
            ${If} $0 == "1"
                Goto sloth_vcredist_ok
            ${EndIf}
            StrCpy $1 "https://aka.ms/vs/17/release/vc_redist.arm64.exe"
        ${EndIf}
    !endif

    StrCmp $1 "" sloth_vcredist_ok 0

    SetDetailsPrint both
    DetailPrint "$(SL_VCREDIST_INSTALLING)"
    SetDetailsPrint listonly

    InitPluginsDir

    # Download + silent install via a standalone PS1 script. Keeping the
    # logic in a file rather than inline avoids tricky NSIS-vs-PowerShell
    # quoting (the comment block above used to embed quote/escape characters
    # which NSIS happily tried to parse). The script is synced into the
    # installer build directory by scripts/sync-desktop-packaging.mjs.
    File "/oname=$pluginsdir\vc_install.ps1" "vc_install.ps1"
    ExecWait '"powershell.exe" -ExecutionPolicy Bypass -NoProfile -File "$pluginsdir\vc_install.ps1" -Url "$1"' $2
    StrCmp $2 "0" sloth_vcredist_ok sloth_vcredist_dl_fail

sloth_vcredist_dl_fail:
    # NB: MB_ICONEXCLAMATION (not MB_ICONWARNING) — older NSIS builds only
    # know the legacy WinAPI name. Both map to 0x30 at runtime.
    MessageBox MB_OK|MB_ICONEXCLAMATION "$(SL_VCREDIST_DOWNLOAD_FAIL)"

sloth_vcredist_ok:
    SetDetailsPrint both
!macroend

Function .onInit
  SetShellVarContext current
  !insertmacro MUI_LANGDLL_DISPLAY
  !insertmacro wails.checkArchitecture
FunctionEnd

Function LaunchAppAsShellUser
  ; Start via Explorer so first launch uses the interactive shell user context
  ; instead of the elevated installer token account.
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\${PRODUCT_EXECUTABLE}"'
FunctionEnd

Section "${INFO_PRODUCTNAME}" SecApp
    SectionIn RO
    SetShellVarContext current
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime
    !insertmacro sloth.vcRedistRuntime

    ; Close running instance if any (no extra prompt — same idea as silent upgrade flows).
    ; Graceful-ish close handling: Windows may keep the exe locked briefly even after taskkill.
    StrCpy $2 0
  KillAndWaitLoop:
    nsExec::ExecToStack 'taskkill /F /T /IM "${PRODUCT_EXECUTABLE}"'
    Pop $0
    Pop $1
    ; nsExec: first Pop = exit code. taskkill: 0 = terminated, 128 = image not found (not running).
    StrCmp $0 "0" KillExit0 KillNot0
  KillExit0:
    DetailPrint 'Stopped running Sloth Clash (taskkill OK).'
    Goto AfterKillMsg
  KillNot0:
    StrCmp $0 "128" KillExit128 KillUnknown
  KillExit128:
    DetailPrint 'Sloth Clash was not running (nothing to stop).'
    Goto AfterKillMsg
  KillUnknown:
    DetailPrint "taskkill exit $0 — continuing anyway."
  AfterKillMsg:
    Sleep 650
    nsExec::ExecToStack 'tasklist /FI "IMAGENAME eq ${PRODUCT_EXECUTABLE}" | find /I "${PRODUCT_EXECUTABLE}"'
    Pop $0
    Pop $1
    StrCmp $0 "0" ProcessStillRunning 0
    Goto ProcessesClosed
  ProcessStillRunning:
    IntOp $2 $2 + 1
    IntCmp $2 12 0 KillAndWaitLoop 0
    MessageBox MB_ICONSTOP|MB_OK "${INFO_PRODUCTNAME} is still running. Close it manually, then run this installer again."
    Abort
  ProcessesClosed:
    ; Give AV/indexers a brief window to release the executable handle.
    Sleep 900

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

; Optional: user opts in on the Components page (unchecked by default).
Section /o "$(SL_DESKTOP_SHORTCUT)" SecDesktop
    CreateShortcut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
SectionEnd

Section "un.$(SL_UNINSTALL_CORE)" SecUnApp
    SectionIn RO
    SetShellVarContext current
    !insertmacro wails.setShellContext

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd

Section /o "un.$(SL_UNINSTALL_DATA)" SecUnData
    RMDir /r "$APPDATA\SlothClash"
    RMDir /r "$LOCALAPPDATA\SlothClash"
SectionEnd
