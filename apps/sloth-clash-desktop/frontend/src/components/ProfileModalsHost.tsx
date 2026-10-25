import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  ConfirmInstallConfigFromLink,
  DeleteProfile,
  DeriveAgePublicKey,
  GenerateAgeKeyPair,
  ImportProfileFromText,
  ImportProfileFromURL,
  ReadProfileConfig,
  RefreshProfileSubscription,
  SetProfileAgeSecretKey,
  SetProfileAutoUpdate,
  SetProfileMergeTemplate,
  UpdateProfileInfo,
  WriteProfileConfig,
} from '../api/profile'
import { EventsOn } from '../api/runtime'
import type { ProfileModals } from '../hooks/useProfileModals'
import type { Toast } from '../hooks/useToasts'
import {
  DEFAULT_MERGE_TEMPLATE,
  mergeTemplateFromProfile,
} from '../mergeTemplate'
import { friendlyErrorMessage, yamlValidationError } from '../utils/yaml'

import { DeleteProfileModal } from './DeleteProfileModal'
import { ImportProfileModal, type ImportMode } from './ImportProfileModal'
import {
  type InstallConfigRequest,
  InstallConfigConfirmModal,
} from './InstallConfigConfirmModal'
import { ProfileContextMenu } from './ProfileContextMenu'
import { ProfileEditInfoModal } from './ProfileEditInfoModal'
import { ProfileFileModal } from './ProfileFileModal'
import { ProfileMergeModal } from './ProfileMergeModal'
import { ProfileProxyModal } from './ProfileProxyModal'
import { ProfileRulesModal } from './ProfileRulesModal'
import { ProfileScriptModal } from './ProfileScriptModal'

/**
 * The profile dialogs: import (manual and deep-link confirm), the per-profile
 * editors (merge template, config file, proxy groups, rules, script, info),
 * delete confirm and the profile context menu. Which one is open comes from
 * `modals` (useProfileModals, held by App); the drafts edited inside live
 * here.
 */
