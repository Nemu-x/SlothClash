import { useTranslation } from 'react-i18next'

import { useDesyncCapabilities } from '../hooks/queries/useDesyncCapabilities'
import { desyncMethodName } from '../utils/desyncStrategy'

/**
 * "What this computer can do": the methods this build implements on this OS
 * and how many catalog strategies calibration tries here. Collapsed by
 * default. It is read from the build, not measured: it says nothing about
 * what works on the current network, and the note says so.
 */
export function DesyncCapabilities() {
  const { t } = useTranslation()
  const report = useDesyncCapabilities()
  if (!report || report.methods.length === 0) return null
  const supported = report.methods.filter((m) => m.supported).length
  const tried = report.strategies.filter((s) => s.tried).length
  return (
    <details className="desyncSection desyncCaps">
      <summary className="desyncCapsSummary">
        <span className="desyncSectionTitle">{t('desync.caps.title')}</span>
        <span className="desyncCapsCount">
          {t('desync.caps.count', {
            supported,
            total: report.methods.length,
          })}
        </span>
      </summary>
      <p className="small muted desyncCapsNote">{t('desync.caps.note')}</p>
      <ul className="desyncCapsList">
        {report.methods.map((m) => {
          const name = desyncMethodName(t, m.method)
          return (
            <li
              key={m.method}
              className="desyncCapsRow"
              data-method={m.method}
              data-supported={m.supported}
            >
              <span className="desyncCapsName">{name}</span>
              <span
                className="desyncChip desyncCapsState"
                data-tone={m.supported ? 'ok' : 'off'}
                title={m.inferred ? t('desync.caps.inferredHint') : undefined}
              >
                <span className="desyncChipDot" aria-hidden />
                <span className="desyncChipText">
                  {m.supported
                    ? m.inferred
                      ? t('desync.caps.supportedInferred')
                      : t('desync.caps.supported')
                    : t('desync.caps.unsupported')}
                </span>
              </span>
              {!m.supported && m.reason ? (
                <span className="desyncCapsReason" lang="en">
                  {m.reason}
                </span>
              ) : null}
            </li>
          )
        })}
      </ul>
      {report.strategies.length > 0 ? (
        <p className="small muted desyncCapsStrategies">
          {t('desync.caps.strategies', {
            tried,
            total: report.strategies.length,
          })}
        </p>
      ) : null}
    </details>
  )
}
