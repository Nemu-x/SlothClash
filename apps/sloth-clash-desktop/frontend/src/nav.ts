import type { ComponentType, SVGProps } from 'react'

import {
  IconAdvanced,
  IconConnections,
  IconHome,
  IconLogs,
  IconProfiles,
  IconProxies,
  IconRules,
  IconSettings,
} from './navIcons'
import type { Screen } from './types/app'

export const NAV_DEFS: {
  id: Screen
  labelKey: string
  Icon: ComponentType<SVGProps<SVGSVGElement>>
}[] = [
  { id: 'home', labelKey: 'nav.home', Icon: IconHome },
  { id: 'profiles', labelKey: 'nav.profiles', Icon: IconProfiles },
  { id: 'proxies', labelKey: 'nav.proxies', Icon: IconProxies },
  { id: 'connections', labelKey: 'nav.connections', Icon: IconConnections },
  { id: 'rules', labelKey: 'nav.rules', Icon: IconRules },
  { id: 'logs', labelKey: 'nav.logs', Icon: IconLogs },
  { id: 'advanced', labelKey: 'nav.advanced', Icon: IconAdvanced },
  { id: 'settings', labelKey: 'nav.settings', Icon: IconSettings },
]
