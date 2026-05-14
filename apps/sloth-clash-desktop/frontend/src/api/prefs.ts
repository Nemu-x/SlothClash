/**
 * Desktop preferences: close-to-tray, launch-on-startup, start-minimized,
 * tray availability, preferred UI language, window-visibility hook.
 */
export {
  GetDesktopPrefs,
  GetLaunchOnStartupPreference,
  GetPreferredLanguage,
  GetTrayAvailability,
  OnWindowBecameVisible,
  SetCloseToTrayPreference,
  SetLaunchOnStartupPreference,
  SetUiLanguage,
  StartedMinimized,
} from '../../wailsjs/go/main/App'
