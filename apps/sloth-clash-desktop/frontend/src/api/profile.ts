/**
 * Profile lifecycle: CRUD, activation, subscription refresh, baselines,
 * extend-config / proxy / rules templates, the JavaScript override, auto-update
 * preference.
 */
export {
  ActivateProfile,
  ClearProfileScriptOverride,
  DeleteProfile,
  DeriveAgePublicKey,
  GenerateAgeKeyPair,
  GetProfilePaths,
  GetProfileProxyGroupsBaseline,
  GetProfileRulesBaseline,
  ImportProfileFromText,
  ImportProfileFromURL,
  PreviewProfileScript,
  ReadProfileConfig,
  RefreshProfileSubscription,
  SetProfileAgeSecretKey,
  SetProfileAutoUpdate,
  SetProfileMergeTemplate,
  SetProfileProxyTemplate,
  SetProfileRulesTemplate,
  SetProfileScriptOverride,
  UpdateProfileInfo,
  WriteProfileConfig,
} from '../../wailsjs/go/main/App'

/** Deep-link subscription import, performed only after the user confirms. */
export { ConfirmInstallConfigFromLink } from '../../wailsjs/go/main/App'
