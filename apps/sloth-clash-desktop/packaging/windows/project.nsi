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

!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "Russian"
!insertmacro MUI_LANGUAGE "SimpChinese"

LangString SL_DESKTOP_SHORTCUT ${LANG_ENGLISH} "Desktop shortcut"
LangString SL_DESKTOP_SHORTCUT ${LANG_RUSSIAN} "Ярлык на рабочем столе"
LangString SL_DESKTOP_SHORTCUT ${LANG_SIMPCHINESE} "桌面快捷方式"

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
    nsExec::ExecToLog 'taskkill /F /IM "Sloth Clash.exe"'
    nsExec::ExecToLog 'taskkill /F /IM "${PRODUCT_EXECUTABLE}"'
    nsExec::ExecToLog 'taskkill /F /IM "SlothClashDesktop.exe"'
    nsExec::ExecToLog 'taskkill /F /IM "sloth-clash-desktop.exe"'
    Sleep 1000
    nsExec::ExecToStack 'tasklist /FI "IMAGENAME eq Sloth Clash.exe" | find /I "Sloth Clash.exe"'
    Pop $0
    Pop $1
    StrCmp $0 "0" 0 +3
    MessageBox MB_ICONSTOP|MB_OK "${INFO_PRODUCTNAME} is still running (Sloth Clash.exe). Please close it and run installer again."
    Abort
    nsExec::ExecToStack 'tasklist /FI "IMAGENAME eq ${PRODUCT_EXECUTABLE}" | find /I "${PRODUCT_EXECUTABLE}"'
    Pop $0
    Pop $1
    StrCmp $0 "0" 0 +3
    MessageBox MB_ICONSTOP|MB_OK "${INFO_PRODUCTNAME} is still running (${PRODUCT_EXECUTABLE}). Please close it and run installer again."
    Abort

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

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
