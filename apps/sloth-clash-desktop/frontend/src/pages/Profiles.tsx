import { useTranslation } from 'react-i18next'

import { formatProfileAgo } from '../utils/format'
import {
  profileSubscriptionHost,
  profileTrafficLine,
  profileTrafficPair,
} from '../utils/subscription'
import { friendlyErrorMessage } from '../utils/yaml'

export type ProfileMenuTarget = {
  id: string
  name: string
  x: number
  y: number
}

export function ProfilesPage({
  profiles,
  activeProfileId,
  refreshBusyId,
  error,
  onImport,
  onActivate,
  onRefresh,
  onContextMenu,
}: {
  profiles: any[]
  activeProfileId: string
  refreshBusyId: string | null
  error: string | null
  onImport: () => void
  onActivate: (id: string) => void
  onRefresh: (id: string) => void
  onContextMenu: (target: ProfileMenuTarget) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="panel">
      <div className="profilesHeader">
        <h2 className="profilesPageTitle">{t('ui.profiles.title')}</h2>
        <p className="muted profilesLead">{t('ui.profiles.lead')}</p>
      </div>
      <div className="profilesToolbar">
        <button type="button" className="btn primary" onClick={onImport}>
          {t('ui.profiles.importSubscription')}
        </button>
      </div>
      <div className="profileList">
        {profiles.map((p) => {
          const active = activeProfileId === p.id
          const trafficPair = profileTrafficPair(p)
          const trafficTitle = profileTrafficLine(p)
          const host = profileSubscriptionHost(String(p.url ?? ''))
          const ago = formatProfileAgo(Number(p.lastUpdated ?? 0))
          return (
            <div
              key={p.id}
              className={`profileCard${active ? ' profileCardActive' : ''}`}
              role="button"
              tabIndex={0}
              onClick={() => {
                if (active) return
                onActivate(p.id)
              }}
              onKeyDown={(e) => {
                if (active) return
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  onActivate(p.id)
                }
              }}
              onContextMenu={(e) => {
                e.preventDefault()
                e.stopPropagation()
                onContextMenu({
                  id: p.id,
                  name: p.name,
                  x: e.clientX,
                  y: e.clientY,
                })
              }}
            >
              <div className="profileCardInner">
                <div className="profileCardTopRow">
                  <div className="profileTitle" title={String(p.name ?? '')}>
                    {p.name}
                  </div>
                  <button
                    type="button"
                    className={`profileRefreshIcon${refreshBusyId === p.id ? ' isBusy' : ''}`}
                    aria-label={t('ui.profiles.refreshSubscription')}
                    title={t('ui.profiles.refreshSubscription')}
                    disabled={
                      !String(p.url ?? '').trim() || refreshBusyId === p.id
                    }
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      if (!String(p.url ?? '').trim()) return
                      onRefresh(p.id)
                    }}
                  >
                    <svg
                      className="profileRefreshSvg"
                      viewBox="0 0 24 24"
                      width="18"
                      height="18"
                      aria-hidden
                    >
                      <path
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        d="M21 12a9 9 0 1 1-3-6.7"
                      />
                      <path
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        d="M21 3v6h-6"
                      />
                    </svg>
                  </button>
                </div>
                <div className="profileHostRow">
                  <span
                    className="profileHost"
                    title={p.url ? String(p.url) : undefined}
                  >
                    {host ||
                      (p.url
                        ? t('ui.common.subscription')
                        : t('ui.profiles.localNoUrl'))}
                  </span>
                  {ago ? <span className="profileUpdated">{ago}</span> : null}
                </div>
                {trafficPair ? (
                  <div
                    className="profileTrafficPair"
                    title={trafficTitle || trafficPair}
                  >
                    {trafficPair}
                  </div>
                ) : null}
                <div className="profileCardFoot">
                  <span className="profileTypeChip">{p.type}</span>
                  {active ? (
                    <span className="profileBadge">
                      {t('ui.profiles.active')}
                    </span>
                  ) : (
                    <span className="profileClickHint">
                      {t('ui.profiles.activate')}
                    </span>
                  )}
                </div>
              </div>
            </div>
          )
        })}
      </div>
      {error ? <p className="error">{friendlyErrorMessage(error)}</p> : null}
    </div>
  )
}
