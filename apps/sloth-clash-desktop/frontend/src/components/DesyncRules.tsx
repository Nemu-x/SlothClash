import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type {
  DesyncRuleInput,
  DesyncRuleKind,
  DesyncRuleRoute,
} from '../api/desync'
import type { DesyncCustomView } from '../utils/desyncStatus'

import { DesyncStatusChip } from './DesyncStatusChip'
import { DesyncStrategyLine } from './DesyncStrategyLine'

const KINDS: DesyncRuleKind[] = ['suffix', 'domain', 'process']
const ROUTES: DesyncRuleRoute[] = ['desync', 'vpn', 'direct']

const PLACEHOLDER: Record<DesyncRuleKind, string> = {
  suffix: 'example.com',
  domain: 'www.example.com',
  process: 'Telegram.exe',
}

/**
 * "My rules": the user's routing rules, one row each (what it matches, its
 * live status, on/off, edit, remove), and the form that adds a rule or edits
 * the one picked with Edit. A rule matches a domain with its subdomains, one
 * domain or an app (process name), and goes through Desync (strategy found
 * automatically; an app gets the default one), the VPN or directly.
 */
export function DesyncRules({
  rows,
  onboarding,
  busy,
  supported,
  allowVPN,
  atLimit,
  limit,
  ipHint,
  error,
  errorId,
  onSubmit,
  onToggle,
  onRemove,
  onClearError,
}: {
  rows: DesyncCustomView[]
  /** Onboarding: rows without live status. */
  onboarding: boolean
  busy: boolean
  /** The core runs Desync: adding and switching rules on is possible. */
  supported: boolean
  /** The profile has a VPN to send a rule to (not a Desync-only profile). */
  allowVPN: boolean
  /** The list is full: nothing can be added. */
  atLimit: boolean
  limit: number
  /** Under a rule Desync cannot unblock. */
  ipHint: string
  /** The last write's error (translated), shown under the form. */
  error: string
  errorId: string
  /** Add a rule (`editing` null) or replace rule `editing`; true on success. */
  onSubmit: (rule: DesyncRuleInput, editing: string | null) => Promise<boolean>
  onToggle: (id: string, enabled: boolean) => void
  onRemove: (id: string) => void
  onClearError: () => void
}) {
  const { t } = useTranslation()
  const [kind, setKind] = useState<DesyncRuleKind>('suffix')
  const [value, setValue] = useState('')
  const [route, setRoute] = useState<DesyncRuleRoute>('desync')
  const [editing, setEditing] = useState<string | null>(null)
  const routes = ROUTES.filter((r) => allowVPN || r !== 'vpn')
  // A rule stored with the VPN route on a profile without one cannot be
  // edited into a new one.
  const shownRoute = routes.includes(route) ? route : 'desync'
  const editedRow = rows.find((r) => r.id === editing) ?? null

  function reset() {
    setKind('suffix')
    setValue('')
    setRoute('desync')
    setEditing(null)
  }

  function startEdit(row: DesyncCustomView) {
    onClearError()
    setKind(row.kind)
    setValue(row.domain)
    setRoute(row.route)
    setEditing(row.id)
  }

  async function submit() {
    const typed = value.trim()
    if (!typed) return
    const target = editedRow ? editing : null
    if (await onSubmit({ kind, value: typed, route: shownRoute }, target))
      reset()
  }

  const describe = (row: DesyncCustomView) =>
    `${t(`desync.rules.kindShort.${row.kind}`)} → ${t(`desync.rules.route.${row.route}`)}`
  const valueLabel = t(
    kind === 'process'
      ? 'desync.rules.valueLabel.process'
      : 'desync.rules.valueLabel.domain',
  )
  const disabledForm = busy || !supported || (atLimit && !editedRow)

  return (
    <div className="desyncSubgroup desyncOwnSites">
      {rows.length > 0 ? (
        <ul className="desyncServices desyncCustomList">
          {rows.map((row) => (
            <li
              key={row.id}
              className="desyncService desyncCustomRow"
              data-custom={row.id}
              data-kind={row.kind}
              data-route={row.route}
              aria-current={row.id === editing ? 'true' : undefined}
            >
              <span className="desyncRuleInfo">
                <span
                  className="desyncServiceName desyncCustomDomain"
                  title={row.domain}
                >
                  {row.domain}
                </span>
                <span className="desyncRuleMeta">{describe(row)}</span>
              </span>
              <div className="desyncServiceStatus">
                {onboarding ? null : (
                  <>
                    <DesyncStatusChip view={row.status} />
                    <DesyncStrategyLine view={row.status} />
                    {row.status.phase === 'unavailable' ? (
                      <span className="desyncCustomNote">{ipHint}</span>
                    ) : null}
                  </>
                )}
              </div>
              <div className="desyncCustomControls">
                <button
                  type="button"
                  role="switch"
                  aria-checked={row.checked}
                  aria-label={row.domain}
                  className={`settingsSwitch${row.checked ? ' on' : ''}`}
                  disabled={busy || (!supported && !row.checked)}
                  onClick={() => onToggle(row.id, !row.checked)}
                >
                  <span className="settingsSwitchKnob" aria-hidden />
                </button>
                <button
                  type="button"
                  className="desyncCustomRemove desyncRuleEdit"
                  aria-label={t('desync.rules.edit', { value: row.domain })}
                  title={t('desync.rules.edit', { value: row.domain })}
                  disabled={busy || !supported}
                  onClick={() => startEdit(row)}
                >
                  <span aria-hidden>✎</span>
                </button>
                <button
                  type="button"
                  className="desyncCustomRemove"
                  aria-label={t('desync.custom.remove', {
                    domain: row.domain,
                  })}
                  title={t('desync.custom.remove', { domain: row.domain })}
                  disabled={busy}
                  onClick={() => {
                    if (row.id === editing) reset()
                    onRemove(row.id)
                  }}
                >
                  <span aria-hidden>×</span>
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : null}
      <form
        className="desyncCustomAdd desyncRuleForm"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <select
          className="selectModern selectInline desyncRuleKind"
          value={kind}
          onChange={(e) => {
            setKind(e.target.value as DesyncRuleKind)
            onClearError()
          }}
          aria-label={t('desync.rules.kindLabel')}
          disabled={disabledForm}
        >
          {KINDS.map((k) => (
            <option key={k} value={k}>
              {t(`desync.rules.kind.${k}`)}
            </option>
          ))}
        </select>
        <input
          className="input"
          type="text"
          value={value}
          onChange={(e) => {
            setValue(e.target.value)
            onClearError()
          }}
          placeholder={PLACEHOLDER[kind]}
          aria-label={valueLabel}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : undefined}
          disabled={disabledForm}
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
        />
        <select
          className="selectModern selectInline desyncRuleRoute"
          value={shownRoute}
          onChange={(e) => {
            setRoute(e.target.value as DesyncRuleRoute)
            onClearError()
          }}
          aria-label={t('desync.rules.routeLabel')}
          disabled={disabledForm}
        >
          {routes.map((r) => (
            <option key={r} value={r}>
              {t(`desync.rules.route.${r}`)}
            </option>
          ))}
        </select>
        <button
          type="submit"
          className="btn"
          disabled={disabledForm || !value.trim()}
        >
          {t(editedRow ? 'desync.rules.save' : 'desync.custom.add')}
        </button>
        {editedRow ? (
          <button
            type="button"
            className="btn"
            disabled={busy}
            onClick={() => {
              reset()
              onClearError()
            }}
          >
            {t('desync.rules.cancel')}
          </button>
        ) : null}
      </form>
      {kind === 'process' && shownRoute === 'desync' && !error ? (
        <p className="small muted desyncRuleNote">
          {t('desync.rules.processHint')}
        </p>
      ) : null}
      {error ? (
        <p id={errorId} className="error small" role="alert">
          {error}
        </p>
      ) : atLimit ? (
        <p className="small muted">
          {t('desync.custom.limitReached', { limit })}
        </p>
      ) : null}
    </div>
  )
}
