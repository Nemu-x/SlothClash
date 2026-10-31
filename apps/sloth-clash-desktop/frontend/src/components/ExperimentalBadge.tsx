import { useTranslation } from 'react-i18next'

/**
 * Marks a feature as experimental next to its title: it may not work on
 * every network and its options can still change.
 */
export function ExperimentalBadge() {
  const { t } = useTranslation()
  return (
    // Outside the accessible name of the title it sits in: the dialog stays
    // "Desync", the label is a visual cue with the hint on hover.
    <span
      className="experimentalBadge"
      title={t('common.experimentalHint')}
      aria-hidden
    >
      {t('common.experimental')}
    </span>
  )
}
