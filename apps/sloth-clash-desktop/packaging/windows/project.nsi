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

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show

Function .onInit
  !insertmacro MUI_LANGDLL_DISPLAY
  !insertmacro wails.checkArchitecture
FunctionEnd

Section "${INFO_PRODUCTNAME}" SecApp
    SectionIn RO
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    MessageBox MB_ICONEXCLAMATION|MB_YESNO "${INFO_PRODUCTNAME} update may require closing running app instances. Installer can do it automatically now. Continue?" IDYES +2 IDNO 0
    Abort
    ; Graceful-ish close handling: Windows may keep the exe locked briefly even after taskkill.
    StrCpy $2 0
  KillAndWaitLoop:
    nsExec::ExecToStack 'taskkill /F /T /IM "${PRODUCT_EXECUTABLE}"'
    Pop $0
    Pop $1
    StrCmp $0 "0" +2 0
      DetailPrint 'Process found - closing the app'
    StrCmp $0 "128" +2 0
      DetailPrint 'Process not found'
    Sleep 650
    nsExec::ExecToStack 'tasklist /FI "IMAGENAME eq Sloth Clash.exe" | find /I "Sloth Clash.exe"'
    Pop $0
    Pop $1
    StrCmp $0 "0" ProcessStillRunning 0
    nsExec::ExecToStack 'tasklist /FI "IMAGENAME eq ${PRODUCT_EXECUTABLE}" | find /I "${PRODUCT_EXECUTABLE}"'
    Pop $0
    Pop $1
    StrCmp $0 "0" ProcessStillRunning 0
    Goto ProcessesClosed
  ProcessStillRunning:
    IntOp $2 $2 + 1
    IntCmp $2 12 0 KillAndWaitLoop 0
    MessageBox MB_ICONSTOP|MB_OK "${INFO_PRODUCTNAME} is still running. Please close it and click Retry in the previous installer prompt."
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
