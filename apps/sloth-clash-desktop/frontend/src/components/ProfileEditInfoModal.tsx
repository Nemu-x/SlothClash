import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

export type ProfileEditInfoTarget = { id: string; name: string; url: string }

// Collapsible "AGE encryption" block. Remounted per profile (key=target.id) so
// reveal/public state never leaks between profiles. Secret (top) and derived
// public key (bottom, read-only) are separate fields; Generate replaces the
// secret in the FORM only — nothing persists until Save.
function AgeKeySection({
  ageKey,
  onAgeKeyChange,
  onGenerateAgeKeyPair,
  onDeriveAgePublicKey,
  onCopyText,
}: {
  ageKey: string
  onAgeKeyChange: (next: string) => void
  onGenerateAgeKeyPair: (
    kind: string,
  ) => Promise<{ publicKey: string; secretKey: string }>
  onDeriveAgePublicKey: (secret: string) => Promise<string>
  onCopyText: (text: string) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(() => Boolean(ageKey.trim()))
  const [reveal, setReveal] = useState(false)
  const [keyKind, setKeyKind] = useState('x25519')
  const [genBusy, setGenBusy] = useState(false)
  const [publicKey, setPublicKey] = useState('')

  // Live-derive the public half from whatever secret is in the field, so the
  // user can copy it for the provider at ANY time (not only right after
  // generation). Invalid/partial input just blanks the public row.
  useEffect(() => {
    if (!open) return
    let cancelled = false
    void (async () => {
      const secret = ageKey.trim()
      if (!secret) {
        if (!cancelled) setPublicKey('')
        return
      }
      try {
        const pub = await onDeriveAgePublicKey(secret)
        if (!cancelled) setPublicKey(String(pub ?? ''))
      } catch {
        if (!cancelled) setPublicKey('')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [ageKey, open, onDeriveAgePublicKey])

  const hasKey = Boolean(ageKey.trim())

  const generate = async () => {
    setGenBusy(true)
    try {
      const pair = await onGenerateAgeKeyPair(keyKind)
      onAgeKeyChange(pair.secretKey)
      setReveal(false)
    } catch {
      /* surfaced by the caller's error channel */
    } finally {
      setGenBusy(false)
    }
  }

  if (!open) {
    return (
      <button
        type="button"
        className="btn btnModalSecondary"
        style={{ marginTop: 12 }}
        onClick={() => setOpen(true)}
      >
        {t('ui.profiles.editInfo.ageSection')}
      </button>
    )
  }

  return (
    <div className="modalAgeSection">
      <label className="field modalField">
        <span className="fieldLab">{t('ui.profiles.editInfo.ageKey')}</span>
        <div style={{ display: 'flex', gap: 8 }}>
          <input
            className="input"
            style={{ flex: 1 }}
            type={reveal ? 'text' : 'password'}
            autoComplete="off"
            spellCheck={false}
            value={ageKey}
            onChange={(e) => onAgeKeyChange(e.target.value)}
            placeholder={t('ui.profiles.editInfo.ageKeyPlaceholder')}
          />
          <button
            type="button"
            className="btn btnModalSecondary"
            onClick={() => setReveal((v) => !v)}
          >
            {reveal
              ? t('ui.profiles.editInfo.ageHide')
              : t('ui.profiles.editInfo.ageReveal')}
          </button>
        </div>
      </label>
      <label className="field modalField">
        <span className="fieldLab">
          {t('ui.profiles.editInfo.agePublicLabel')}
        </span>
        <input
          className="input"
          readOnly
          spellCheck={false}
          value={publicKey}
          placeholder={t('ui.profiles.editInfo.agePublicPlaceholder')}
          onFocus={(e) => e.target.select()}
        />
      </label>
      <div className="modalActions">
        <select
          className="input"
          style={{ width: 'auto' }}
          aria-label={t('ui.profiles.editInfo.ageKeyType')}
          value={keyKind}
          onChange={(e) => setKeyKind(e.target.value)}
        >
          <option value="x25519">X25519</option>
          <option value="hybrid">MLKEM768-X25519</option>
        </select>
        <button
          type="button"
          className="btn btnModalSecondary"
          disabled={genBusy}
          onClick={() => void generate()}
        >
          {t('ui.profiles.editInfo.generateAgePair')}
        </button>
        {publicKey ? (
          <button
            type="button"
            className="btn btnModalSecondary"
            onClick={() => onCopyText(publicKey)}
          >
            {t('ui.profiles.editInfo.copyAgePublic')}
          </button>
        ) : null}
        {hasKey ? (
          <button
            type="button"
            className="btn btnModalSecondary"
            onClick={() => onAgeKeyChange('')}
          >
            {t('ui.profiles.editInfo.ageClear')}
          </button>
        ) : null}
      </div>
    </div>
  )
}

export function ProfileEditInfoModal({
  target,
  name,
  url,
  autoEnabled,
  autoInterval,
  ageKey,
  onAgeKeyChange,
  onGenerateAgeKeyPair,
  onDeriveAgePublicKey,
  onNameChange,
  onUrlChange,
  onAutoEnabledToggle,
  onAutoIntervalChange,
  onCopyUrl,
  onCopyName,
  onClose,
  onSave,
}: {
  target: ProfileEditInfoTarget | null
  name: string
  url: string
  autoEnabled: boolean
  autoInterval: string
  ageKey: string
  onAgeKeyChange: (next: string) => void
  onGenerateAgeKeyPair: (
    kind: string,
  ) => Promise<{ publicKey: string; secretKey: string }>
  onDeriveAgePublicKey: (secret: string) => Promise<string>
  onNameChange: (next: string) => void
  onUrlChange: (next: string) => void
  onAutoEnabledToggle: () => void
  onAutoIntervalChange: (next: string) => void
  onCopyUrl: (url: string) => void
  onCopyName: (name: string) => void
  onClose: () => void
  onSave: (id: string) => void
}) {
  const { t } = useTranslation()
  if (!target) return null
  return (
    <div
      className="modalOverlay"
      role="presentation"
      style={{ zIndex: 70 }}
      onClick={onClose}
    >
      <div
        className="modalCard"
        role="dialog"
        aria-modal="true"
        aria-labelledby="editInfoTitle"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 id="editInfoTitle" className="modalTitle">
          {t('ui.profiles.editInfo.title')}
        </h3>
        <p className="muted small">{t('ui.profiles.editInfo.blurb')}</p>
        <label className="field modalField">
          <span className="fieldLab">{t('ui.profiles.editInfo.name')}</span>
          <input
            className="input"
            value={name}
            onChange={(e) => onNameChange(e.target.value)}
            autoFocus
          />
        </label>
        <label className="field modalField">
          <span className="fieldLab">
            {t('ui.profiles.editInfo.subscriptionUrl')}
          </span>
          <input
            className="input"
            value={url}
            onChange={(e) => onUrlChange(e.target.value)}
            placeholder={t('ui.profiles.editInfo.urlPlaceholder')}
          />
        </label>
        <div className="fieldGrid">
          <label className="field modalField">
            <span className="fieldLab">
              {t('ui.profiles.editInfo.autoUpdate')}
            </span>
            <button
              type="button"
              className={`trafficKnob ${autoEnabled ? 'on' : ''}`}
              onClick={onAutoEnabledToggle}
            >
              {autoEnabled ? t('common.on') : t('common.off')}
            </button>
          </label>
          <label className="field modalField">
            <span className="fieldLab">
              {t('ui.profiles.editInfo.intervalMinutes')}
            </span>
            <input
              className="input"
              value={autoInterval}
              onChange={(e) => onAutoIntervalChange(e.target.value)}
              placeholder="360"
            />
          </label>
        </div>
        <AgeKeySection
          key={target.id}
          ageKey={ageKey}
          onAgeKeyChange={onAgeKeyChange}
          onGenerateAgeKeyPair={onGenerateAgeKeyPair}
          onDeriveAgePublicKey={onDeriveAgePublicKey}
          onCopyText={onCopyName}
        />
        <div className="modalActions">
          <button
            type="button"
            className="btn btnModalSecondary"
            disabled={!url.trim()}
            onClick={() => onCopyUrl(url.trim())}
          >
            {t('ui.profiles.editInfo.copyUrl')}
          </button>
          <button
            type="button"
            className="btn btnModalSecondary"
            disabled={!name.trim()}
            onClick={() => onCopyName(name.trim())}
          >
            {t('ui.profiles.editInfo.copyName')}
          </button>
        </div>
        <div className="modalFooter">
          <div className="modalFooterRight" style={{ width: '100%' }}>
            <button type="button" className="btn ghost" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className="btn primary"
              disabled={!name.trim()}
              onClick={() => onSave(target.id)}
            >
              {t('common.save')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
