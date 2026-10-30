import { useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { DesyncSettings } from '../api/desync'
import { ActivateProfile, CreateDesyncOnlyProfile } from '../api/profile'
import { useDesyncStatus } from '../hooks/queries/useDesyncStatus'
import { useDesyncDraft } from '../hooks/useDesyncDraft'
import { useDialogFocus } from '../hooks/useDialogFocus'
import {
  desyncEnablePatch,
  desyncErrorText,
  desyncHasSelection,
} from '../utils/desyncStatus'
import { isDesyncOnlyProfile } from '../utils/profileKind'

import { DesyncPanel, type DesyncPanelVariant } from './DesyncPanel'

type CreatedProfile = { id: string; name: string; active: boolean }

/**
 * The single home of the Desync feature, opened from the Home chip, the
 * Settings summary and a Desync-only profile card (manage), or from "No VPN
 * subscription?" in the import dialog and on an empty Home (onboarding).
 *
 * Onboarding ends in "Create profile": a Desync-only profile (always on) is
 * created with the chosen services and becomes the active one. Cancel
 * creates nothing.
 *
 * Rendered only while open (App), so every opening starts fresh.
 */
export function DesyncModal({
  view,
  connected,
  mode,
  traffic,
  profiles,
  refresh,
  onClose,
  onCreated,
  fallbackFocus,
}: {
  view: DesyncPanelVariant
  connected: boolean
  mode: string
  traffic: string
  /** The profiles before creation: the new one is the one not among them. */
  profiles: any[] | undefined
  refresh: () => Promise<void>
  onClose: () => void
  /** A Desync-only profile was created and activated. */
  onCreated: (name: string) => void
  /**
   * Where focus goes on close when the element that opened the dialog is
   * gone, e.g. the empty-Home link replaced by the created profile's Home.
   */
  fallbackFocus?: () => HTMLElement | null | undefined
}) {
  const { t } = useTranslation()
  const baseId = useId()
  const titleId = `${baseId}-title`
  const leadId = `${baseId}-lead`
  const cardRef = useRef<HTMLDivElement>(null)
  const overlayDownRef = useRef(false)
  const onboarding = view === 'onboarding'
  const { snapshot } = useDesyncStatus({ connected, mode, traffic })
  // Onboarding: the choices stay here until Create profile.
  const draft = useDesyncDraft(onboarding)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [panelSaving, setPanelSaving] = useState(false)
  const [error, setError] = useState('')
  // A profile created by an attempt whose activation then failed: a retry
  // only activates it, it never creates a second one.
  const createdRef = useRef<CreatedProfile | null>(null)

  const close = busy ? undefined : onClose
  useDialogFocus(cardRef, true, close, fallbackFocus)

  const status = snapshot?.status ?? null
  const chosen = onboarding ? draft.settings : (status?.settings ?? null)
  const hasSelection = chosen ? desyncHasSelection(chosen) : false
  const canCreate =
    status !== null &&
    status.coreSupported === true &&
    hasSelection &&
    !busy &&
    !panelSaving

  async function createProfile(
    selection: DesyncSettings,
  ): Promise<CreatedProfile> {
    if (createdRef.current) return createdRef.current
    const before = new Set((profiles ?? []).map((p: any) => String(p.id)))
    const st = await CreateDesyncOnlyProfile(
      name.trim() || t('desync.onboarding.defaultName'),
      selection,
    )
    const list: any[] = st?.profile?.profiles ?? []
    // The backend appends the new profile: the last Desync-only one that
    // was not there before (the last one at all if `profiles` was stale).
    const made =
      list.findLast(
        (p) => !before.has(String(p.id)) && isDesyncOnlyProfile(p),
      ) ?? list.findLast((p) => isDesyncOnlyProfile(p))
    if (!made) throw new Error(t('desync.onboarding.notCreated'))
    const created = {
      id: String(made.id),
      name: String(made.name ?? ''),
      // The backend activates it itself only when no profile was active.
      active: String(st?.profile?.activeProfileId ?? '') === String(made.id),
    }
    createdRef.current = created
    return created
  }

  async function create() {
    if (!status || !draft.settings || !canCreate) return
    // Same patch as the master switch: a selected Discord gets voice.
    const selection = {
      ...draft.settings,
      ...desyncEnablePatch({ ...status, settings: draft.settings }),
    }
    setBusy(true)
    setError('')
    try {
      const created = await createProfile(selection)
      if (!created.active) {
        await ActivateProfile(created.id)
        created.active = true
      }
      await refresh()
      onCreated(created.name)
      onClose()
    } catch (err: unknown) {
      setError(desyncErrorText(t, err))
      setBusy(false)
      await refresh()
    }
  }

  return (
    <div
      className="modalOverlay"
      role="presentation"
      onMouseDown={(e) => {
        overlayDownRef.current = e.target === e.currentTarget
      }}
      onClick={(e) => {
        // Only a click that started on the backdrop: a text selection
        // dragged out of the dialog must not close it.
        if (e.target === e.currentTarget && overlayDownRef.current) close?.()
        overlayDownRef.current = false
      }}
    >
      <div
        ref={cardRef}
        className="modalCard desyncModal"
        data-variant={view}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={leadId}
        tabIndex={-1}
      >
        <header className="desyncModalHead">
          <h3 id={titleId} className="modalTitle">
            {onboarding ? t('desync.onboarding.title') : t('desync.title')}
          </h3>
          <p id={leadId} className="muted small desyncModalLead">
            {onboarding
              ? t('desync.onboarding.lead')
              : status?.desyncOnly
                ? t('desync.leadDesyncOnly')
                : // A subscription profile: Desync takes these off the VPN.
                  t('desync.lead')}
          </p>
        </header>
        <DesyncPanel
          connected={connected}
          mode={mode}
          traffic={traffic}
          variant={view}
          locked={busy}
          error={error}
          onSavingChange={setPanelSaving}
          draft={onboarding ? draft : undefined}
          footerNote={
            onboarding && status && chosen && !hasSelection ? (
              <span className="small muted desyncFooterNote">
                {t('desync.onboarding.needSelection')}
              </span>
            ) : null
          }
          actions={
            onboarding ? (
              <>
                <button
                  type="button"
                  className="btn ghost"
                  disabled={busy}
                  onClick={onClose}
                >
                  {t('common.cancel')}
                </button>
                <button
                  type="button"
                  className="btn primary"
                  disabled={!canCreate}
                  aria-busy={busy || undefined}
                  onClick={() => void create()}
                >
                  {busy
                    ? t('desync.onboarding.creating')
                    : t('desync.onboarding.create')}
                </button>
              </>
            ) : (
              <button type="button" className="btn ghost" onClick={onClose}>
                {t('common.close')}
              </button>
            )
          }
        >
          {onboarding ? (
            <section className="desyncSection desyncOnboardName">
              <label className="field">
                <span className="fieldLab">
                  {t('desync.onboarding.nameLabel')}{' '}
                  <span className="optional">{t('common.optional')}</span>
                </span>
                <input
                  className="input"
                  value={name}
                  disabled={busy}
                  placeholder={t('desync.onboarding.defaultName')}
                  onChange={(e) => setName(e.target.value)}
                />
              </label>
            </section>
          ) : null}
        </DesyncPanel>
      </div>
    </div>
  )
}
