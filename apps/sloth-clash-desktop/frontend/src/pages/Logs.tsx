import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { main } from '../api/models'

type Level = 'info' | 'warn' | 'error' | 'debug' | 'other'

const TAIL_WINDOW = 400

// Mihomo / Clash Meta log lines come in a few shapes:
//   level=info  msg="..."
//   [INFO] ...
//   2024-01-02 12:34:56 [INFO] ...
//   time="..." level=warning msg="..."
// Match the level token regardless of casing or surrounding decoration.
const LEVEL_RE =
  /(?:level\s*=\s*|\[)(debug|info|warn(?:ing)?|error|fatal)(?:\]|\b)/i

function classifyLevel(line: string): Level {
  const m = LEVEL_RE.exec(line)
  if (!m) return 'other'
  const v = m[1].toLowerCase()
  if (v === 'debug') return 'debug'
  if (v === 'info') return 'info'
  if (v === 'warn' || v === 'warning') return 'warn'
  if (v === 'error' || v === 'fatal') return 'error'
  return 'other'
}

type LevelFilter = 'all' | Level

export function LogsPage({
  serviceLog,
  onRefresh,
}: {
  serviceLog: main.ServiceLogPeek | null
  onRefresh: () => void
}) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [level, setLevel] = useState<LevelFilter>('all')
  const [tailOnly, setTailOnly] = useState(true)
  const [copyToast, setCopyToast] = useState('')

  const text = serviceLog?.text ?? ''

  const allLines = useMemo(() => {
    if (!text) return [] as string[]
    return text.split('\n')
  }, [text])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    let lines = allLines
    if (tailOnly && lines.length > TAIL_WINDOW) {
      lines = lines.slice(-TAIL_WINDOW)
    }
    if (level !== 'all') {
      lines = lines.filter((line) => classifyLevel(line) === level)
    }
    if (q) {
      lines = lines.filter((line) => line.toLowerCase().includes(q))
    }
    return lines
  }, [allLines, query, level, tailOnly])

  const copyText = async (lines: string[]) => {
    if (lines.length === 0) return
    try {
      await navigator.clipboard.writeText(lines.join('\n'))
      setCopyToast(t('ui.logs.copied'))
      window.setTimeout(() => setCopyToast(''), 2200)
    } catch {
      setCopyToast('Clipboard unavailable')
      window.setTimeout(() => setCopyToast(''), 2200)
    }
  }

  return (
    <div className="panel rulesPanel">
      <div className="rulesPanelHead">
        <h2 className="rulesPanelTitle">{t('ui.logs.title')}</h2>
        <div className="row" style={{ gap: 8 }}>
          <button type="button" className="btn ghost" onClick={onRefresh}>
            {t('ui.logs.refresh')}
          </button>
          <button
            type="button"
            className="btn ghost"
            onClick={() => void copyText(allLines)}
            disabled={allLines.length === 0}
          >
            {t('ui.logs.copyAll')}
          </button>
          <button
            type="button"
            className="btn ghost"
            onClick={() => void copyText(filtered)}
            disabled={filtered.length === 0}
          >
            {t('ui.logs.copyFiltered')}
          </button>
        </div>
      </div>

      <div className="logsToolbar">
        <input
          type="text"
          className="inputModern logsSearchInput"
          placeholder={t('ui.logs.search')}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label={t('ui.logs.search')}
        />
        <div className="logsLevelChips" role="group" aria-label="Log level">
          {(['all', 'info', 'warn', 'error', 'debug'] as LevelFilter[]).map(
            (lv) => (
              <button
                key={lv}
                type="button"
                className={`logsLevelChip${level === lv ? ' active' : ''} logsLevelChip-${lv}`}
                onClick={() => setLevel(lv)}
              >
                {t(
                  lv === 'all'
                    ? 'ui.logs.levelAll'
                    : lv === 'info'
                      ? 'ui.logs.levelInfo'
                      : lv === 'warn'
                        ? 'ui.logs.levelWarn'
                        : lv === 'error'
                          ? 'ui.logs.levelError'
                          : 'ui.logs.levelDebug',
                )}
              </button>
            ),
          )}
        </div>
        <label className="logsTailToggle" title={t('ui.logs.tailOnlyHint')}>
          <input
            type="checkbox"
            checked={tailOnly}
            onChange={(e) => setTailOnly(e.target.checked)}
          />
          {t('ui.logs.tailOnly')}
        </label>
      </div>

      {serviceLog?.path ? (
        <p className="muted small tight">
          <strong className="monoTight">{serviceLog.path}</strong>{' '}
          <span className="muted">
            ·{' '}
            {t('ui.logs.lineCount', {
              visible: filtered.length,
              total: allLines.length,
            })}
          </span>
        </p>
      ) : null}
      {serviceLog?.lastError ? (
        <p className="error tight">{serviceLog.lastError}</p>
      ) : null}

      {copyToast ? <p className="muted small tight">{copyToast}</p> : null}

      {filtered.length === 0 ? (
        <p className="muted small">
          {allLines.length === 0 ? t('ui.logs.empty') : t('ui.logs.noMatch')}
        </p>
      ) : (
        <pre className="mono tightPre logPre">{filtered.join('\n')}</pre>
      )}
    </div>
  )
}
