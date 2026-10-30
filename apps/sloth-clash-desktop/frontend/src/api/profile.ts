/**
 * Profile lifecycle: CRUD, activation, subscription refresh, baselines,
 * extend-config / proxy / rules templates, the JavaScript override, auto-update
 * preference.
 */
import { CreateDesyncOnlyProfile as createDesyncOnly } from '../../wailsjs/go/main/App'

import type { DesyncSettings } from './desync'
import { main } from './models'

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
  ImportProfileFromURLWithDesync,
  PeekSubscriptionFromURL,
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

/**
 * Create a Desync-only profile with what it unblocks (Desync is always on in
 * it). An empty name gets the backend's default; the UI passes its localized
 * one.
 */
export const CreateDesyncOnlyProfile = (
  name: string,
  settings: DesyncSettings,
): Promise<main.AppState> =>
  createDesyncOnly(name, main.DesyncSettings.createFrom(settings))

/** Deep-link subscription import, performed only after the user confirms. */
export { ConfirmInstallConfigFromLink } from '../../wailsjs/go/main/App'