export function ProfileModalsHost({
  modals,
  profiles,
  proxyGroups,
  hidePaste,
  run,
  refresh,
  setError,
  setBanner,
  pushToast,
  onImported,
}: {
  modals: ProfileModals
  profiles: any[] | undefined
  proxyGroups: any[] | undefined
  hidePaste?: boolean
  /** App's action wrapper: clears the error, runs, surfaces a failure, refreshes. */
  run: (action: () => Promise<any>) => Promise<void>
  refresh: () => Promise<void>
  setError: (msg: string) => void
  /** Settings status line. */
  setBanner: (msg: string) => void
  pushToast: (toast: Omit<Toast, 'id'>) => number
  /** A profile was imported from the import dialog. */
  onImported: () => void
}) {
  const { t } = useTranslation()
  const [importName, setImportName] = useState('')
  const [importUrl, setImportUrl] = useState('')
  const [importMode, setImportMode] = useState<ImportMode>('url')
  const [importContent, setImportContent] = useState('')
  const [importBusy, setImportBusy] = useState(false)
  const [mergeTemplateDraft, setMergeTemplateDraft] = useState('')
  const [mergeTemplateYamlErr, setMergeTemplateYamlErr] = useState<
    string | null
  >(null)
  const [profileFileText, setProfileFileText] = useState('')
  const [profileFileYamlErr, setProfileFileYamlErr] = useState<string | null>(
    null,
  )
  const [profileFilePath, setProfileFilePath] = useState('')
  const [profileFileLoadErr, setProfileFileLoadErr] = useState('')
  const [profileEditName, setProfileEditName] = useState('')
  const [profileEditUrl, setProfileEditUrl] = useState('')
  const [profileEditAutoEnabled, setProfileEditAutoEnabled] = useState(true)
  const [profileEditAutoInterval, setProfileEditAutoInterval] = useState('360')
  const [profileEditAgeKey, setProfileEditAgeKey] = useState('')
  const [installConfigRequest, setInstallConfigRequest] =
    useState<InstallConfigRequest | null>(null)
  const [installConfigBusy, setInstallConfigBusy] = useState(false)
  const profileMergeModal = modals.merge
  const profileFileModal = modals.file
  const profileEditInfo = modals.editInfo
  const profileMenu = modals.menu

  // The YAML errors and the draft seeding below are effects carried over from
  // App.tsx unchanged (lint-relaxed there): the errors trail the draft by one
  // commit, and a draft is seeded only when its modal opens, never on a later
  // state refresh.
  /* eslint-disable @eslint-react/set-state-in-effect */
  useEffect(() => {
    setMergeTemplateYamlErr(yamlValidationError(mergeTemplateDraft, true))
  }, [mergeTemplateDraft])

  useEffect(() => {
    setProfileFileYamlErr(yamlValidationError(profileFileText, true))
  }, [profileFileText])
  /* eslint-enable @eslint-react/set-state-in-effect */

  // A deep link never imports silently: the backend asks first (audit SEC1).
  useEffect(() => {
    const off = EventsOn('app:install-config-request', (payload: unknown) => {
      const p = payload as { name?: string; url?: string; host?: string }
      if (!p?.url) return
      setInstallConfigRequest({
        name: String(p.name ?? ''),
        url: String(p.url),
        host: String(p.host ?? p.url),
      })
    })
    return () => off()
  }, [])

  const confirmInstallConfig = useCallback(
    async (req: InstallConfigRequest) => {
      setInstallConfigBusy(true)
      try {
        await ConfirmInstallConfigFromLink(req.name, req.url)
      } catch (e) {
        pushToast({
          kind: 'error',
          message: friendlyErrorMessage(String(e)),
          durationMs: 0,
        })
      } finally {
        setInstallConfigBusy(false)
        setInstallConfigRequest(null)
      }
    },
    [pushToast],
  )

  /* eslint-disable @eslint-react/set-state-in-effect, @eslint-react/exhaustive-deps, react-hooks/exhaustive-deps */
  useEffect(() => {
    if (!profileMergeModal) return
    const raw = mergeTemplateFromProfile(profiles, profileMergeModal.id)
    setMergeTemplateDraft(raw.trim() ? raw : DEFAULT_MERGE_TEMPLATE)
    // Intentionally only when opening the modal — avoid overwriting edits on refresh.
  }, [profileMergeModal])

  useEffect(() => {
    if (!profileEditInfo) return
    setProfileEditName(profileEditInfo.name)
    setProfileEditUrl(profileEditInfo.url)
    setProfileEditAgeKey(
      String(
        profiles?.find((x: any) => x.id === profileEditInfo.id)?.ageSecretKey ??
          '',
      ),
    )
    const p = profiles?.find((x: any) => x.id === profileEditInfo.id)
    const interval = Number(p?.autoUpdateIntervalMinutes ?? 360)
    setProfileEditAutoEnabled(p?.autoUpdateEnabled === false ? false : true)
    setProfileEditAutoInterval(
      String(Number.isFinite(interval) && interval > 0 ? interval : 360),
    )
  }, [profileEditInfo])
  /* eslint-enable @eslint-react/set-state-in-effect, @eslint-react/exhaustive-deps, react-hooks/exhaustive-deps */

  useEffect(() => {
    if (!profileFileModal) return
    let cancelled = false
    void (async () => {
      setProfileFileLoadErr('')
      setProfileFilePath('')
      setProfileFileText('')
      try {
        const peek = await ReadProfileConfig(profileFileModal.id)
        if (cancelled) return
        setProfileFilePath(peek.path ?? '')
        if (peek.lastError) {
          setProfileFileLoadErr(peek.lastError)
          setProfileFileText(
            `# config.yaml not found yet (${peek.lastError}).\n# Connect once to generate, then open again — or paste a full profile below.\n`,
          )
        } else {
          setProfileFileText(peek.body ?? '')
        }
      } catch (e: any) {
        if (cancelled) return
        setProfileFileLoadErr(String(e))
        setProfileFileText(
          '# Could not load current config.yaml. Paste YAML below to overwrite.\n',
        )
      }
    })()
    return () => {
      cancelled = true
    }
  }, [profileFileModal])

  const closeImportModal = () => {
    modals.closeImport()
    setImportBusy(false)
  }

  const pasteFromClipboard = async () => {
    setError('')
    try {
      const text = await navigator.clipboard.readText()
      if (importMode === 'paste') setImportContent(text)
      else setImportUrl(text.trim())
    } catch {
      setError(t('ui.toasts.clipboardReadFail'))
    }
  }

  const performImportAndClose = async () => {
    setError('')
    setImportBusy(true)
    try {
      const name = importName.trim()
      if (importMode === 'paste') {
        await ImportProfileFromText(name, importContent.trim())
      } else {
        await ImportProfileFromURL(name, importUrl.trim())
      }
      await refresh()
      onImported()
      setImportUrl('')
      setImportName('')
      setImportContent('')
      closeImportModal()
    } catch (e: any) {
      setError(String(e))
      await refresh()
    } finally {
      setImportBusy(false)
    }
  }

  const importModalTitle = () => {
    if (modals.importReason === 'connect') {
      return t('ui.import.connectNeedsProfile')
    }
    if (modals.importReason === 'beacon') {
      return t('ui.import.addFirstSubscription')
    }
    return t('ui.import.importSubscription')
  }

  const importModalBlurb = () => {
    if (modals.importReason === 'connect') {
      return t('ui.import.connectBlurb')
    }
    return t('ui.import.defaultBlurb')
  }

  return (
    <>
      <ImportProfileModal
        open={modals.importOpen}
        title={importModalTitle()}
        blurb={importModalBlurb()}
        mode={importMode}
        url={importUrl}
        name={importName}
        content={importContent}
        busy={importBusy}
        hidePaste={hidePaste}
        onModeChange={setImportMode}
        onUrlChange={setImportUrl}
        onNameChange={setImportName}
        onContentChange={setImportContent}
        onPasteFromClipboard={() => pasteFromClipboard()}
        onClose={closeImportModal}
        onSubmit={() => performImportAndClose()}
      />

      <ProfileMergeModal
        target={profileMergeModal}
        value={mergeTemplateDraft}
        yamlError={mergeTemplateYamlErr}
        onChange={setMergeTemplateDraft}
        onResetScaffold={() => setMergeTemplateDraft(DEFAULT_MERGE_TEMPLATE)}
        onClose={() => modals.setMerge(null)}
        onSave={async (id) => {
          if (mergeTemplateYamlErr) return
          setError('')
          try {
            await SetProfileMergeTemplate(id, mergeTemplateDraft)
            modals.setMerge(null)
            setBanner(
              'Merge template saved. Reconnect if you are already connected.',
            )
          } catch (e: any) {
            setError(String(e))
          }
          await refresh()
        }}
      />

      <ProfileFileModal
        target={profileFileModal}
        path={profileFilePath}
        loadError={profileFileLoadErr}
        value={profileFileText}
        yamlError={profileFileYamlErr}
        onChange={setProfileFileText}
        onCopyPath={(path) => {
          if (path) {
            void navigator.clipboard.writeText(path)
            setBanner(t('ui.toasts.configPathCopied'))
          }
        }}
        onClose={() => modals.setFile(null)}
        onSave={async (id) => {
          if (profileFileYamlErr) return
          setError('')
          try {
            await WriteProfileConfig(id, profileFileText)
            modals.setFile(null)
            setBanner(
              'Config saved. Sloth reapplies ports and secret on connect.',
            )
          } catch (e: any) {
            setError(String(e))
          }
          await refresh()
        }}
      />

      <ProfileProxyModal
        key={`pgm-${modals.proxy?.id ?? 'closed'}`}
        target={modals.proxy}
        profiles={profiles}
        onClose={() => modals.setProxy(null)}
        onSaved={(banner) => {
          modals.setProxy(null)
          setBanner(banner)
          void refresh()
        }}
        onError={(msg) => setError(msg)}
      />
      <ProfileRulesModal
        key={`prm-${modals.rules?.id ?? 'closed'}`}
        target={modals.rules}
        profiles={profiles}
        proxyGroups={proxyGroups}
        onClose={() => modals.setRules(null)}
        onSaved={(banner) => {
          modals.setRules(null)
          setBanner(banner)
          void refresh()
        }}
        onError={(msg) => setError(msg)}
      />
      <ProfileScriptModal
        key={`psm-${modals.script?.id ?? 'closed'}`}
        target={modals.script}
        profiles={profiles}
        onClose={() => modals.setScript(null)}
        onSaved={(banner) => {
          modals.setScript(null)
          setBanner(banner)
          void refresh()
        }}
        onError={(msg) => setError(msg)}
      />
      <ProfileEditInfoModal
        target={profileEditInfo}
        name={profileEditName}
        url={profileEditUrl}
        autoEnabled={profileEditAutoEnabled}
        autoInterval={profileEditAutoInterval}
        ageKey={profileEditAgeKey}
        onAgeKeyChange={setProfileEditAgeKey}
        onGenerateAgeKeyPair={async (kind) => {
          const pair = await GenerateAgeKeyPair(kind)
          return {
            publicKey: String(pair?.publicKey ?? ''),
            secretKey: String(pair?.secretKey ?? ''),
          }
        }}
        onDeriveAgePublicKey={async (secret) =>
          String((await DeriveAgePublicKey(secret)) ?? '')
        }
        onNameChange={setProfileEditName}
        onUrlChange={setProfileEditUrl}
        onAutoEnabledToggle={() => setProfileEditAutoEnabled((v) => !v)}
        onAutoIntervalChange={setProfileEditAutoInterval}
        onCopyUrl={(url) => {
          if (url) {
            void navigator.clipboard.writeText(url)
            setBanner(t('ui.toasts.urlCopied'))
          }
        }}
        onCopyName={(name) => {
          if (name) {
            void navigator.clipboard.writeText(name)
            setBanner(t('ui.toasts.nameCopied'))
          }
        }}
        onClose={() => modals.setEditInfo(null)}
        onSave={(id) => {
          void run(async () => {
            await UpdateProfileInfo(
              id,
              profileEditName.trim(),
              profileEditUrl.trim(),
            )
            await SetProfileAgeSecretKey(id, profileEditAgeKey.trim())
            const interval = Number(profileEditAutoInterval || '360')
            await SetProfileAutoUpdate(
              id,
              profileEditAutoEnabled,
              Number.isFinite(interval) && interval > 0 ? interval : 360,
            )
          })
          modals.setEditInfo(null)
        }}
      />

      <InstallConfigConfirmModal
        request={installConfigRequest}
        busy={installConfigBusy}
        onCancel={() => setInstallConfigRequest(null)}
        onConfirm={(req) => void confirmInstallConfig(req)}
      />

      <DeleteProfileModal
        target={modals.remove}
        onClose={() => modals.setRemove(null)}
        onConfirm={(id) => {
          modals.setRemove(null)
          void run(() => DeleteProfile(id))
        }}
      />

      <ProfileContextMenu
        target={profileMenu}
        profile={
          profileMenu
            ? (profiles?.find((x: any) => x.id === profileMenu.id) ?? null)
            : null
        }
        onUpdate={(id) => {
          modals.setMenu(null)
          void run(() => RefreshProfileSubscription(id))
        }}
        onEditInfo={(id, name, url) => {
          modals.setMenu(null)
          modals.setEditInfo({ id, name, url })
        }}
        onCopyUrl={(url) => {
          if (url) {
            void navigator.clipboard.writeText(url)
            setBanner(t('ui.toasts.urlCopied'))
          }
          modals.setMenu(null)
        }}
        onOpenRules={(id, name) => {
          modals.setMenu(null)
          modals.setRules({ id, name })
        }}
        onDelete={(id, name) => {
          modals.setMenu(null)
          modals.setRemove({ id, name })
        }}
        onOpenExtendConfig={(id, name) => {
          modals.setMenu(null)
          modals.setMerge({ id, name })
        }}
        onOpenProxyGroups={(id, name) => {
          modals.setMenu(null)
          modals.setProxy({ id, name })
        }}
        onOpenScript={(id, name) => {
          modals.setMenu(null)
          modals.setScript({ id, name })
        }}
        onOpenEditFile={(id, name) => {
          modals.setMenu(null)
          modals.setFile({ id, name })
        }}
      />
    </>
  )
}
