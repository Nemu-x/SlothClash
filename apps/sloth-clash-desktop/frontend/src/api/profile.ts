/**
 * Profile lifecycle: CRUD, activation, subscription refresh, baselines,
 * extend-config / proxy / rules templates, auto-update preference.
 */
export {
  ActivateProfile,
  DeleteProfile,
  DeriveAgePublicKey,
  GenerateAgeKeyPair,
  GetProfilePaths,
  GetProfileProxyGroupsBaseline,
  GetProfileRulesBaseline,
  ImportProfileFromText,
  ImportProfileFromURL,
  ReadProfileConfig,
  RefreshProfileSubscription,
  SetProfileAgeSecretKey,
  SetProfileAutoUpdate,
  SetProfileMergeTemplate,
  SetProfileProxyTemplate,
  SetProfileRulesTemplate,
  UpdateProfileInfo,
  WriteProfileConfig,
} from '../../wailsjs/go/main/App'
