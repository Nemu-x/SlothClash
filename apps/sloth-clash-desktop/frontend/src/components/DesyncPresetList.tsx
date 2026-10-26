import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { DesyncPresetView } from '../utils/desyncStatus'

import { DesyncStatusChip } from './DesyncStatusChip'

/** Domains shown before "+N more". */
const SUMMARY_DOMAINS = 2

/**
 * "Popular": one row per catalog preset — name, a one-line domain summary
 * (the full list behind "+N more"), live status in manage, and a switch.
 * Notes under a row: the full domain list, a shared-domain hint, and which
 * plain entries switching it on replaced.
 */
export function DesyncPresetList({
  views,
  onboarding,
  busy,
  supported,
  replaced,
  error,
  onToggle,
}: {
  views: DesyncPresetView[]
  onboarding: boolean
  busy: boolean
  supported: boolean
  /** The last preset switched on and the plain domains it replaced. */
  replaced: { id: string; domains: string[] } | null
  error: string
  onToggle: (view: DesyncPresetView) => void
}) {
  const { t } = useTranslation()
  const baseId = useId()
  const titleId = `${baseId}-title`
  const errorId = `${baseId}-error`
  return (
    <div
      className="desyncSubgroup desyncPresets"
      role="group"
      aria-labelledby={titleId}
    >
      <h5 id={titleId} className="desyncSubTitle">
        {t('desync.presets.title')}
      </h5>
      <ul className="desyncServices desyncPresetList">
        {views.map((view) => (
          <DesyncPresetRow
            key={view.id}
            view={view}
            onboarding={onboarding}
            disabled={busy || (!supported && !view.checked)}
            replaced={replaced?.id === view.id ? replaced.domains : []}
            errorId={error ? errorId : undefined}
            onToggle={() => onToggle(view)}
          />
        ))}
      </ul>
      {error ? (
        <p id={errorId} className="error small desyncPresetError" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  )
}

function DesyncPresetRow({
  view,
  onboarding,
  disabled,
  replaced,
  errorId,
  onToggle,
}: {
  view: DesyncPresetView
  onboarding: boolean
  disabled: boolean
  replaced: string[]
  errorId?: string
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const baseId = useId()
  const domainsId = `${baseId}-domains`
  const allId = `${baseId}-all`
  const [expanded, setExpanded] = useState(false)
  const summary = view.domains.slice(0, SUMMARY_DOMAINS).join(', ')
  const more = view.domains.length - SUMMARY_DOMAINS
  const all = view.domains.join(', ')
  return (
    <li
      className="desyncService desyncPresetRow"
      data-preset={view.id}
      data-checked={view.checked}
    >
      <div className="desyncPresetInfo">
        <span className="desyncServiceName desyncPresetName">{view.name}</span>
        {view.domains.length > 0 ? (
          <span className="desyncPresetDomains">
            <span
              id={domainsId}
              className="desyncPresetDomainsText"
              title={all}
            >
              {summary}
            </span>
            {more > 0 ? (
              <button
                type="button"
                className="desyncPresetMore"
                aria-expanded={expanded}
                aria-controls={allId}
                onClick={() => setExpanded((v) => !v)}
              >
                {expanded
                  ? t('desync.presets.less')
                  : t('desync.presets.more', { count: more })}
              </button>
            ) : null}
          </span>
        ) : null}
      </div>
      <div className="desyncServiceStatus">
        {onboarding || !view.checked ? null : (
          <>
            <DesyncStatusChip view={view.status} />
            {view.status.phase === 'unavailable' ? (
              <span className="desyncCustomNote">
                {t('desync.custom.ipHint')}
              </span>
            ) : null}
          </>
        )}
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={view.checked}
        aria-label={view.name}
        aria-describedby={
          [view.domains.length > 0 ? domainsId : '', errorId ?? '']
            .filter(Boolean)
            .join(' ') || undefined
        }
        className={`settingsSwitch${view.checked ? ' on' : ''}`}
        disabled={disabled}
        onClick={onToggle}
      >
        <span className="settingsSwitchKnob" aria-hidden />
      </button>
      {more > 0 ? (
        <p
          id={allId}
          className="desyncPresetNote desyncPresetAll"
          hidden={!expanded}
        >
          {all}
        </p>
      ) : null}
      {view.shared ? (
        <p className="desyncPresetNote" data-note="shared">
          {t('desync.presets.shared', {
            domains: view.shared.domains.join(', '),
            names: view.shared.names.join(', '),
          })}
        </p>
      ) : null}
      {replaced.length > 0 ? (
        <p className="desyncPresetNote" data-note="replaced" role="status">
          {t('desync.presets.replaced', {
            domains: replaced.join(', '),
            name: view.name,
            count: replaced.length,
          })}
        </p>
      ) : null}
    </li>
  )
}
