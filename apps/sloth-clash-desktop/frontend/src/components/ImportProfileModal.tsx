import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

export type ImportMode = 'url' | 'paste'

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
}: {
  open: boolean
  title: string
  blurb: string
  mode: ImportMode
  url: string
  name: string
  content: string
  busy: boolean
  onModeChange: (next: ImportMode) => void
  onUrlChange: (next: string) => void
  onNameChange: (next: string) => void
  onContentChange: (next: string) => void
  onPasteFromClipboard: () => void
  onClose: () => void
  onSubmit: () => void
}) {
  const { t } = useTranslation()
  const fileRef = useRef<HTMLInputElement>(null)
  if (!open) return null

  const detection = mode === 'paste' ? detect(content, t) : null
  const canSubmit =
    !busy &&
    (mode === 'url'
      ? url.trim().length > 0
      : content.trim().length > 0 && detection?.kind !== 'unknown')

  const onFilePicked = async (file: File | undefined) => {
    if (!file) return
    const text = await file.text()
    onContentChange(text)
  }

  return (
    <div className="modalOverlay" role="presentation" onClick={onClose}>
      <div
        className="modalCard"
        role="dialog"
        aria-modal="true"
        aria-labelledby="importTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="importTitle" className="modalTitle">
          {title}
        </h3>
        <p className="muted small">{blurb}</p>

        <div className="segmentInset importModeTabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={mode === 'url'}
            className={mode === 'url' ? 'btn' : 'btn ghost'}
            onClick={() => onModeChange('url')}
          >
            {t('ui.profiles.importModal.tabUrl')}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === 'paste'}
            className={mode === 'paste' ? 'btn' : 'btn ghost'}
            onClick={() => onModeChange('paste')}
          >
            {t('ui.profiles.importModal.tabPaste')}
          </button>
        </div>

        {mode === 'url' ? (
          <label className="field modalField">
            <span className="fieldLab">
              {t('ui.profiles.importModal.subscriptionUrl')}
            </span>
            <input
              className="input"
              value={url}
              onChange={(e) => onUrlChange(e.target.value)}
              placeholder="https://…"
            />
          </label>
        ) : (
          <label className="field modalField">
            <span className="fieldLab">
              {t('ui.profiles.importModal.configOrLinks')}
              {detection ? (
                <span className={`importDetect importDetect-${detection.kind}`}>
                  {' '}
                  · {detection.label}
                </span>
              ) : null}
            </span>
            <textarea
              className="input importTextarea"
              value={content}
              rows={10}
              spellCheck={false}
              onChange={(e) => onContentChange(e.target.value)}
              placeholder={t('ui.profiles.importModal.pastePlaceholder')}
            />
          </label>
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
              mode === 'url'
                ? t('ui.profiles.importModal.namePlaceholderUrl')
                : t('ui.profiles.importModal.namePlaceholderPaste')
            }
          />
        </label>

        <div className="modalFooter">
          <div className="modalFooterLeft">
            <button
              type="button"
              className="btn btnModalSecondary"
              onClick={onPasteFromClipboard}
            >
              {t('ui.profiles.importModal.pasteFromClipboard')}
            </button>
            {mode === 'paste' ? (
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
      </div>
    </div>
  )
}
