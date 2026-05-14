/** Runtime diagnostics: event ring, privileged-service log tail, Advanced
 *  page tools (path inspector, geo status, restart-core / reset-cache /
 *  re-extract). */
export {
  GetAdvancedGeoStatus,
  GetAdvancedPaths,
  GetRuntimeDiagEvents,
  OpenPathInExplorer,
  ReadServiceLatestLog,
  ReExtractBundledResources,
  ResetSubscriptionCache,
  RestartCore,
} from '../../wailsjs/go/main/App'
