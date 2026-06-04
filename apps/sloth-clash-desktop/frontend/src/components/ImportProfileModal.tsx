import { useRef } from 'react'

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

function detect(raw: string): Detection | null {
  const t = raw.trim()
  if (!t) return null
  if (looksLikeYaml(t)) return { kind: 'yaml', label: 'Clash YAML config' }
  const n = countLinks(t)
  if (n > 0)
    return { kind: 'links', label: `${n} share link${n > 1 ? 's' : ''}` }
  const dec = tryBase64(t)
  if (dec) {
    const m = countLinks(dec)
    if (m > 0)
      return {
        kind: 'links',
        label: `${m} share link${m > 1 ? 's' : ''} (base64)`,
      }
  }
  return {
    kind: 'unknown',
    label:
      'Unrecognized — expecting Clash YAML or vless:// / vmess:// / ss:// / trojan:// links',
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
  const fileRef = useRef<HTMLInputElement>(null)
  if (!open) return null

  const detection = mode === 'paste' ? detect(content) : null
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
            Subscription URL
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === 'paste'}
            className={mode === 'paste' ? 'btn' : 'btn ghost'}
            onClick={() => onModeChange('paste')}
          >
            Paste / File
          </button>
        </div>

        {mode === 'url' ? (
          <label className="field modalField">
            <span className="fieldLab">Subscription URL</span>
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
              Config or share links
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
              placeholder={
                'Paste a mihomo/Clash config.yaml,\nor share links — one per line:\nvless://…\nvmess://…\ntrojan://…'
              }
            />
          </label>
        )}

        <label className="field modalField">
          <span className="fieldLab">
            Display name <span className="optional">(optional)</span>
          </span>
          <input
            className="input"
            value={name}
            onChange={(e) => onNameChange(e.target.value)}
            placeholder={
              mode === 'url'
                ? 'Empty = use Profile-Title from server'
                : 'Empty = "Local config"'
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
              Paste from clipboard
            </button>
            {mode === 'paste' ? (
              <>
                <button
                  type="button"
                  className="btn btnModalSecondary"
                  onClick={() => fileRef.current?.click()}
                >
                  Load from file…
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
              Cancel
            </button>
            <button
              type="button"
              className="btn primary"
              disabled={!canSubmit}
              onClick={onSubmit}
            >
              Import
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
