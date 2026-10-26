import { type CSSProperties, type KeyboardEvent, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { useDialogFocus } from '../hooks/useDialogFocus'

export type ImportMode = 'url' | 'paste'

const MODES: readonly ImportMode[] = ['url', 'paste']

const SCHEME_RE = /\b(?:vless|vmess|ss|trojan|hysteria2|hy2|tuic):\/\//gi

function countLinks(s: string): number {
  const m = s.match(SCHEME_RE)
  return m ? m.length : 0
}

function looksLikeYaml(s: string): boolean {
  return /^[ \t]*(proxies|proxy-groups|proxy-providers|rule-providers|rules|dns|tun)\s*:/m.test(
    s,
  )
}

function tryBase64(s: string): string {
  try {
    return atob(s.replace(/\s+/g, ''))
  } catch {
    return ''
  }
}

type Detection = { kind: 'yaml' | 'links' | 'unknown'; label: string }

type TranslateFn = (key: string, options?: Record<string, unknown>) => string

function detect(raw: string, t: TranslateFn): Detection | null {
  const trimmed = raw.trim()
  if (!trimmed) return null
  if (looksLikeYaml(trimmed))
    return { kind: 'yaml', label: t('ui.profiles.importModal.detectYaml') }
  const n = countLinks(trimmed)
  if (n > 0)
    return {
      kind: 'links',
      label: t(
        n > 1
          ? 'ui.profiles.importModal.detectLinksPlural'
          : 'ui.profiles.importModal.detectLinksSingular',
        { count: n },
      ),
    }
  const dec = tryBase64(trimmed)
  if (dec) {
    const m = countLinks(dec)
    if (m > 0)
      return {
        kind: 'links',
        label: t(
          m > 1
            ? 'ui.profiles.importModal.detectLinksBase64Plural'
            : 'ui.profiles.importModal.detectLinksBase64Singular',
          { count: m },
        ),
      }
  }
  return {
    kind: 'unknown',
    label: t('ui.profiles.importModal.detectUnknown'),
  }
}

const tabId = (m: ImportMode) => `importTab-${m}`

/**
 * Add a profile: a subscription URL, or a pasted / loaded config. Below the
 * actions, a quiet way out for someone without a VPN subscription: the
 * Desync-only onboarding.
 */
export function ImportProfileModal({
  open,
  title,
  blurb,
  mode,
  url,
  name,
  content,
  busy,
  onModeChange,
  onUrlChange,
  onNameChange,
  onContentChange,
  onPasteFromClipboard,
  onClose,
  onSubmit,
  onStartDesyncOnly,
  hidePaste,
}: {
  open: boolean
  title: string
  blurb: string
  mode: ImportMode
  url: string
  name: string
  content: string
  busy: boolean
  // Brand hide flag: drop the paste / local-config mode and the Desync-only
  // entry (both make a local profile); URL-only import.
  hidePaste?: boolean
  onModeChange: (next: ImportMode) => void
  onUrlChange: (next: string) => void
  onNameChange: (next: string) => void
  onContentChange: (next: string) => void
  onPasteFromClipboard: () => void
  onClose: () => void
  onSubmit: () => void
  /** "No VPN subscription?": leave for the Desync-only onboarding. */
  onStartDesyncOnly?: () => void
}) {
  const { t } = useTranslation()
  const fileRef = useRef<HTMLInputElement>(null)
  const cardRef = useRef<HTMLDivElement>(null)
  const overlayDownRef = useRef(false)
  const close = busy ? undefined : onClose
  useDialogFocus(cardRef, open, close)
  if (!open) return null

  const effectiveMode: ImportMode = hidePaste ? 'url' : mode
  const detection = effectiveMode === 'paste' ? detect(content, t) : null
  const canSubmit =
    !busy &&
    (effectiveMode === 'url'
      ? url.trim().length > 0
      : content.trim().length > 0 && detection?.kind !== 'unknown')

  const onFilePicked = async (file: File | undefined) => {
    if (!file) return
    const text = await file.text()
    onContentChange(text)
  }

  // Tabs pattern: arrows / Home / End move between the two modes.
  const onTabKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    const i = MODES.indexOf(effectiveMode)
    const next =
      e.key === 'ArrowRight'
        ? MODES[(i + 1) % MODES.length]
        : e.key === 'ArrowLeft'
          ? MODES[(i + MODES.length - 1) % MODES.length]
          : e.key === 'Home'
            ? MODES[0]
            : e.key === 'End'
              ? MODES[MODES.length - 1]
              : null
    if (!next) return
    e.preventDefault()
    onModeChange(next)
    document.getElementById(tabId(next))?.focus()
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
        // dragged out of a field must not close the dialog.
        if (e.target === e.currentTarget && overlayDownRef.current) close?.()
        overlayDownRef.current = false
      }}
    >
      <div
        ref={cardRef}
        className="modalCard importModal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="importTitle"
        aria-describedby="importBlurb"
        tabIndex={-1}
      >
        <h3 id="importTitle" className="modalTitle">
          {title}
        </h3>
        <p id="importBlurb" className="muted small importBlurb">
          {blurb}
        </p>

        {hidePaste ? null : (
          <div
            className="segmentInset segmentInset2 importModeSwitch"
            role="tablist"
            aria-label={t('ui.profiles.importModal.sourceAria')}
          >
            <div
              className="segmentGlider"
              aria-hidden
              style={
                {
                  '--seg-i': MODES.indexOf(effectiveMode),
                } as CSSProperties
              }
            />
            {MODES.map((m) => (
              <button
                key={m}
                id={tabId(m)}
                type="button"
                role="tab"
                aria-selected={effectiveMode === m}
                aria-controls="importPanel"
                tabIndex={effectiveMode === m ? 0 : -1}
                className={
                  effectiveMode === m
                    ? 'segmentInsetBtn isOn'
                    : 'segmentInsetBtn'
                }
                onClick={() => onModeChange(m)}
                onKeyDown={onTabKeyDown}
              >
                {m === 'url'
                  ? t('ui.profiles.importModal.tabUrl')
                  : t('ui.profiles.importModal.tabPaste')}
              </button>
            ))}
          </div>
        )}

        <div
          id="importPanel"
          className="importPanel"
          role={hidePaste ? undefined : 'tabpanel'}
          aria-labelledby={hidePaste ? undefined : tabId(effectiveMode)}
        >
          {effectiveMode === 'url' ? (
            <div className="field modalField">
              <label className="fieldLab" htmlFor="importUrl">
                {t('ui.profiles.importModal.subscriptionUrl')}
              </label>
              <input
                id="importUrl"
                className="input"
                value={url}
                onChange={(e) => onUrlChange(e.target.value)}
                placeholder="https://…"
                aria-describedby="importHelper"
                autoComplete="off"
                spellCheck={false}
                data-autofocus
              />
              <p id="importHelper" className="importHelper">
                {t('ui.profiles.importModal.helperUrl')}
              </p>
            </div>
          ) : (
            <div className="field modalField">
              <label className="fieldLab" htmlFor="importContent">
                {t('ui.profiles.importModal.configOrLinks')}
              </label>
              <textarea
                id="importContent"
                className="input importTextarea"
                value={content}
                rows={10}
                spellCheck={false}
                onChange={(e) => onContentChange(e.target.value)}
                placeholder={t('ui.profiles.importModal.pastePlaceholder')}
                aria-describedby="importHelper"
                aria-invalid={detection?.kind === 'unknown' ? true : undefined}
                data-autofocus
              />
              {/* One line kept for the detection result, so the layout
                  does not jump while typing. */}
              <p
                id="importHelper"
                className={`importHelper${
                  detection
                    ? ` importDetect importDetect-${detection.kind}`
                    : ''
                }`}
                aria-live="polite"
              >
                {detection
                  ? detection.label
                  : t('ui.profiles.importModal.helperPaste')}
              </p>
            </div>
          )}

          <label className="field modalField">
            <span className="fieldLab">
              {t('ui.profiles.importModal.displayName')}{' '}
              <span className="optional">{t('common.optional')}</span>
            </span>
            <input
              className="input"
              value={name}
              onChange={(e) => onNameChange(e.target.value)}
              placeholder={
                effectiveMode === 'url'
                  ? t('ui.profiles.importModal.namePlaceholderUrl')
                  : t('ui.profiles.importModal.namePlaceholderPaste')
              }
            />
          </label>
        </div>

        <div className="modalFooter importFooter">
          <div className="modalFooterLeft">
            <button
              type="button"
              className="btn btnModalSecondary"
              onClick={onPasteFromClipboard}
            >
              {t('ui.profiles.importModal.pasteFromClipboard')}
            </button>
            {effectiveMode === 'paste' ? (
              <>
                <button
                  type="button"
                  className="btn btnModalSecondary"
                  onClick={() => fileRef.current?.click()}
                >
                  {t('ui.profiles.importModal.loadFromFile')}
                </button>
                <input
                  ref={fileRef}
                  type="file"
                  accept=".yaml,.yml,.txt,.conf,text/*"
                  style={{ display: 'none' }}
                  onChange={(e) => {
                    void onFilePicked(e.target.files?.[0])
                    e.target.value = ''
                  }}
                />
              </>
            ) : null}
          </div>
          <div className="modalFooterRight">
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
              disabled={!canSubmit}
              onClick={onSubmit}
            >
              {t('common.import')}
            </button>
          </div>
        </div>

        {hidePaste || !onStartDesyncOnly ? null : (
          <p className="desyncAltLine importAlt">
            <span>{t('ui.profiles.importModal.desyncAltQuestion')}</span>
            <button
              type="button"
              className="linkBtn"
              disabled={busy}
              onClick={onStartDesyncOnly}
            >
              {t('ui.profiles.importModal.desyncAltLink')}
              <span aria-hidden> →</span>
            </button>
          </p>
        )}
      </div>
    </div>
  )
}
