import { useTranslation } from 'react-i18next'

import { useDesyncCapabilities } from '../hooks/queries/useDesyncCapabilities'
import type { DesyncPhaseView } from '../utils/desyncStatus'
import { desyncStrategyLabel } from '../utils/desyncStrategy'

/**
 * Under a status chip: the strategy calibration chose for the line, in plain
 * words ("Strategy: Split the TLS record, variant 2 · chosen automatically").
 * Nothing while calibration has not settled or the route does not use one.
 */
export function DesyncStrategyLine({
  view,
  voice = false,
}: {
  view: DesyncPhaseView
  /** The Discord voice line ("Voice strategy: …"). */
  voice?: boolean
}) {
  const { t } = useTranslation()
  const caps = useDesyncCapabilities()
  const label = desyncStrategyLabel(t, view, caps?.strategies ?? [])
  if (!label) return null
  const strategy = label.strategy
    ? `${label.text} · ${t('desync.strategy.automatic')}`
    : label.text
  return (
    <span
      className="desyncStrategy"
      data-strategy={label.strategy?.id ?? ''}
      title={label.strategy?.description || undefined}
    >
      {t(voice ? 'desync.strategy.voiceLine' : 'desync.strategy.line', {
        strategy,
      })}
    </span>
  )
}
