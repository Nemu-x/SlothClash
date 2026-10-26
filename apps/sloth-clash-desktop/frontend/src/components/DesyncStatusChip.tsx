import { useTranslation } from 'react-i18next'

import type { DesyncPhaseView } from '../utils/desyncStatus'

/** Inline status chip: a toned dot plus the phase text. */
export function DesyncStatusChip({
  view,
  prefix,
  sub,
}: {
  view: DesyncPhaseView
  prefix?: string
  sub?: boolean
}) {
  const { t } = useTranslation()
  const text = t(`desync.phase.${view.phase}`, {
    attempt: view.attempt,
    total: view.total,
  })
  return (
    <span
      className={`desyncChip${sub ? ' desyncChipSub' : ''}`}
      data-tone={view.tone}
      data-phase={view.phase}
      role="status"
    >
      <span className="desyncChipDot" aria-hidden />
      <span className="desyncChipText">
        {prefix ? `${prefix}: ${text}` : text}
      </span>
    </span>
  )
}
