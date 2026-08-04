import { useTranslation } from 'react-i18next'

import { NAV_DEFS } from '../nav'
import { IconChevronNav } from '../navIcons'
import type { Screen } from '../types/app'

export function SidebarNav({
  screen,
  onChange,
  collapsed,
  onToggleCollapse,
  hiddenScreens,
  activeScreens,
}: {
  screen: Screen
  onChange: (next: Screen) => void
  collapsed: boolean
  onToggleCollapse: () => void
  // Brand hide flags (presentation-only): screens dropped from the nav.
  hiddenScreens?: Screen[]
  // Screens showing a live "active" dot (e.g. Corporate VPN connected).
  activeScreens?: Screen[]
}) {
  const { t } = useTranslation()
  return (
    <aside className="nav">
      <nav className="navList">
        {NAV_DEFS.filter(({ id }) => !hiddenScreens?.includes(id)).map(
          ({ id, labelKey, Icon }) => (
            <button
              key={id}
              type="button"
              className={screen === id ? 'navItem active' : 'navItem'}
              title={t(labelKey)}
              onClick={() => onChange(id)}
            >
              <span className="navIcon" aria-hidden>
                <Icon />
                {activeScreens?.includes(id) ? (
                  <span className="navActiveDot" aria-hidden />
                ) : null}
              </span>
              <span className="navLabel">{t(labelKey)}</span>
            </button>
          ),
        )}
      </nav>
      <button
        type="button"
        className="navCollapseBtn"
        title={collapsed ? t('nav.expandSidebar') : t('nav.collapseSidebar')}
        onClick={onToggleCollapse}
      >
        <span className={`navChevronWrap ${collapsed ? 'collapsed' : ''}`}>
          <IconChevronNav />
        </span>
      </button>
    </aside>
  )
}
